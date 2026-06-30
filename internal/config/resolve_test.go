package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestResolve_DefaultsOnly(t *testing.T) {
	t.Parallel()
	got := Resolve(Default())
	want := Default()
	if !reflectEqual(got, want) {
		t.Errorf("Resolve(defaults) drift:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestResolve_CLIBeatsEnvBeatsFileBeatsBase(t *testing.T) {
	t.Parallel()
	// File sets: manager=fnm, track.current=true
	file := Default()
	file.Manager = "fnm"
	file.Track.Current = true
	doc, err := yaml.Marshal(file)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var root yaml.Node
	if err := yaml.Unmarshal(doc, &root); err != nil {
		t.Fatalf("parse: %v", err)
	}
	fileLayer := FileOverlayFromNode(file, &root)
	// env sets: manager=volta (overrides file), track.lts=false
	envLayer := NewOverlay()
	envLayer.SetManager("volta")
	envLayer.SetTrackLTS(false)
	// CLI sets: manager=n (overrides env)
	cliLayer := NewOverlay()
	cliLayer.SetManager("n")

	got := Resolve(Default(), fileLayer, envLayer, cliLayer)

	if got.Manager != "n" {
		t.Errorf("CLI should win: Manager = %q, want n", got.Manager)
	}
	if got.Track.LTS {
		t.Errorf("env track.lts=false should beat default true; got lts=%v", got.Track.LTS)
	}
	if !got.Track.Current {
		t.Errorf("file track.current=true should survive (no higher layer touched it); got %v", got.Track.Current)
	}
	if got.Cache.TTL != 3600 {
		t.Errorf("default cache.ttl should survive; got %d", got.Cache.TTL)
	}
}

func TestResolve_NilLayersAreSafe(t *testing.T) {
	t.Parallel()
	got := Resolve(Default(), nil, nil, nil)
	if !reflectEqual(got, Default()) {
		t.Errorf("nil layers should be no-op")
	}
}

func TestResolve_PartialOverlayDoesNotClobber(t *testing.T) {
	t.Parallel()
	// File says: manager=fnm, track.lts=false (current left at default)
	file := Default()
	file.Manager = "fnm"
	file.Track.LTS = false
	doc, err := yaml.Marshal(file)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var root yaml.Node
	if err := yaml.Unmarshal(doc, &root); err != nil {
		t.Fatalf("parse: %v", err)
	}
	fileLayer := FileOverlayFromNode(file, &root)
	// env: only TTL changed
	envLayer := NewOverlay()
	envLayer.SetCacheTTL(120)

	got := Resolve(Default(), fileLayer, envLayer)
	if got.Manager != "fnm" {
		t.Errorf("Manager = %q, want fnm (from file)", got.Manager)
	}
	if got.Track.LTS {
		t.Errorf("Track.LTS should be false (from file)")
	}
	if got.Track.Current {
		t.Errorf("Track.Current should remain false (default; file did not set it)")
	}
	if got.Cache.TTL != 120 {
		t.Errorf("Cache.TTL = %d, want 120 (from env)", got.Cache.TTL)
	}
}

func TestResolve_ExplicitZeroFromFile(t *testing.T) {
	t.Parallel()
	// A file with `track.lts: false` should be respected as a deliberate
	// user choice — Resolve(Default(), fileLayer) must yield lts=false,
	// not be hidden by the default true.
	file := Default()
	file.Track.LTS = false
	doc, _ := yaml.Marshal(file)
	var root yaml.Node
	_ = yaml.Unmarshal(doc, &root)
	fileLayer := FileOverlayFromNode(file, &root)

	got := Resolve(Default(), fileLayer)
	if got.Track.LTS {
		t.Errorf("file track.lts=false must be honored, got %v", got.Track.LTS)
	}
}

func TestOverlay_SetMethodsFlipCorrectFlags(t *testing.T) {
	t.Parallel()
	o := NewOverlay()
	o.SetManager("fnm")
	o.SetTrackLTS(true)
	o.SetTrackCurrent(true)
	o.SetPackagesMigrate(true)
	o.SetPackagesStrategy(StrategyLatest)
	o.SetPackagesSkip([]string{"yarn"})
	o.SetCleanupAuto(true)
	o.SetCleanupPrompt(true)
	o.SetCacheTTL(60)
	if !o.ManagerSet || !o.TrackLTSSet || !o.TrackCurrentSet ||
		!o.PackagesMigrateSet || !o.PackagesStrategySet || !o.PackagesSkipSet ||
		!o.CleanupAutoSet || !o.CleanupPromptSet || !o.CacheTTLSet {
		t.Errorf("not all set-flags flipped: %+v", o)
	}
}
