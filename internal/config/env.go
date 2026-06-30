package config

import (
	"os"
	"strconv"
)

// Environment variable names exported as constants so tests, docs, and
// the README can refer to them by symbol instead of by string literal.
// Keep in sync with docs/configuration.md "Environment variables" section.
const (
	EnvManager          = "NODEUP_MANAGER"
	EnvTrackLTS         = "NODEUP_TRACK_LTS"
	EnvTrackCurrent     = "NODEUP_TRACK_CURRENT"
	EnvPackagesMigrate  = "NODEUP_PACKAGES_MIGRATE"
	EnvPackagesStrategy = "NODEUP_PACKAGES_STRATEGY"
	EnvCacheTTL         = "NODEUP_CACHE_TTL"

	// EnvConfig lets power users point nodeup at a different config
	// file path entirely (e.g. for tests, or for users who keep their
	// dotfiles in a non-standard location).
	EnvConfig = "NODEUP_CONFIG"

	// EnvHome lets users redirect every nodeup state file (~/.nodeup)
	// to a custom root.
	EnvHome = "NODEUP_HOME"
)

// FromEnv returns an Overlay containing ONLY the env-var-overridden keys.
// Callers pass this Overlay to Resolve() so it gets layered on top of
// file/defaults.
//
// Returning an Overlay (not a Config) is important: it lets us distinguish
// "set explicitly to false" from "unset". For example, a user who runs
// `NODEUP_TRACK_LTS=false nodeup upgrade` on a system whose config file
// says `track.lts: true` should get LTS turned OFF, not silently inherit
// the file's value. A *Config couldn't express that — the zero value
// of Track.LTS is also false, so we'd lose the "set" signal.
//
// A variable that is set but unparsable (e.g. NODEUP_TRACK_LTS="yelp")
// returns an error rather than silently ignoring it — env typos are
// frustrating to debug otherwise.
//
// A variable that is set to the empty string is treated the same as
// unset (so `unset NODEUP_FOO` and `NODEUP_FOO=""` have identical
// effect). This matches typical 12-factor / docker-compose behavior
// and keeps tests hermetic under t.Setenv(k, "").
//
// This function NEVER reads the file; use Load() for that. It exists
// so it can be unit-tested without touching disk.
func FromEnv() (*Overlay, error) {
	out := NewOverlay()
	if v, ok := os.LookupEnv(EnvManager); ok && v != "" {
		out.SetManager(v)
	}
	if v, ok := os.LookupEnv(EnvTrackLTS); ok && v != "" {
		b, err := parseBool(v)
		if err != nil {
			return nil, errEnv(EnvTrackLTS, err)
		}
		out.SetTrackLTS(b)
	}
	if v, ok := os.LookupEnv(EnvTrackCurrent); ok && v != "" {
		b, err := parseBool(v)
		if err != nil {
			return nil, errEnv(EnvTrackCurrent, err)
		}
		out.SetTrackCurrent(b)
	}
	if v, ok := os.LookupEnv(EnvPackagesMigrate); ok && v != "" {
		b, err := parseBool(v)
		if err != nil {
			return nil, errEnv(EnvPackagesMigrate, err)
		}
		out.SetPackagesMigrate(b)
	}
	if v, ok := os.LookupEnv(EnvPackagesStrategy); ok && v != "" {
		s := Strategy(v)
		if !s.IsValid() {
			return nil, errEnv(EnvPackagesStrategy, &strconv.NumError{
				Func: "Strategy",
				Num:  string(s),
				Err:  errInvalidStrategy,
			})
		}
		out.SetPackagesStrategy(s)
	}
	if v, ok := os.LookupEnv(EnvCacheTTL); ok && v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, errEnv(EnvCacheTTL, err)
		}
		out.SetCacheTTL(n)
	}
	return out, nil
}

// errInvalidStrategy is a sentinel so FromEnv's strategy-bad-error path
// doesn't have to invent one inline.
var errInvalidStrategy = stringErr("must be one of: exact, latest")

type stringErr string

func (s stringErr) Error() string { return string(s) }

// errEnv wraps an env-var parse error with the variable name so the
// caller can show exactly which variable is wrong.
func errEnv(name string, err error) error {
	return &EnvError{Name: name, Err: err}
}

// EnvError is returned when an environment variable is set but cannot
// be parsed. It implements error and supports errors.Is/As via Unwrap.
type EnvError struct {
	Name string
	Err  error
}

func (e *EnvError) Error() string {
	return e.Name + ": " + e.Err.Error()
}

func (e *EnvError) Unwrap() error { return e.Err }
