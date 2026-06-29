// Package node provides access to the nodejs.org/dist/index.json API.
// It fetches, caches, and resolves Node.js versions (LTS, Current, etc.).
package node

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/Masterminds/semver/v3"
)

// ManifestVersion represents a single entry from nodejs.org/dist/index.json.
// Only the fields we need are captured; the JSON has many more.
type ManifestVersion struct {
	Version string `json:"version"` // e.g., "v22.5.0"
	Date    string `json:"date"`    // e.g., "2024-07-01"
	LTS     bool   `json:"lts"`     // true if this is an LTS release
	TS      string `json:"ts"`      // LTS codename like "Argon" or empty/nil
}

// Manifest is the full parsed index.json structure.
type Manifest []ManifestVersion

// FetchManifest pulls the latest index.json from nodejs.org.
// On success, it caches the result for offline use.
// Cache TTL is 24 hours by default.
func FetchManifest() (Manifest, error) {
	m, ok := loadFromCache()
	if ok {
		return m, nil
	}

	return fetchAndCacheFresh()
}

// FetchManifestForce bypasses the cache and fetches fresh data.
func FetchManifestForce() (Manifest, error) {
	return fetchAndCacheFresh()
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

// fetchAndCacheFresh downloads, parses, and caches the manifest.
func fetchAndCacheFresh() (Manifest, error) {
	url := "https://nodejs.org/dist/index.json"

	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("fetch manifest from %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch manifest from %s: HTTP %d", url, resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read manifest body: %w", err)
	}

	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}

	if err := saveToCache(data); err != nil {
		// Cache failure is not fatal; the manifest is still valid.
		_ = err
	}

	return m, nil
}

// LatestLTS returns the most recent LTS version from the manifest.
// Uses semver comparison to find the highest LTS version.
func (m Manifest) LatestLTS() (*ManifestVersion, error) {
	var latest *ManifestVersion
	var latestSem *semver.Version

	for i := range m {
		// Skip LTS=false with empty TS (these are Current releases)
		// But include LTS=true OR TS!="" (LTS releases)
		isLTS := m[i].LTS || m[i].TS != ""
		if !isLTS {
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
		// Current releases have LTS=false and empty TS
		if m[i].LTS || m[i].TS != "" {
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
func cachePath() (string, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}

	appDir := filepath.Join(cacheDir, "nodeup")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		return "", err
	}

	return filepath.Join(appDir, "node-dist-index.json"), nil
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
	cacheFile, err := cachePath()
	if err != nil {
		return nil, false
	}

	metaFile, err := cacheMetaPath()
	if err != nil {
		return nil, false
	}

	data, err := os.ReadFile(cacheFile)
	if err != nil {
		return nil, false
	}

	meta, err := os.ReadFile(metaFile)
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

	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, false
	}

	return m, true
}

// saveToCache writes the raw JSON to cache and stores expiry timestamp.
func saveToCache(data []byte) error {
	cacheFile, err := cachePath()
	if err != nil {
		return err
	}

	metaFile, err := cacheMetaPath()
	if err != nil {
		return err
	}

	if err := os.WriteFile(cacheFile, data, 0o644); err != nil {
		return err
	}

	expiry := time.Now().Add(24 * time.Hour)
	return os.WriteFile(metaFile, []byte(expiry.Format(time.RFC3339)), 0o644)
}
