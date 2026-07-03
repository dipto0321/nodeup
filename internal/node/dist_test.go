package node

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dipto0321/nodeup/internal/platform"
)

// TestMain gives the whole package a throwaway home directory so no
// test — present or future — can touch the real user's nodeup data
// or cache dirs, even if it forgets to call withEmptyCache. This is
// the structural guard for issue #110: before it existed, a fetch
// test that redirected manifestURL but not the cache paths wrote its
// fixture manifest into the developer's real cache, and `nodeup
// check` served that fixture for the next 24h.
func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "nodeup-node-test-home-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "TestMain: create temp home: %v\n", err)
		os.Exit(1)
	}

	// Cover every var platform.DataDir() reads across the three OSes:
	// HOME/USERPROFILE (darwin + unix fallback), XDG_DATA_HOME (linux),
	// APPDATA (windows). XDG_CACHE_HOME is legacy coverage for the
	// pre-#110 os.UserCacheDir() location.
	os.Setenv("HOME", tmp)
	os.Setenv("USERPROFILE", tmp)
	os.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "xdg-data"))
	os.Setenv("XDG_CACHE_HOME", filepath.Join(tmp, "xdg-cache"))
	os.Setenv("APPDATA", filepath.Join(tmp, "AppData", "Roaming"))
	os.Setenv("LOCALAPPDATA", filepath.Join(tmp, "AppData", "Local"))

	code := m.Run()
	_ = os.RemoveAll(tmp)
	os.Exit(code)
}

func TestLatestLTS(t *testing.T) {
	c1 := "Argon"
	c2 := "Iron"
	m := Manifest{
		{Version: "v22.0.0", LTSCodename: &c1},
		{Version: "v20.0.0", LTSCodename: &c2},
		{Version: "v23.0.0", LTSCodename: nil},
		{Version: "v18.0.0", LTSCodename: &c1},
	}

	lts, err := m.LatestLTS()
	if err != nil {
		t.Fatalf("LatestLTS() error: %v", err)
	}

	if lts.Version != "v22.0.0" {
		t.Errorf("LatestLTS() = %s, want v22.0.0", lts.Version)
	}
}

func TestLatestCurrent(t *testing.T) {
	c1 := "Argon"
	c2 := "Iron"
	m := Manifest{
		{Version: "v22.0.0", LTSCodename: &c1},
		{Version: "v20.0.0", LTSCodename: &c2},
		{Version: "v23.0.0", LTSCodename: nil},
		{Version: "v24.0.0", LTSCodename: nil},
	}

	current, err := m.LatestCurrent()
	if err != nil {
		t.Fatalf("LatestCurrent() error: %v", err)
	}

	if current.Version != "v24.0.0" {
		t.Errorf("LatestCurrent() = %s, want v24.0.0", current.Version)
	}
}

// TestManifestUnmarshalUnion exercises the JSON `false | "codename"`
// union that nodejs.org uses for the `lts` field. The pre-fix struct
// (LTS bool) failed to parse any LTS entry; this test guards against
// regression now that UnmarshalJSON handles both shapes.
func TestManifestUnmarshalUnion(t *testing.T) {
	const fixture = `[
		{"version":"v24.0.0","date":"2025-04-01","lts":false},
		{"version":"v22.10.0","date":"2025-02-04","lts":"Jod"},
		{"version":"v20.19.0","date":"2025-03-04","lts":"Iron"}
	]`
	var m Manifest
	if err := json.Unmarshal([]byte(fixture), &m); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	// 3 entries parsed.
	if got := len(m); got != 3 {
		t.Fatalf("len(Manifest) = %d, want 3", got)
	}

	// v24.0.0 is Current — codename nil.
	if m[0].LTSCodename != nil {
		t.Errorf("v24.0.0 codename = %q, want nil", *m[0].LTSCodename)
	}

	// v22.10.0 is LTS Jod.
	if m[1].LTSCodename == nil || *m[1].LTSCodename != "Jod" {
		got := "<nil>"
		if m[1].LTSCodename != nil {
			got = *m[1].LTSCodename
		}
		t.Errorf("v22.10.0 codename = %q, want %q", got, "Jod")
	}

	// LatestLTS picks the higher semver between v22.10.0 and v20.19.0.
	lts, err := m.LatestLTS()
	if err != nil {
		t.Fatalf("LatestLTS() error: %v", err)
	}
	if lts.Version != "v22.10.0" {
		t.Errorf("LatestLTS() = %s, want v22.10.0", lts.Version)
	}

	// LatestCurrent picks v24.0.0.
	cur, err := m.LatestCurrent()
	if err != nil {
		t.Fatalf("LatestCurrent() error: %v", err)
	}
	if cur.Version != "v24.0.0" {
		t.Errorf("LatestCurrent() = %s, want v24.0.0", cur.Version)
	}
}

