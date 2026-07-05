package detector

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"

	"github.com/dipto0321/nodeup/internal/platform"
)

// FNM is the Fast Node Manager implementation. See
// internal/detector/detector.go for the Manager interface contract.
//
// Detection:
//   - Detect        : cheap PATH probe via exec.LookPath
//   - Version       : `fnm --version`, parsed to drop the leading "fnm "
//   - ListInstalled : `fnm list`, parsed into sorted []semver.Version
//   - Current       : `fnm current`, parsed via parseFNMCurrent
//
// Mutations shell out to the `fnm` binary directly:
//   - Install       : `fnm install <v>`
//   - Uninstall     : `fnm uninstall <v>` (refuses if <v> is the default;
//     callers must SetDefault elsewhere first)
//   - Use           : `fnm use <v>` (current shell only)
//   - SetDefault    : `fnm default <v>` (persists for new shells)
//
// Layout queries:
//   - GlobalNpmPrefix : resolves $FNM_DIR/node-versions/<v>/installation/lib/node_modules,
//     with a fallback to the older .../lib/node_modules
//     layout used before fnm 1.30.
type FNM struct{}

// NewFNM constructs a fresh fnm detector. Returned by value so each
// detection cycle gets its own state.
func NewFNM() *FNM { return &FNM{} }

func (f *FNM) Name() string { return "fnm" }

// runShell is the package-level seam used by FNM to invoke fnm. Tests
// overwrite it to capture arguments and return canned output without
// spawning a subprocess. Production code never reassigns it.
//
// Signature matches platform.RunShell so a direct assignment works.
var runShell = platform.RunShell

// Detect returns true when an fnm executable can be located on PATH.
// Per the Manager contract, it MUST be cheap — exec.LookPath does a
// directory walk but no subprocess spawn.
func (f *FNM) Detect() bool {
	return platform.LookupManagerBinary("fnm") != ""
}

// Version returns fnm's own version string, e.g. "1.39.0". The binary
// emits "fnm 1.39.0\n" so we drop the leading "fnm " prefix and trim
// surrounding whitespace.
func (f *FNM) Version() (string, error) {
	res, err := runShell(context.Background(), "fnm", "--version")
	if err != nil {
		return "", fmt.Errorf("fnm --version: %w", err)
	}
	return parseFNMVersion(res.Stdout)
}

// parseFNMVersion extracts the version token from `fnm --version` output.
// Real observed output:
//
//	fnm 1.39.0\n
//
// We accept either "fnm X.Y.Z" or bare "X.Y.Z" (some forks omit the
// program name).
func parseFNMVersion(stdout string) (string, error) {
	out := strings.TrimSpace(stdout)
	if out == "" {
		return "", errors.New("fnm --version returned empty output")
	}
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return "", errors.New("fnm --version returned no tokens")
	}
	// If the first token is literally "fnm" take the next one.
	if fields[0] == "fnm" && len(fields) >= 2 {
		return strings.TrimSpace(fields[1]), nil
	}
	// Otherwise assume the whole string is already a version.
	return strings.TrimSpace(fields[0]), nil
}

// ListInstalled returns every Node.js version fnm has installed, sorted
// ascending. Source: `fnm list` which prints lines like:
//
//   - v24.15.0 default
//   - v25.9.0
//   - system
//
// The leading "* " is a default-marker (not per-line current); we strip
// it. The literal "system" line (which represents the system Node, not
// an fnm-managed install) is excluded — nodeup only cares about versions
// fnm actually manages.
func (f *FNM) ListInstalled(ctx context.Context) ([]semver.Version, error) {
	res, err := runShell(ctx, "fnm", "list")
	if err != nil {
		return nil, fmt.Errorf("fnm list: %w", err)
	}
	return parseFNMInstalled(res.Stdout)
}

