// Package config loads, validates, and persists nodeup's user configuration.
//
// nodeup reads its settings from a YAML file at ~/.nodeup/config.yaml.
// The schema is documented in docs/configuration.md and is the source of
// truth for every field name, default value, and validation rule below.
//
// Resolution precedence (highest first):
//
//  1. CLI flags (per-command --flag)
//  2. Environment variables (NODEUP_MANAGER, NODEUP_TRACK_LTS, ...)
//  3. Config file (~/.nodeup/config.yaml)
//  4. Hard-coded defaults
//
// When a config file does not exist, nodeup behaves as if it contained an
// empty document — defaults are applied and no error is raised. A malformed
// file IS an error: the user almost certainly has a typo to fix.
//
// This package has no dependencies on cobra or on any other nodeup
// internal package — it is safe to import from anywhere, including
// subcommands, library code, and tests.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// SchemaVersion is the YAML config schema version. Bump it when fields are
// renamed, removed, or change semantics in a way that would break an
// existing user's file.
const SchemaVersion = 1

// Config is the in-memory representation of nodeup's YAML configuration.
// Field names carry `yaml:` tags that match docs/configuration.md exactly.
// Don't add a field without updating the docs and bumping SchemaVersion.
type Config struct {
	// SchemaVersion lets us migrate older files later without guessing.
	SchemaVersion int `yaml:"schema_version"`

	// Manager pins a specific version manager. Empty string means "auto-detect".
	Manager string `yaml:"manager,omitempty"`

	Track    TrackConfig    `yaml:"track"`
	Packages PackagesConfig `yaml:"packages"`
	Cleanup  CleanupConfig  `yaml:"cleanup"`
	Cache    CacheConfig    `yaml:"cache"`
}

// TrackConfig controls which Node.js release lines nodeup follows.
// See docs/configuration.md#track for details.
type TrackConfig struct {
	LTS     bool `yaml:"lts"`
	Current bool `yaml:"current"`
}

// PackagesConfig controls global npm package handling.
// See docs/configuration.md#packages for details.
type PackagesConfig struct {
	Migrate  bool     `yaml:"migrate"`
	Strategy Strategy `yaml:"strategy"`
	// Skip lists package names that should NOT be migrated (typically
	// because they ship with the Node.js install itself).
	// Note: no `omitempty` on Skip. We need the file layer to be able to
	// express "user wrote an empty list" vs "user omitted skip entirely".
	// The former must override the default; the latter should keep the
	// default. yaml.v3 writes an empty list as `skip: []` either way, so
	// keeping the field always-present in the struct makes the round-trip
	// faithful and lets `nodeup config set packages.skip ""` actually
	// clear the list.
	Skip []string `yaml:"skip"`
}

// CleanupConfig controls the prompt that asks whether to remove old
// Node.js versions after a successful upgrade.
// See docs/configuration.md#cleanup for details.
type CleanupConfig struct {
	Auto   bool `yaml:"auto"`
	Prompt bool `yaml:"prompt"`
}

// CacheConfig controls the manifest cache TTL.
// See docs/configuration.md#cache for details.
type CacheConfig struct {
	TTL int `yaml:"ttl"` // seconds
}

// Strategy is the package-migration strategy. It is a typed string so the
// compiler catches typos in callers and YAML round-trips keep the canonical
// lowercase form.
type Strategy string

const (
	// StrategyExact re-installs the same package@version that was previously
	// installed. Safer; recommended.
	StrategyExact Strategy = "exact"

	// StrategyLatest re-installs each package name without pinning a version,
	// letting npm fetch whatever is current. Riskier — could pull breaking
	// changes — but useful for short-lived dev environments.
	StrategyLatest Strategy = "latest"
)

// IsValid reports whether s is one of the defined Strategy constants.
func (s Strategy) IsValid() bool {
	switch s {
	case StrategyExact, StrategyLatest:
		return true
	default:
		return false
	}
}

// Default returns a Config populated with the documented defaults. This is
// the answer to "what would the config be if the file did not exist?"
// Every default here MUST match the value column in
// docs/configuration.md. If you change one, change the doc.
func Default() *Config {
	return &Config{
		SchemaVersion: SchemaVersion,
		Manager:       "", // empty -> auto-detect
		Track: TrackConfig{
			LTS:     true,
			Current: false,
		},
		Packages: PackagesConfig{
			Migrate:  true,
			Strategy: StrategyExact,
			Skip:     []string{"npm", "corepack", "npx"},
		},
		Cleanup: CleanupConfig{
			Auto:   false,
			Prompt: true,
		},
		Cache: CacheConfig{
			TTL: 3600,
		},
	}
}