func TestParseVersion(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"v22.5.0", "22.5.0"},
		{"20.0.0", "20.0.0"},
		{"v18.17.0", "18.17.0"},
	}

	for _, tt := range tests {
		v, err := parseVersion(tt.input)
		if err != nil {
			t.Errorf("parseVersion(%q) error: %v", tt.input, err)
			continue
		}
		if v.String() != tt.expected {
			t.Errorf("parseVersion(%q) = %s, want %s", tt.input, v.String(), tt.expected)
		}
	}
}

// TestCacheLoadSaveRoundTrip exercises the path-injecting load/save
// helpers. The fixture builds a tiny manifest, writes it via
// saveToCacheAt, then reads it back via loadFromCacheAt and asserts
// the parsed shape matches. This was implicit before #48 (the
// pre-fix code couldn't be unit-tested without touching the user's
// real cache file); now it's a focused, hermetic test.
func TestCacheLoadSaveRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	p := cachePaths{
		data: filepath.Join(tmp, "node-dist-index.json"),
		meta: filepath.Join(tmp, "node-dist-index.json.meta"),
	}

	c := "Argon"
	original := Manifest{{Version: "v22.0.0", LTSCodename: &c}}
	data, _ := json.Marshal(original)

	if err := saveToCacheAt(p, data); err != nil {
		t.Fatalf("saveToCacheAt: %v", err)
	}

	got, ok := loadFromCacheAt(p)
	if !ok {
		t.Fatal("loadFromCacheAt returned ok=false after a fresh save")
	}
	if len(got) != 1 || got[0].Version != "v22.0.0" {
		t.Fatalf("loadFromCacheAt = %+v, want the saved manifest back", got)
	}
}

// TestCacheLoadFromCache_ExpiredMetaIsRejected pins the freshness
// gate: if meta says expired, loadFromCacheAt returns ok=false even
// if the data file is valid. This is the case a stale `-ttl=0`
// caller would hit if we ever exposed a no-TTL knob; today it's
// belt-and-suspenders to make sure a corrupted meta doesn't get
// silently treated as fresh.
func TestCacheLoadFromCache_ExpiredMetaIsRejected(t *testing.T) {
	tmp := t.TempDir()
	p := cachePaths{
		data: filepath.Join(tmp, "node-dist-index.json"),
		meta: filepath.Join(tmp, "node-dist-index.json.meta"),
	}

	c := "Iron"
	data, _ := json.Marshal(Manifest{{Version: "v20.0.0", LTSCodename: &c}})
	if err := saveToCacheAt(p, data); err != nil {
		t.Fatalf("saveToCacheAt: %v", err)
	}

	// Force the meta to a past timestamp; loadFromCacheAt must now
	// refuse to serve the (still-valid) data.
	expired := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)
	if err := os.WriteFile(p.meta, []byte(expired), 0o644); err != nil {
		t.Fatalf("overwrite meta: %v", err)
	}

	if _, ok := loadFromCacheAt(p); ok {
		t.Fatal("loadFromCacheAt returned ok=true for expired meta — freshness gate broken")
	}
}