// parseFNMInstalled turns raw `fnm list` output into a sorted-ascending
// []semver.Version. Exported (lowercase) for direct unit testing.
//
// Returns a non-nil empty slice (never nil) when no parseable versions
// are present — callers rely on this for "no versions, but no error"
// semantics (e.g., "fnm installed, nothing managed yet").
func parseFNMInstalled(stdout string) ([]semver.Version, error) {
	versions := make([]semver.Version, 0)
	for _, raw := range strings.Split(stdout, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		// Strip the leading "* " default marker if present.
		line = strings.TrimPrefix(line, "* ")
		line = strings.TrimPrefix(line, "*")
		line = strings.TrimSpace(line)

		// The first whitespace-separated token is the version or the
		// literal "system". Anything beyond it (e.g. "default", an alias
		// name) is metadata we don't need.
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		first := fields[0]
		if first == "system" {
			continue
		}
		// fnm emits "v22.11.0" (with v prefix); semver.NewVersion handles
		// that, but we normalize to be safe across forks.
		v, err := semver.NewVersion(strings.TrimPrefix(first, "v"))
		if err != nil {
			// Skip unparseable lines rather than aborting the whole list.
			continue
		}
		versions = append(versions, *v)
	}
	// semver.Collection in v3.5.0 is []*Version (pointers), so a value
	// slice doesn't satisfy it. Use sort.Slice with semver.Compare.
	sort.Slice(versions, func(i, j int) bool {
		return versions[i].Compare(&versions[j]) < 0
	})
	return versions, nil
}

// --- Mutation methods ----------------------------------------------------
//
// Install, Uninstall, Use, SetDefault, GlobalNpmPrefix, and Current all
// shell out to the `fnm` binary through runShell.
//
// All shell-outs follow the same error-wrapping convention: a non-zero
// exit becomes a wrapped error containing the captured stderr, so the
// user can see WHY the manager refused (e.g., "version not found").
// `errors.Is(err, platform.ErrNotFound)` is preserved through the
// wrapping so callers can detect a missing binary independently of
// the manager's own refusal.

// Install runs `fnm install <v>`. fnm installs into the per-user
// $FNM_DIR (or platform default) and prints the resolved path on
// success. Stderr is folded into the error on non-zero exit.
func (f *FNM) Install(v semver.Version) error {
	res, err := runShell(context.Background(), "fnm", "install", v.String())
	if err != nil {
		return fmt.Errorf("fnm install %s: %w", v, err)
	}
	_ = res // success path has no payload to consume
	return nil
}

// Uninstall runs `fnm uninstall <v>`. fnm refuses if the version is
// the current default — callers that want to remove the default must
// run SetDefault to a different version first.
func (f *FNM) Uninstall(v semver.Version) error {
	res, err := runShell(context.Background(), "fnm", "uninstall", v.String())
	if err != nil {
		return fmt.Errorf("fnm uninstall %s: %w", v, err)
	}
	_ = res
	return nil
}

// Use runs `fnm use <v>` for the current shell. Note that this only
// affects the subprocess's environment — it does NOT persist a
// per-shell version across sessions. For persistence, callers should
// use SetDefault.
func (f *FNM) Use(v semver.Version) error {
	res, err := runShell(context.Background(), "fnm", "use", v.String())
	if err != nil {
		return fmt.Errorf("fnm use %s: %w", v, err)
	}
	_ = res
	return nil
}

// SetDefault runs `fnm default <v>` to pin the version for new
// shells. This is what `nodeup upgrade` calls after Install.
func (f *FNM) SetDefault(v semver.Version) error {
	res, err := runShell(context.Background(), "fnm", "default", v.String())
	if err != nil {
		return fmt.Errorf("fnm default %s: %w", v, err)
	}
	_ = res
	return nil
}

