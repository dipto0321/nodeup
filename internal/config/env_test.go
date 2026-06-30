package config

import (
	"errors"
	"testing"
)

// envKeys are all the env vars that FromEnv reads (other than NODEUP_CONFIG
// and NODEUP_HOME, which only affect path resolution).
var envKeys = []string{
	EnvManager,
	EnvTrackLTS,
	EnvTrackCurrent,
	EnvPackagesMigrate,
	EnvPackagesStrategy,
	EnvCacheTTL,
}

// clearEnv unsets every env var the config package reads. Uses t.Setenv
// which both blanks the value and auto-restores it on test exit.
func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range envKeys {
		t.Setenv(k, "")
	}
}

func TestFromEnv_AllUnset(t *testing.T) {
	clearEnv(t)

	got, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	if got == nil || got.C == nil {
		t.Fatal("FromEnv returned nil overlay or nil config")
	}
	if got.ManagerSet || got.TrackLTSSet || got.TrackCurrentSet ||
		got.PackagesMigrateSet || got.PackagesStrategySet ||
		got.CacheTTLSet {
		t.Errorf("FromEnv with no env vars should set no flags, got flags: %+v", got)
	}
	if !emptyConfig(got.C) {
		t.Errorf("FromEnv with no env vars should leave C zero, got %#v", got.C)
	}
}

func TestFromEnv_OverlayEach(t *testing.T) {
	clearEnv(t)
	t.Setenv(EnvManager, "fnm")
	t.Setenv(EnvTrackLTS, "true")
	t.Setenv(EnvTrackCurrent, "true")
	t.Setenv(EnvPackagesMigrate, "false")
	t.Setenv(EnvPackagesStrategy, string(StrategyLatest))
	t.Setenv(EnvCacheTTL, "120")

	got, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	if got.C.Manager != "fnm" {
		t.Errorf("Manager = %q", got.C.Manager)
	}
	if !got.C.Track.LTS || !got.C.Track.Current {
		t.Errorf("track: lts=%v current=%v", got.C.Track.LTS, got.C.Track.Current)
	}
	if got.C.Packages.Migrate {
		t.Errorf("Packages.Migrate = true, want false")
	}
	if got.C.Packages.Strategy != StrategyLatest {
		t.Errorf("Packages.Strategy = %q", got.C.Packages.Strategy)
	}
	if got.C.Cache.TTL != 120 {
		t.Errorf("Cache.TTL = %d", got.C.Cache.TTL)
	}
	// Every set-flag should be flipped.
	if !got.ManagerSet || !got.TrackLTSSet || !got.TrackCurrentSet ||
		!got.PackagesMigrateSet || !got.PackagesStrategySet || !got.CacheTTLSet {
		t.Errorf("expected all set-flags flipped, got: %+v", got)
	}
}

// TestFromEnv_ExplicitFalsePreserved verifies the whole reason FromEnv
// returns an Overlay (not a *Config): a value explicitly set to "false"
// must survive into the overlay, not be lost to the zero value.
func TestFromEnv_ExplicitFalsePreserved(t *testing.T) {
	clearEnv(t)
	t.Setenv(EnvTrackLTS, "false")

	got, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	if !got.TrackLTSSet {
		t.Error("TrackLTSSet = false, want true (env var was set)")
	}
	if got.C.Track.LTS {
		t.Error("Track.LTS = true, want false (env var said false)")
	}
}

func emptyConfig(c *Config) bool {
	return c.Manager == "" && !c.Track.LTS && !c.Track.Current &&
		!c.Packages.Migrate && c.Packages.Strategy == "" && len(c.Packages.Skip) == 0 &&
		!c.Cleanup.Auto && !c.Cleanup.Prompt && c.Cache.TTL == 0
}

func TestFromEnv_BoolParseError(t *testing.T) {
	clearEnv(t)
	t.Setenv(EnvTrackLTS, "yelp")

	_, err := FromEnv()
	if err == nil {
		t.Fatal("expected error from bad NODEUP_TRACK_LTS value")
	}
	var ee *EnvError
	if !errors.As(err, &ee) || ee.Name != EnvTrackLTS {
		t.Errorf("err = %v, want EnvError{Name: %q}", err, EnvTrackLTS)
	}
}

func TestFromEnv_StrategyParseError(t *testing.T) {
	clearEnv(t)
	t.Setenv(EnvPackagesStrategy, "aggressive")

	_, err := FromEnv()
	if err == nil {
		t.Fatal("expected error from bad NODEUP_PACKAGES_STRATEGY value")
	}
}

func TestFromEnv_IntParseError(t *testing.T) {
	clearEnv(t)
	t.Setenv(EnvCacheTTL, "forever")

	_, err := FromEnv()
	if err == nil {
		t.Fatal("expected error from bad NODEUP_CACHE_TTL value")
	}
}