// TestSaveToCache_NoTempFileLeakOnSuccess is a regression pin for the
// atomic-rename path: after a successful save there must be no
// leftover .tmp-* file in the cache dir. A leaked tmp means either
// the rename failed silently or another process is half-way through
// its own write.
func TestSaveToCache_NoTempFileLeakOnSuccess(t *testing.T) {
	tmp := t.TempDir()
	p := cachePaths{
		data: filepath.Join(tmp, "node-dist-index.json"),
		meta: filepath.Join(tmp, "node-dist-index.json.meta"),
	}

	c := "Jod"
	data, _ := json.Marshal(Manifest{{Version: "v22.0.0", LTSCodename: &c}})
	if err := saveToCacheAt(p, data); err != nil {
		t.Fatalf("saveToCacheAt: %v", err)
	}

	entries, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatalf("read tmp dir: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tmp-") || strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("leftover tmp file after save: %s", e.Name())
		}
	}
}

// withManifestServer swaps manifestURL to point at a httptest server
// for the duration of the test, restoring the original on cleanup.
// This is the seam fetchManifestOnce calls into via httpClient.Do,
// so a redirect to a test server isolates our behavior from the real
// network (and from timeouts on slow CI runners).
func withManifestServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	origURL := manifestURL
	manifestURL = srv.URL
	t.Cleanup(func() { manifestURL = origURL })
	return srv
}

// validManifestBody is a 2-entry fixture returning 200 + JSON.
// It exists so the success-path tests stay readable instead of
// inlining the same JSON everywhere.
const validManifestBody = `[
	{"version":"v22.10.0","date":"2025-02-04","lts":"Jod"},
	{"version":"v24.0.0","date":"2025-04-01","lts":false}
]`

// withEmptyCache steers defaultCachePaths at an isolated per-test
// tempdir so the fetch tests don't pick up cache state left by an
// earlier test in this package (TestMain already guarantees nothing
// leaks to the real user's dirs; this adds test-to-test isolation
// on top, since a saved manifest from one fetch test would
// short-circuit the next with a fresh-cache read).
//
// The cache lives under platform.CacheDir() = <DataDir>/cache, so we
// override every env var platform.DataDir() reads: HOME/USERPROFILE
// (darwin + unix fallback), XDG_DATA_HOME (linux preference), and
// APPDATA (windows — cleared so the Windows branch falls back to the
// HOME-derived path inside the tempdir).
func withEmptyCache(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "xdg-data"))
	t.Setenv("APPDATA", "")
	t.Setenv("LOCALAPPDATA", "")
}

// TestCachePathUnderPlatformCacheDir pins the manifest cache to the
// documented on-disk layout: <DataDir>/cache per platform.CacheDir().
// Issue #110: an earlier revision cached under os.UserCacheDir()/nodeup,
// leaving the documented cache dir empty while data accumulated in an
// undocumented one.
func TestCachePathUnderPlatformCacheDir(t *testing.T) {
	withEmptyCache(t)

	got, err := cachePath()
	if err != nil {
		t.Fatalf("cachePath: %v", err)
	}
	want, err := platform.CacheDir()
	if err != nil {
		t.Fatalf("platform.CacheDir: %v", err)
	}
	if filepath.Dir(got) != want {
		t.Errorf("cachePath() dir = %s, want platform.CacheDir() = %s", filepath.Dir(got), want)
	}
	if filepath.Base(got) != "node-dist-index.json" {
		t.Errorf("cachePath() basename = %s, want node-dist-index.json", filepath.Base(got))
	}
}