// GlobalNpmPrefix returns the per-version global npm directory for
// the given version. fnm's on-disk layout is:
//
//	$FNM_DIR/node-versions/<v>/installation/lib/node_modules
//
// (the `installation/` subdir is the resolved symlink target — older
// fnm versions skipped it, but v1.30+ adds it consistently).
//
// We probe the conventional path rather than calling `fnm exec`
// because:
//   - the path is stable across fnm versions (any future change
//     would be a layout-breaking release)
//   - we don't want to spawn a subprocess just to compute a path
//   - the directory might exist but be unreadable — we return a
//     non-nil error so the migration step can fall back to a
//     different strategy (e.g., re-run npm install -g)
func (f *FNM) GlobalNpmPrefix(v semver.Version) (string, error) {
	dir := fnmNodeVersionDir(v)
	if dir == "" {
		return "", errors.New("fnm: cannot resolve node-versions directory")
	}
	prefix := filepath.Join(dir, "installation", "lib", "node_modules")
	if _, err := os.Stat(prefix); err != nil {
		if os.IsNotExist(err) {
			// Older fnm layout (no `installation/` subdir).
			prefix = filepath.Join(dir, "lib", "node_modules")
			if _, err2 := os.Stat(prefix); err2 != nil {
				return "", fmt.Errorf("fnm global npm prefix for %s not found at %s or %s: %w", v, prefix, filepath.Join(dir, "installation", "lib", "node_modules"), err2)
			}
		} else {
			return "", fmt.Errorf("stat fnm prefix %s: %w", prefix, err)
		}
	}
	return prefix, nil
}

// fnmNodeVersionDir returns the directory fnm stores <v> in.
// Resolution order:
//  1. $FNM_DIR (the official override)
//  2. ~/.local/share/fnm (Linux default per fnm install script)
//  3. ~/Library/Application Support/fnm (macOS default)
//
// Returns "" if none resolve. The exact filename is `<v>` with no
// "v" prefix (fnm strips it during install).
func fnmNodeVersionDir(v semver.Version) string {
	dir := fnmDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "node-versions", v.String())
}

// fnmDir mirrors the resolution logic fnm itself uses to pick its
// data dir. Resolution order:
//  1. $FNM_DIR (the official override)
//  2. $XDG_DATA_HOME/fnm (XDG default on Linux)
//  3. ~/.local/share/fnm (Linux fallback per fnm install script)
//  4. ~/Library/Application Support/fnm (macOS)
//
// Returns "" if none resolve. We deliberately don't try the Windows
// %AppData%\fnm path here — the production code only runs on the
// host OS, so we don't need cross-platform fallbacks.
//
// homeDir (declared in volta.go) is the package-level seam used so
// tests can stub out the home-dir lookup without monkey-patching
// os.UserHomeDir itself.
func fnmDir() string {
	if d := strings.TrimSpace(os.Getenv("FNM_DIR")); d != "" {
		return d
	}
	if xd := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); xd != "" {
		return filepath.Join(xd, "fnm")
	}
	home, err := homeDir()
	if err != nil || home == "" {
		return ""
	}
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library", "Application Support", "fnm")
	}
	return filepath.Join(home, ".local", "share", "fnm")
}

// Current returns the version fnm has currently selected for new
// shells — i.e., `fnm current`. The output is a bare semver like
// "v22.11.0" or "22.11.0"; fnm 1.39+ includes a "v" prefix, older
// versions don't. We strip the prefix defensively.
func (f *FNM) Current(ctx context.Context) (semver.Version, error) {
	res, err := runShell(ctx, "fnm", "current")
	if err != nil {
		return semver.Version{}, fmt.Errorf("fnm current: %w", err)
	}
	return parseFNMCurrent(res.Stdout)
}

// parseFNMCurrent extracts the version from `fnm current` output.
// Exposed (lowercase) for direct unit testing.
//
// Observed output (fnm 1.39.0):
//
//	v22.11.0
//
// Older versions emitted bare "22.11.0". We strip an optional "v"
// prefix and feed the remainder to semver.NewVersion.
func parseFNMCurrent(stdout string) (semver.Version, error) {
	out := strings.TrimSpace(stdout)
	if out == "" {
		return semver.Version{}, errors.New("fnm current returned empty output")
	}
	// Take the first whitespace-separated token (in case fnm ever
	// appends metadata, like fnm 1.40+ might).
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return semver.Version{}, errors.New("fnm current returned no tokens")
	}
	v, err := semver.NewVersion(strings.TrimPrefix(fields[0], "v"))
	if err != nil {
		return semver.Version{}, fmt.Errorf("fnm current: parse %q: %w", fields[0], err)
	}
	return *v, nil
}
