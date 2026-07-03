// Package node provides access to the nodejs.org/dist/index.json API.
// It fetches, caches, and resolves Node.js versions (LTS, Current, etc.).
package node

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/Masterminds/semver/v3"

	"github.com/dipto0321/nodeup/internal/platform"
)

// ManifestVersion represents a single entry from nodejs.org/dist/index.json.
// Only the fields we need are captured; the JSON has many more.
//
// The nodejs.org `lts` field is a JSON union: a string codename (e.g.
// "Iron") for LTS releases, and the literal `false` for Current releases.
// We model it as *string: nil means non-LTS / Current; non-nil means LTS
// and the value is the codename. A custom UnmarshalJSON is used so this
// works without callers having to know about the union.
type ManifestVersion struct {
	Version     string  `json:"version"` // e.g., "v22.5.0"
	Date        string  `json:"date"`    // e.g., "2024-07-01"
	LTSCodename *string `json:"lts"`     // nil=Current; otherwise LTS codename
}

// UnmarshalJSON decodes a nodejs.org index.json entry. The `lts` field is
// either the JSON literal `false` (Current release) or a string codename
// (LTS release). We split it with json.RawMessage so the struct remains
// idiomatic Go.
func (m *ManifestVersion) UnmarshalJSON(data []byte) error {
	// alias avoids recursing into UnmarshalJSON.
	type alias ManifestVersion
	var raw struct {
		alias
		LTS json.RawMessage `json:"lts"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*m = ManifestVersion(raw.alias)

	// Distinguish the JSON literal `false` from a missing/null field.
	if len(raw.LTS) == 0 || string(raw.LTS) == "null" {
		m.LTSCodename = nil
		return nil
	}
	if string(raw.LTS) == "false" {
		m.LTSCodename = nil
		return nil
	}
	var name string
	if err := json.Unmarshal(raw.LTS, &name); err != nil {
		return fmt.Errorf("decode lts codename: %w", err)
	}
	m.LTSCodename = &name
	return nil
}

// Manifest is the full parsed index.json structure.
type Manifest []ManifestVersion

// httpClient is the package-level HTTP client used by FetchManifestCtx.
//
// We use http.DefaultClient as the seed so tests can inject a
// transport via http.DefaultTransport (the standard idiom —
// http.DefaultClient.Transport is shared with anything that builds on
// default). The Timeout is the floor for every fetch: an in-flight
// download cannot block longer than this no matter what the caller
// hands us via context. See issue #48.
var httpClient = &http.Client{
	Timeout: 30 * time.Second,
}

// FetchManifest pulls the latest index.json from nodejs.org.
// On success, it caches the result for offline use.
// Cache TTL is 24 hours by default.
//
// Equivalent to FetchManifestCtx(context.Background()) — retained for
// callers that don't have a context handy. New code should prefer
// FetchManifestCtx so user cancellation propagates.
func FetchManifest() (Manifest, error) {
	return FetchManifestCtx(context.Background())
}

// FetchManifestCtx is the context-aware variant of FetchManifest.
//
// The ctx is threaded through the HTTP request so a caller
// (`nodeup upgrade` / `nodeup check`) can cancel an in-flight fetch on
// Ctrl-C. Combined with httpClient.Timeout above, an unresponsive
// nodejs.org (or a proxy that silently drops the connection) cannot
// hang nodeup forever.
func FetchManifestCtx(ctx context.Context) (Manifest, error) {
	m, ok := loadFromCache()
	if ok {
		return m, nil
	}

	return fetchAndCacheFresh(ctx)
}

// FetchManifestForce bypasses the cache and fetches fresh data.
// Equivalent to FetchManifestForceCtx(context.Background()).
func FetchManifestForce() (Manifest, error) {
	return FetchManifestForceCtx(context.Background())
}

// FetchManifestForceCtx is the context-aware variant.
func FetchManifestForceCtx(ctx context.Context) (Manifest, error) {
	return fetchAndCacheFresh(ctx)
}

// LoadCached loads the manifest from cache without checking expiry.
// Used when --offline flag is set.
func LoadCached() (Manifest, error) {
	cacheFile, err := cachePath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(cacheFile)
	if err != nil {
		return nil, fmt.Errorf("read cache file: %w", err)
	}

	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse cached manifest: %w", err)
	}

	return m, nil
}

// manifestURL is the canonical nodejs.org index. Exposed as a package
// var (not const) so tests can redirect it to httptest.Server.URL.
var manifestURL = "https://nodejs.org/dist/index.json"

// maxFetchAttempts bounds the retry loop. We retry transient failures
// (5xx, network errors, ctx-not-yet-canceled) up to this many times;
// beyond it the surface error is fatal.
const maxFetchAttempts = 3

// fetchAndCacheFresh downloads, parses, and caches the manifest.
//
// Retry behavior: a transient failure (network error, 5xx response)
// triggers up to `maxFetchAttempts` total attempts with exponential
// backoff (200ms, 400ms, ... capped at 2s). Permanent errors (4xx
// other than 408/429) are returned on the first attempt without
// retry. Context cancellation aborts the retry loop immediately —
// nothing is cached on cancellation.
func fetchAndCacheFresh(ctx context.Context) (Manifest, error) {
	data, err := fetchManifestWithRetry(ctx)
	if err != nil {
		return nil, err
	}

	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}

	// Cache failure is not fatal; the manifest is still valid for this
	// run. We deliberately swallow the error so transient cache I/O
	// problems don't break an otherwise successful network fetch.
	if err := saveToCache(data); err != nil {
		_ = err
	}

	return m, nil
}

// fetchManifestWithRetry implements the retry-with-backoff loop. It
// returns the raw response body so the caller can both parse it and
// persist it to cache.
func fetchManifestWithRetry(ctx context.Context) ([]byte, error) {
	// Backoff sequence: 200ms, 400ms, 800ms. Picked empirically — long
	// enough to ride out a brief nodejs.org hiccup, short enough that
	// the user doesn't wait noticeably.
	backoff := 200 * time.Millisecond

	var lastErr error
	for attempt := 1; attempt <= maxFetchAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("fetch manifest: %w", err)
		}

		data, err := fetchManifestOnce(ctx)
		if err == nil {
			return data, nil
		}
		lastErr = err

		// Permanent errors (4xx that aren't 408/429) are not
		// retryable: hitting them again just wastes time.
		if !retryableFetchError(err) {
			return nil, err
		}

		// Don't sleep after the last attempt — we'd just delay the
		// return of the final error.
		if attempt == maxFetchAttempts {
			break
		}

		// Respect cancellation while sleeping.
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("fetch manifest: %w", ctx.Err())
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > 2*time.Second {
			backoff = 2 * time.Second
		}
	}
	return nil, fmt.Errorf("fetch manifest from %s: %w", manifestURL, lastErr)
}

// fetchManifestOnce performs a single HTTP fetch with context.
// Returns the raw body bytes. Network errors and 5xx return
// `*fetchError` so the retry loop can distinguish retryable from
// permanent.
func fetchManifestOnce(ctx context.Context) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("build manifest request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "nodeup/1.0 (+https://github.com/dipto0321/nodeup)")

	resp, err := httpClient.Do(req)
	if err != nil {
		// http.Client.Do returns the network error directly (no
		// response, since we never got one). Always retryable.
		return nil, &fetchError{Status: 0, Err: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, &fetchError{Status: resp.StatusCode, Err: err}
		}
		return data, nil
	}

	// Drain a short prefix of the body so error messages aren't
	// completely opaque. Cap at 512 bytes — bigger errors are just
	// noise in the CLI.
	prefix, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	return nil, &fetchError{
		Status: resp.StatusCode,
		Err:    fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(prefix)),
	}
}

// fetchError wraps the underlying HTTP / I/O error with the response
// status (0 = no response received). retryableFetchError reads this
// to decide whether to retry.
type fetchError struct {
	Status int
	Err    error
}

func (e *fetchError) Error() string { return e.Err.Error() }
func (e *fetchError) Unwrap() error { return e.Err }

// retryableFetchError implements the retry classifier. Network errors
// (Status == 0), 5xx, plus the explicitly retryable 4xx (408 Request
// Timeout, 429 Too Many Requests) get retried up to maxFetchAttempts;
// everything else (400, 401, ..., 404) is treated as permanent and
// returned immediately.
func retryableFetchError(err error) bool {
	var fe *fetchError
	if !errors.As(err, &fe) {
		// Non-fetchError (e.g. URL parse failure). Treat as permanent.
		return false
	}
	if fe.Status == 0 {
		// Network error — connection refused, DNS failure, etc. Always
		// retryable; the next attempt might land on a different path.
		return true
	}
	if fe.Status >= 500 {
		return true
	}
	if fe.Status == http.StatusRequestTimeout || fe.Status == http.StatusTooManyRequests {
		return true
	}
	return false
}

// LatestLTS returns the most recent LTS version from the manifest.
// Uses semver comparison to find the highest LTS version.
func (m Manifest) LatestLTS() (*ManifestVersion, error) {
	var latest *ManifestVersion
	var latestSem *semver.Version

	for i := range m {
		// LTS releases have a non-nil codename (e.g. "Iron"); Current
		// releases leave the field as JSON `false` and decode to nil.
		if m[i].LTSCodename == nil {
			continue
		}

		v, err := parseVersion(m[i].Version)
		if err != nil {
			continue
		}

		if latestSem == nil || v.GreaterThan(latestSem) {
			latestSem = v
			latest = &m[i]
		}
	}

	if latest == nil {
		return nil, fmt.Errorf("no LTS version found in manifest")
	}
	return latest, nil
}

// LatestCurrent returns the most recent Current (non-LTS) version.
func (m Manifest) LatestCurrent() (*ManifestVersion, error) {
	var latest *ManifestVersion
	var latestSem *semver.Version

	for i := range m {
		// Current releases have a nil LTS codename.
		if m[i].LTSCodename != nil {
			continue
		}

		v, err := parseVersion(m[i].Version)
		if err != nil {
			continue
		}

		if latestSem == nil || v.GreaterThan(latestSem) {
			latestSem = v
			latest = &m[i]
		}
	}

	if latest == nil {
		return nil, fmt.Errorf("no Current version found in manifest")
	}
	return latest, nil
}

// parseVersion parses a version string like "v22.5.0" into semver.
func parseVersion(s string) (*semver.Version, error) {
	if s != "" && s[0] == 'v' {
		s = s[1:]
	}
	return semver.NewVersion(s)
}

// cachePath returns the path to the cached manifest file.
//
// The cache lives under platform.CacheDir() (<DataDir>/cache) — the
// single documented on-disk location for everything nodeup persists.
// An earlier revision used os.UserCacheDir()/nodeup, which diverged
// from CLAUDE.md's layout and left the documented cache dir empty
// while data accumulated elsewhere. See issue #110. The legacy
// directory is not migrated or deleted: worst case is one cold fetch
// after upgrading.
func cachePath() (string, error) {
	cacheDir, err := platform.CacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cacheDir, "node-dist-index.json"), nil
}

// cacheMetaPath returns the path to the cache metadata file (stores expiry).
func cacheMetaPath() (string, error) {
	cacheFile, err := cachePath()
	if err != nil {
		return "", err
	}
	return cacheFile + ".meta", nil
}

// loadFromCache attempts to load manifest from cache if not expired (24h TTL).
func loadFromCache() (Manifest, bool) {
	p, err := defaultCachePaths()
	if err != nil {
		return nil, false
	}
	return loadFromCacheAt(p)
}

// loadFromCacheAt is the path-injecting variant: tests redirect
// cacheIO to point at a tempdir and call this directly. Cache lookup
// is "either both files visible and meta fresh, or nothing visible
// at all" — the .meta file is the freshness gate.
func loadFromCacheAt(p cachePaths) (Manifest, bool) {
	meta, err := os.ReadFile(p.meta)
	if err != nil {
		return nil, false
	}
	var expiry time.Time
	if err := expiry.UnmarshalText(meta); err != nil {
		return nil, false
	}
	if time.Now().After(expiry) {
		return nil, false
	}
	data, err := os.ReadFile(p.data)
	if err != nil {
		return nil, false
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, false
	}
	return m, true
}

// saveToCache writes the raw JSON to cache and stores expiry timestamp.
//
// Atomicity: the manifest data file is written via temp + rename so a
// concurrent `nodeup` invocation never sees a half-written payload.
// The .meta (freshness gate) is written second; readers that race us
// during the window between the data rename and the meta rename see
// the new data with the *old* (expired) meta and treat the cache as
// stale, which is the safe failure mode. See issue #48.
func saveToCache(data []byte) error {
	p, err := defaultCachePaths()
	if err != nil {
		return err
	}
	return saveToCacheAt(p, data)
}

// saveToCacheAt is the path-injecting variant used by tests.
func saveToCacheAt(p cachePaths, data []byte) error {
	if err := writeFileAtomic(p.data, data, 0o644); err != nil {
		return err
	}
	expiry := time.Now().Add(24 * time.Hour)
	return writeFileAtomic(p.meta, []byte(expiry.Format(time.RFC3339)), 0o644)
}

// cachePaths bundles the two on-disk cache locations so tests can
// redirect both atomically without re-implementing the lookup logic.
type cachePaths struct {
	data string
	meta string
}

func defaultCachePaths() (cachePaths, error) {
	data, err := cachePath()
	if err != nil {
		return cachePaths{}, err
	}
	meta, err := cacheMetaPath()
	if err != nil {
		return cachePaths{}, err
	}
	return cachePaths{data: data, meta: meta}, nil
}

// writeFileAtomic writes data to path via temp + rename. On POSIX,
// rename within a directory is atomic; on Windows, os.Rename replaces
// the destination if it exists. We deliberately use os.Rename rather
// than os.Link + os.Remove: Link is not atomic on every Windows
// network filesystem, and a half-written cache file would be worse
// than a half-second window of staleness.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file in %s: %w", dir, err)
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close %s: %w", tmpPath, err)
	}
	if err := os.Chmod(tmpPath, perm); err != nil {
		cleanup()
		return fmt.Errorf("chmod %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		cleanup()
		return fmt.Errorf("rename %s → %s: %w", tmpPath, path, err)
	}
	return nil
}
