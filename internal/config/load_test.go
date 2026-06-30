package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// tempConfigPath returns a path inside t.TempDir() for "config.yaml".
// Using t.TempDir means Go automatically cleans up.
func tempConfigPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "config.yaml")
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestLoad_MissingFileIsDefaults(t *testing.T) {
	t.Parallel()
	cfg, fromFile, err := Load("/definitely/does/not/exist/config.yaml")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if fromFile {
		t.Error("fromFile = true, want false for missing file")
	}
	if !reflectEqual(cfg, Default()) {
		t.Errorf("Load missing-file result != Default():\n got: %#v\nwant: %#v", cfg, Default())
	}
}

func TestLoad_PartialFilePreservesDefaults(t *testing.T) {
	t.Parallel()
	path := tempConfigPath(t)
	writeFile(t, path, `
manager: fnm
track:
  current: true
`)
	cfg, fromFile, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !fromFile {
		t.Error("fromFile = false, want true")
	}
	if cfg.Manager != "fnm" {
		t.Errorf("Manager = %q, want fnm", cfg.Manager)
	}
	// File set current=true; lts omitted -> default (true) must survive.
	if !cfg.Track.LTS {
		t.Errorf("Track.LTS = false, want true (default, file omitted)")
	}
	if !cfg.Track.Current {
		t.Errorf("Track.Current = false, want true (file set it)")
	}
	// packages block omitted entirely -> defaults must survive.
	if !cfg.Packages.Migrate {
		t.Errorf("Packages.Migrate = false, want true (default)")
	}
	if cfg.Cache.TTL != 3600 {
		t.Errorf("Cache.TTL = %d, want 3600 (default)", cfg.Cache.TTL)
	}
}

func TestLoad_FullFileOverride(t *testing.T) {
	t.Parallel()
	path := tempConfigPath(t)
	writeFile(t, path, `
schema_version: 1
manager: volta
track:
  lts: true
  current: true
packages:
  migrate: false
  strategy: latest
  skip: [yarn, pnpm]
cleanup:
  auto: true
  prompt: false
cache:
  ttl: 60
`)
	cfg, _, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Manager != "volta" {
		t.Errorf("Manager = %q", cfg.Manager)
	}
	if !cfg.Track.LTS || !cfg.Track.Current {
		t.Errorf("track: lts=%v current=%v, both want true", cfg.Track.LTS, cfg.Track.Current)
	}
	if cfg.Packages.Migrate {
		t.Errorf("Packages.Migrate = true, want false")
	}
	if cfg.Packages.Strategy != StrategyLatest {
		t.Errorf("Packages.Strategy = %q, want latest", cfg.Packages.Strategy)
	}
	if strings.Join(cfg.Packages.Skip, ",") != "yarn,pnpm" {
		t.Errorf("Packages.Skip = %v", cfg.Packages.Skip)
	}
	if !cfg.Cleanup.Auto || cfg.Cleanup.Prompt {
		t.Errorf("cleanup: auto=%v prompt=%v", cfg.Cleanup.Auto, cfg.Cleanup.Prompt)
	}
	if cfg.Cache.TTL != 60 {
		t.Errorf("Cache.TTL = %d, want 60", cfg.Cache.TTL)
	}
}

func TestLoad_MalformedYAMLIsError(t *testing.T) {
	t.Parallel()
	path := tempConfigPath(t)
	writeFile(t, path, `
manager: fnm
  track:
  this is: not: aligned
`)
	_, _, err := Load(path)
	if err == nil {
		t.Fatal("Load: expected error for malformed YAML, got nil")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error should mention %q, got: %v", path, err)
	}
}

func TestLoad_InvalidValueIsError(t *testing.T) {
	t.Parallel()
	path := tempConfigPath(t)
	writeFile(t, path, `
packages:
  strategy: bogus
`)
	_, _, err := Load(path)
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
}

func TestSaveAndReload(t *testing.T) {
	t.Parallel()
	path := tempConfigPath(t)
	cfg := Default()
	cfg.Manager = "fnm"
	cfg.Track.Current = true
	cfg.Cache.TTL = 120

	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// File should exist and have restrictive perms on POSIX systems.
	// Windows' os.Chmod only honors the read-only bit (everything else
	// collapses to 0666), so we only assert the strict 0600 mode on
	// non-Windows. On Windows we still want the file to *exist* and
	// round-trip — those are the platform-agnostic properties.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if runtime.GOOS != "windows" {
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("file mode = %o, want 0600", perm)
		}
	} else {
		// On Windows the temp file must at least not be read-only.
		if info.Mode().Perm()&0o200 == 0 {
			t.Errorf("file is read-only on Windows: mode = %o", info.Mode().Perm())
		}
	}

	got, _, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Manager != "fnm" || !got.Track.Current || got.Cache.TTL != 120 {
		t.Errorf("round-trip mismatch: %#v", got)
	}
}

func TestSave_RefusesInvalid(t *testing.T) {
	t.Parallel()
	path := tempConfigPath(t)
	bad := Default()
	bad.Packages.Strategy = "bogus"
	if err := Save(path, bad); err == nil {
		t.Fatal("Save accepted invalid config")
	}
	if _, err := os.Stat(path); err == nil {
		t.Errorf("Save created file despite validation failure")
	}
}

func TestSave_AtomicDoesNotLeakTemp(t *testing.T) {
	t.Parallel()
	path := tempConfigPath(t)
	if err := Save(path, Default()); err != nil {
		t.Fatalf("Save: %v", err)
	}
	dir := filepath.Dir(path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".config-") && strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("leaked temp file: %s", e.Name())
		}
	}
}

// TestSave_EmptyListClearsDefault guards the omitempty choice on Packages.Skip.
//
// Saving a config with an explicit empty Skip list must produce a file that,
// when reloaded, yields a zero-length Skip (not the default `[npm, corepack,
// npx]`). Without this guarantee, `nodeup config set packages.skip ""` (or
// authoring a hand-written config with `skip: []`) cannot actually clear the
// list — the omission-vs-zero distinction would be lost at the YAML layer.
//
// We assert both halves of the round-trip:
//  1. The serialized file actually contains `skip: []`, not an absent key.
//  2. The loaded Config has len(Packages.Skip) == 0.
func TestSave_EmptyListClearsDefault(t *testing.T) {
	t.Parallel()
	path := tempConfigPath(t)

	cfg := Default()
	cfg.Packages.Skip = nil // explicit empty (same outcome as []string{})

	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(body), "skip: []") {
		t.Errorf("file should contain literal `skip: []`, got:\n%s", body)
	}

	got, _, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got.Packages.Skip) != 0 {
		t.Errorf("len(Packages.Skip) after reload = %d, want 0 (got %v)",
			len(got.Packages.Skip), got.Packages.Skip)
	}
}

// reflectEqual is a tiny helper so we don't have to import "reflect"
// just for a single use.
func reflectEqual(a, b *Config) bool {
	if a == nil || b == nil {
		return a == b
	}
	// Compare carefully because Skip may be nil-vs-empty.
	if a.SchemaVersion != b.SchemaVersion || a.Manager != b.Manager {
		return false
	}
	if a.Track != b.Track || a.Packages.Migrate != b.Packages.Migrate ||
		a.Packages.Strategy != b.Packages.Strategy {
		return false
	}
	if a.Cleanup != b.Cleanup || a.Cache != b.Cache {
		return false
	}
	if len(a.Packages.Skip) != len(b.Packages.Skip) {
		return false
	}
	for i := range a.Packages.Skip {
		if a.Packages.Skip[i] != b.Packages.Skip[i] {
			return false
		}
	}
	return true
}