// Validate returns nil if c is internally consistent and conforms to the
// schema. It does NOT check whether c.Manager is a manager nodeup knows
// about — that lives in detector.ByName and is enforced only at the point
// of use, so the config package stays free of detector imports.
func (c *Config) Validate() error {
	if c == nil {
		return errors.New("config is nil")
	}
	if c.SchemaVersion != 0 && c.SchemaVersion != SchemaVersion {
		return fmt.Errorf("config schema_version=%d, this build expects %d — please run `nodeup config init` to migrate", c.SchemaVersion, SchemaVersion)
	}
	if !c.Packages.Strategy.IsValid() {
		return fmt.Errorf("packages.strategy=%q must be one of: exact, latest", c.Packages.Strategy)
	}
	if c.Cache.TTL < 0 {
		return fmt.Errorf("cache.ttl=%d must be >= 0 (0 disables caching)", c.Cache.TTL)
	}
	for i, name := range c.Packages.Skip {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("packages.skip[%d] is empty", i)
		}
	}
	return nil
}

// SetByKey updates a single dotted-path field (e.g. "track.lts",
// "packages.skip"). Returns (true, nil) if the key was recognized and set,
// (false, nil) if the key was unknown (so callers can decide whether to
// surface a "no such key" error), and (_, err) on type mismatch.
//
// Supported keys:
//
//	manager                string
//	track.lts              bool
//	track.current          bool
//	packages.migrate       bool
//	packages.strategy      string  (exact | latest)
//	packages.skip          []string (CSV on input, JSON list on output)
//	cleanup.auto           bool
//	cleanup.prompt         bool
//	cache.ttl              int     (seconds)
func (c *Config) SetByKey(key, raw string) (bool, error) {
	switch key {
	case "manager":
		c.Manager = strings.TrimSpace(raw)
		return true, nil
	case "track.lts":
		b, err := parseBool(raw)
		if err != nil {
			return true, fmt.Errorf("track.lts: %w", err)
		}
		c.Track.LTS = b
		return true, nil
	case "track.current":
		b, err := parseBool(raw)
		if err != nil {
			return true, fmt.Errorf("track.current: %w", err)
		}
		c.Track.Current = b
		return true, nil
	case "packages.migrate":
		b, err := parseBool(raw)
		if err != nil {
			return true, fmt.Errorf("packages.migrate: %w", err)
		}
		c.Packages.Migrate = b
		return true, nil
	case "packages.strategy":
		s := Strategy(strings.TrimSpace(raw))
		if !s.IsValid() {
			return true, fmt.Errorf("packages.strategy=%q must be one of: exact, latest", s)
		}
		c.Packages.Strategy = s
		return true, nil
	case "packages.skip":
		// Accept a CSV (e.g. "yarn,pnpm") so the CLI subcommand is ergonomic.
		// Empty string -> empty list, NOT defaults — `nodeup config set
		// packages.skip ""` is the way to clear the list.
		if raw == "" {
			c.Packages.Skip = nil
			return true, nil
		}
		parts := strings.Split(raw, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				out = append(out, p)
			}
		}
		c.Packages.Skip = out
		return true, nil
	case "cleanup.auto":
		b, err := parseBool(raw)
		if err != nil {
			return true, fmt.Errorf("cleanup.auto: %w", err)
		}
		c.Cleanup.Auto = b
		return true, nil
	case "cleanup.prompt":
		b, err := parseBool(raw)
		if err != nil {
			return true, fmt.Errorf("cleanup.prompt: %w", err)
		}
		c.Cleanup.Prompt = b
		return true, nil
	case "cache.ttl":
		n, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil {
			return true, fmt.Errorf("cache.ttl=%q must be an integer (seconds)", raw)
		}
		c.Cache.TTL = n
		return true, nil
	default:
		return false, nil
	}
}

// parseBool accepts the same truthy/falsy spellings the CLI flag library
// accepts, plus a few extras ("yes"/"no"). We do not import pflag here to
// keep the config package library-pure.
func parseBool(s string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "t", "true", "yes", "y", "on":
		return true, nil
	case "0", "f", "false", "no", "n", "off":
		return false, nil
	default:
		return false, fmt.Errorf("cannot parse %q as bool", s)
	}
}

// DefaultPath returns the canonical config file path, honoring overrides:
//
//  1. $NODEUP_CONFIG (full path to the file, useful for tests)
//  2. $NODEUP_HOME/config.yaml (lets power-users redirect everything)
//  3. $HOME/.nodeup/config.yaml
//
// It does NOT check whether the file exists. Callers handle missing files
// by applying defaults.
func DefaultPath() (string, error) {
	if p := strings.TrimSpace(os.Getenv("NODEUP_CONFIG")); p != "" {
		return p, nil
	}
	if home := strings.TrimSpace(os.Getenv("NODEUP_HOME")); home != "" {
		return filepath.Join(home, "config.yaml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("determine config path: %w", err)
	}
	return filepath.Join(home, ".nodeup", "config.yaml"), nil
}
