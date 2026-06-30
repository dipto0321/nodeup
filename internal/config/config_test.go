package config

import (
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

func TestDefault_MatchesSpec(t *testing.T) {
	// These exact defaults are documented in docs/configuration.md.
	// If any of them drifts, the docs are wrong (or vice versa).
	want := &Config{
		SchemaVersion: 1,
		Manager:       "",
		Track:         TrackConfig{LTS: true, Current: false},
		Packages: PackagesConfig{
			Migrate:  true,
			Strategy: StrategyExact,
			Skip:     []string{"npm", "corepack", "npx"},
		},
		Cleanup: CleanupConfig{Auto: false, Prompt: true},
		Cache:   CacheConfig{TTL: 3600},
	}
	got := Default()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Default() drift:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestValidate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr bool
	}{
		{"defaults are valid", func(*Config) {}, false},
		{"bad strategy", func(c *Config) { c.Packages.Strategy = "guess" }, true},
		{"strategy latest is valid", func(c *Config) { c.Packages.Strategy = StrategyLatest }, false},
		{"negative ttl", func(c *Config) { c.Cache.TTL = -1 }, true},
		{"zero ttl is valid (disables cache)", func(c *Config) { c.Cache.TTL = 0 }, false},
		{"skip empty string is bad", func(c *Config) { c.Packages.Skip = []string{"yarn", ""} }, true},
		{"skip nil is fine", func(c *Config) { c.Packages.Skip = nil }, false},
		{"future schema is bad", func(c *Config) { c.SchemaVersion = 999 }, true},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := Default()
			tc.mutate(c)
			err := c.Validate()
			if (err != nil) != tc.wantErr {
				t.Errorf("Validate() err = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}

func TestSetByKey(t *testing.T) {
	t.Parallel()
	tests := []struct {
		key, raw string
		check    func(*Config) bool
		wantErr  bool
		wantSet  bool
	}{
		{"manager", "fnm", func(c *Config) bool { return c.Manager == "fnm" }, false, true},
		{"track.lts", "false", func(c *Config) bool { return c.Track.LTS == false }, false, true},
		{"track.lts", "yes", func(c *Config) bool { return c.Track.LTS == true }, false, true},
		{"track.current", "true", func(c *Config) bool { return c.Track.Current == true }, false, true},
		{"packages.migrate", "false", func(c *Config) bool { return c.Packages.Migrate == false }, false, true},
		{"packages.strategy", "latest", func(c *Config) bool { return c.Packages.Strategy == StrategyLatest }, false, true},
		{"packages.strategy", "bogus", nil, true, true},
		{"packages.skip", "yarn,pnpm", func(c *Config) bool { return reflect.DeepEqual(c.Packages.Skip, []string{"yarn", "pnpm"}) }, false, true},
		{"packages.skip", "", func(c *Config) bool { return c.Packages.Skip == nil }, false, true},
		{"cleanup.auto", "true", func(c *Config) bool { return c.Cleanup.Auto == true }, false, true},
		{"cleanup.prompt", "no", func(c *Config) bool { return c.Cleanup.Prompt == false }, false, true},
		{"cache.ttl", "7200", func(c *Config) bool { return c.Cache.TTL == 7200 }, false, true},
		{"cache.ttl", "absurd", nil, true, true},
		{"bogus.key", "anything", nil, false, false},
		// bad bool on a known key
		{"track.lts", "yelp", nil, true, true},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.key+"="+tc.raw, func(t *testing.T) {
			t.Parallel()
			c := Default() // fresh config per test
			set, err := c.SetByKey(tc.key, tc.raw)
			if (err != nil) != tc.wantErr {
				t.Errorf("err = %v, wantErr = %v", err, tc.wantErr)
			}
			if set != tc.wantSet {
				t.Errorf("set = %v, want %v", set, tc.wantSet)
			}
			if tc.check != nil && !tc.check(c) {
				t.Errorf("post-condition failed; got config = %#v", c)
			}
		})
	}
}

func TestDefaultPath(t *testing.T) {
	// We can't use t.Setenv across parallel tests cleanly, so we run serial.
	//
	// Paths are built with filepath.Join so the test passes on both Unix
	// ("/") and Windows ("\\") — the production code uses filepath.Join
	// too, so any platform-quoting mismatch would be a real bug.
	//
	// The HOME-fallback case is gated to non-Windows: os.UserHomeDir() on
	// Windows reads USERPROFILE, not HOME, so setting HOME via t.Setenv
	// doesn't steer the lookup. The NODEUP_CONFIG and NODEUP_HOME paths
	// still get platform-correct coverage.
	homeRoot := "/tmp/u"
	homeRoot2 := "/tmp/home"
	explicit := "/tmp/explicit/config.yaml"
	tests := []struct {
		name      string
		envConfig string
		envHome   string
		homeDir   string // sets HOME
		want      string
		skipOnWin bool
	}{
		{
			name:      "NODEUP_CONFIG wins",
			envConfig: explicit,
			want:      explicit,
		},
		{
			name:    "NODEUP_HOME used when no NODEUP_CONFIG",
			envHome: homeRoot2,
			want:    filepath.Join(homeRoot2, "config.yaml"),
		},
		{
			name:      "HOME/.nodeup/config.yaml fallback",
			homeDir:   homeRoot,
			want:      filepath.Join(homeRoot, ".nodeup", "config.yaml"),
			skipOnWin: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.skipOnWin && runtime.GOOS == "windows" {
				t.Skip("HOME env override is not honored by os.UserHomeDir() on Windows")
			}
			t.Setenv("NODEUP_CONFIG", tc.envConfig)
			t.Setenv("NODEUP_HOME", tc.envHome)
			if tc.homeDir != "" {
				t.Setenv("HOME", tc.homeDir)
			} else {
				t.Setenv("HOME", "/should-not-be-used")
			}
			got, err := DefaultPath()
			if err != nil {
				t.Fatalf("DefaultPath: %v", err)
			}
			if got != tc.want {
				t.Errorf("DefaultPath = %q, want %q", got, tc.want)
			}
		})
	}
}
