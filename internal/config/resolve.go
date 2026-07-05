package config

// Resolve merges configurations from all sources in precedence order:
//
//  1. cli    (highest priority — explicit --flag values from the user)
//  2. env    (environment variables)
//  3. file   (the loaded ~/.nodeup/config.yaml)
//  4. base   (the lowest layer — typically Default())
//
// "Merge" here means: if a field is set in the higher layer, it wins;
// otherwise the lower layer's value is kept. To distinguish "unset"
// from "explicitly false / zero", each layer is passed as an Overlay
// with per-field set-flags (see Overlay.SetXxx methods).
//
// Resolve is pure: it does not call Validate. Callers should validate
// the returned Config before persisting or acting on it.
func Resolve(base *Config, layers ...*Overlay) *Config {
	if base == nil {
		base = &Config{}
	}
	out := *base // shallow copy; sub-structs copy by value

	for _, layer := range layers {
		if layer == nil || layer.C == nil {
			continue
		}
		applyOverlay(&out, layer)
	}
	return &out
}

// applyOverlay copies every explicitly-set field from src onto dst.
func applyOverlay(dst *Config, src *Overlay) {
	if src.ManagerSet {
		dst.Manager = src.C.Manager
	}
	if src.SchemaVersionSet {
		dst.SchemaVersion = src.C.SchemaVersion
	}
	if src.TrackLTSSet {
		dst.Track.LTS = src.C.Track.LTS
	}
	if src.TrackCurrentSet {
		dst.Track.Current = src.C.Track.Current
	}
	if src.PackagesMigrateSet {
		dst.Packages.Migrate = src.C.Packages.Migrate
	}
	if src.PackagesStrategySet {
		dst.Packages.Strategy = src.C.Packages.Strategy
	}
	if src.PackagesSkipSet {
		dst.Packages.Skip = src.C.Packages.Skip
	}
	if src.CleanupAutoSet {
		dst.Cleanup.Auto = src.C.Cleanup.Auto
	}
	if src.CleanupPromptSet {
		dst.Cleanup.Prompt = src.C.Cleanup.Prompt
	}
	if src.CacheTTLSet {
		dst.Cache.TTL = src.C.Cache.TTL
	}
}

// Overlay is the per-source-layer state used by Resolve. Each SetXxx
// method marks the corresponding field as explicitly provided so
// Resolve can distinguish "user said no" from "user said nothing".
//
// Build one Overlay per source (CLI, env, file). Pass all of them to
// Resolve. The file overlay is normally built by FileOverlayFromNode
// (see internal/config/fileoverlay.go) from a loaded Config and its
// parsed yaml.Node; that form preserves "key absent" vs "key present
// with zero value" — a distinction FileOverlay alone cannot make.
type Overlay struct {
	C *Config

	// Set-flags. Each is true iff the corresponding field was
	// explicitly provided in this layer (regardless of value).
	ManagerSet          bool
	SchemaVersionSet    bool
	TrackLTSSet         bool
	TrackCurrentSet     bool
	PackagesMigrateSet  bool
	PackagesStrategySet bool
	PackagesSkipSet     bool
	CleanupAutoSet      bool
	CleanupPromptSet    bool
	CacheTTLSet         bool
}

// NewOverlay returns an empty Overlay (no fields set).
func NewOverlay() *Overlay { return &Overlay{C: &Config{}} }

// SetManager records an explicit manager override.
func (o *Overlay) SetManager(name string) {
	if o == nil || o.C == nil {
		return
	}
	o.C.Manager = name
	o.ManagerSet = true
}

// SetTrackLTS records an explicit track.lts override.
func (o *Overlay) SetTrackLTS(v bool) {
	if o == nil || o.C == nil {
		return
	}
	o.C.Track.LTS = v
	o.TrackLTSSet = true
}

// SetTrackCurrent records an explicit track.current override.
func (o *Overlay) SetTrackCurrent(v bool) {
	if o == nil || o.C == nil {
		return
	}
	o.C.Track.Current = v
	o.TrackCurrentSet = true
}

// SetPackagesMigrate records an explicit packages.migrate override.
func (o *Overlay) SetPackagesMigrate(v bool) {
	if o == nil || o.C == nil {
		return
	}
	o.C.Packages.Migrate = v
	o.PackagesMigrateSet = true
}

// SetPackagesStrategy records an explicit packages.strategy override.
func (o *Overlay) SetPackagesStrategy(s Strategy) {
	if o == nil || o.C == nil {
		return
	}
	o.C.Packages.Strategy = s
	o.PackagesStrategySet = true
}

// SetPackagesSkip records an explicit packages.skip override. Pass nil
// to mean "skip nothing" (which is still an explicit choice).
func (o *Overlay) SetPackagesSkip(skip []string) {
	if o == nil || o.C == nil {
		return
	}
	o.C.Packages.Skip = skip
	o.PackagesSkipSet = true
}

// SetCleanupAuto records an explicit cleanup.auto override.
func (o *Overlay) SetCleanupAuto(v bool) {
	if o == nil || o.C == nil {
		return
	}
	o.C.Cleanup.Auto = v
	o.CleanupAutoSet = true
}

// SetCleanupPrompt records an explicit cleanup.prompt override.
func (o *Overlay) SetCleanupPrompt(v bool) {
	if o == nil || o.C == nil {
		return
	}
	o.C.Cleanup.Prompt = v
	o.CleanupPromptSet = true
}

// SetCacheTTL records an explicit cache.ttl override (seconds).
func (o *Overlay) SetCacheTTL(seconds int) {
	if o == nil || o.C == nil {
		return
	}
	o.C.Cache.TTL = seconds
	o.CacheTTLSet = true
}
