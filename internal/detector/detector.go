// Package detector discovers and abstracts Node.js version managers on the
// user's system: fnm, nvm, Volta, asdf, mise, n, nodenv, nvm-windows.
//
// The public surface is the Manager interface and the DetectAll /
// ResolveManager helpers. Each concrete manager lives in its own file
// (fnm.go, nvm.go, ...). Windows-only managers live in *_windows.go
// files with the //go:build windows tag.
//
// Detection priority (highest first):
//  1. --manager CLI flag (caller-supplied)
//  2. ~/.nodeup/config.yaml setting
//  3. environment variables (NVM_DIR, FNM_DIR, VOLTA_HOME, ASDF_DATA_DIR, ...)
//  4. binary lookup on PATH
//  5. well-known data directories
//
// If multiple managers are detected in step 3-5, the caller is prompted
// to choose one. The choice is written to the config file so subsequent
// invocations skip the prompt.
package detector

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/Masterminds/semver/v3"
)

// ErrNoManager is returned when no version manager can be detected.
var ErrNoManager = errors.New("no Node.js version manager detected")

// Manager is the abstraction every concrete manager implementation
// satisfies. See the doc comments on individual Manager methods for
// the full contract.
type Manager interface {
	// Name returns the canonical short name: "fnm", "nvm", "volta", ...
	Name() string

	// Detect returns true if this manager appears to be installed and
	// usable on the current system. It MUST be cheap (no spawning of
	// child processes beyond a single version probe).
	Detect() bool

	// Version returns the manager's own version string (e.g., "1.35.1").
	// Returns an error if the binary is missing or broken even though
	// Detect() returned true.
	Version() (string, error)

	// ListInstalled returns every Node.js version the manager currently
	// has installed, sorted ascending. Implementations should respect
	// ctx cancellation so a Ctrl-C during `nodeup upgrade` propagates
	// into the underlying `manager list` subprocess. The CLI layer
	// passes cmd.Context().
	ListInstalled(ctx context.Context) ([]semver.Version, error)

	// Install downloads and installs the given Node.js version.
	Install(v semver.Version) error

	// Uninstall removes the given Node.js version.
	Uninstall(v semver.Version) error

	// Use switches the active shell to the given version. For shell-function
	// managers like nvm, this MUST source the manager first.
	Use(v semver.Version) error

	// SetDefault marks the given version as the default for new shells.
	SetDefault(v semver.Version) error

	// GlobalNpmPrefix returns the directory where npm installs globals
	// for the given Node.js version. Used to enumerate packages.
	GlobalNpmPrefix(v semver.Version) (string, error)

	// Current returns the version that is currently active on PATH.
	// Used by the upgrade command to exclude the active version from
	// post-upgrade cleanup candidates. May return an error if the
	// manager has no way to query the active version (e.g.,
	// nvm-windows — that one returns a sentinel "not implemented"
	// error). Callers should treat errors as "active version unknown"
	// and proceed without excluding it. Implementations should respect
	// ctx cancellation; the CLI layer passes cmd.Context().
	Current(ctx context.Context) (semver.Version, error)
}

// Registry holds the list of managers nodeup knows about. It is built
// once at startup by DetectAll and consumed by the upgrade command.
type Registry struct {
	Found []Manager
}

// DetectAll probes every supported manager and returns the ones that
// appear installed. Order is the package-level priority order defined in
// Priority() below.
func DetectAll() Registry {
	candidates := All()
	var found []Manager
	for _, m := range candidates {
		if m.Detect() {
			found = append(found, m)
		}
	}

	// Stable order by priority so prompts always list in the same order.
	sort.SliceStable(found, func(i, j int) bool {
		return Priority(found[i].Name()) < Priority(found[j].Name())
	})

	return Registry{Found: found}
}

// ResolveManager picks a single manager from a registry, applying the
// optional `preferred` override (from --manager flag or config file).
//
// If preferred is non-empty:
//   - If it matches a found manager, return that.
//   - If it doesn't match but the manager exists (i.e., is installed), we
//     still use it — the user explicitly asked for it.
//   - If neither, return an error.
//
// If preferred is empty and exactly one manager is found, return it.
// If preferred is empty and multiple are found, return an error asking
// the caller to prompt the user (caller uses ResolveInteractive).
func ResolveManager(reg Registry, preferred string) (Manager, error) {
	if preferred != "" {
		for _, m := range reg.Found {
			if m.Name() == preferred {
				return m, nil
			}
		}
		// Preferred manager not detected — check if it's installed at all
		// by re-running Detect on a freshly constructed instance.
		if m, ok := ByName(preferred); ok {
			if m.Detect() {
				return m, nil
			}
			return nil, fmt.Errorf("--manager %s requested but not detected", preferred)
		}
		return nil, fmt.Errorf("unknown manager %q", preferred)
	}

	switch len(reg.Found) {
	case 0:
		return nil, ErrNoManager
	case 1:
		return reg.Found[0], nil
	default:
		return nil, fmt.Errorf(
			"multiple version managers detected: %s. Use --manager <name> or `nodeup config set manager <name>` to pick one",
			managerNames(reg.Found),
		)
	}
}

func managerNames(ms []Manager) string {
	names := make([]string, len(ms))
	for i, m := range ms {
		names[i] = m.Name()
	}
	return joinComma(names)
}

// joinComma joins strings with a comma+space. Tiny helper to avoid
// dragging in strings just for this in detector.
func joinComma(ss []string) string {
	if len(ss) == 0 {
		return ""
	}
	out := ss[0]
	for _, s := range ss[1:] {
		out += ", " + s
	}
	return out
}