// TestFetchManifestCtx_Success covers the happy path: a 200 JSON
// response yields a parsed Manifest with both versions visible.
func TestFetchManifestCtx_Success(t *testing.T) {
	withEmptyCache(t)
	srv := withManifestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(validManifestBody))
	}))

	m, err := FetchManifestCtx(context.Background())
	if err != nil {
		t.Fatalf("FetchManifestCtx: %v", err)
	}
	if len(m) != 2 {
		t.Fatalf("len(Manifest) = %d, want 2", len(m))
	}
	_ = srv
}

// TestFetchManifestCtx_RetriesOn5xx exercises the retry-with-backoff
// path. The server returns 503 on the first two attempts and 200 on
// the third; fetchManifestWithRetry must absorb the 503s and report
// success, total attempts == 3.
func TestFetchManifestCtx_RetriesOn5xx(t *testing.T) {
	withEmptyCache(t)
	var hits int32
	srv := withManifestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&hits, 1)
		if n < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(validManifestBody))
	}))

	m, err := FetchManifestCtx(context.Background())
	if err != nil {
		t.Fatalf("FetchManifestCtx: %v (expected success after retries)", err)
	}
	if len(m) != 2 {
		t.Fatalf("len(Manifest) = %d, want 2", len(m))
	}
	if got := atomic.LoadInt32(&hits); got != 3 {
		t.Fatalf("server hits = %d, want 3 (two 503s + one 200)", got)
	}
	_ = srv
}

// TestFetchManifestCtx_Permanent4xxReturnsFast asserts that a
// permanent 4xx (404) is NOT retried — we'd just waste cycles
// hitting the same broken endpoint.
func TestFetchManifestCtx_Permanent4xxReturnsFast(t *testing.T) {
	withEmptyCache(t)
	var hits int32
	srv := withManifestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusNotFound)
	}))

	_, err := FetchManifestCtx(context.Background())
	if err == nil {
		t.Fatal("expected error on 404, got nil")
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("server hits = %d, want 1 (no retry on permanent 4xx)", got)
	}
	_ = srv
}

// TestFetchManifestCtx_ContextCancelMidFlight cancels the context
// while the server is sleeping. The fetch must return promptly with a
// context-canceled error and NOT retry (the cancel raced past the
// retry gate). This is the regression test for the "Ctrl-C during
// nodeup upgrade hangs forever" failure mode.
func TestFetchManifestCtx_ContextCancelMidFlight(t *testing.T) {
	withEmptyCache(t)
	srv := withManifestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block until the client disconnects. When the client cancels,
		// the connection is dropped and r.Context() fires — we just
		// wait on the channel directly. (gosimple flags single-case
		// select as S1000; the direct receive is clearer anyway.)
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		// Give the request time to land in flight, then cancel.
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, err := FetchManifestCtx(ctx)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error after ctx cancel, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled in chain", err)
	}
	// FetchManifestCtx must not stall after cancellation. 1s is an
	// overestimate (real CI finishes in <100ms) but conservative enough
	// not to flake on a busy runner.
	if elapsed > 1*time.Second {
		t.Errorf("FetchManifestCtx returned after %v; should be near-instant after cancel", elapsed)
	}
}

// TestFetchManifestCtx_RequestRetriesAreBounded asserts
// fetchManifestWithRetry gives up after maxFetchAttempts even when
// the server keeps returning 5xx. We can't shrink the package const,
// but we can monkey-patch it by constructing the call against a
// server that always 503s and asserting the attempt count is bounded.
func TestFetchManifestCtx_RequestRetriesAreBounded(t *testing.T) {
	withEmptyCache(t)
	var hits int32
	srv := withManifestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusBadGateway)
	}))

	_, err := FetchManifestCtx(context.Background())
	if err == nil {
		t.Fatal("expected error after exhausting retries, got nil")
	}
	if got := atomic.LoadInt32(&hits); got != maxFetchAttempts {
		t.Fatalf("server hits = %d, want maxFetchAttempts (%d)", got, maxFetchAttempts)
	}
	_ = srv
}
