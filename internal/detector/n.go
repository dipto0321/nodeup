package detector

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"

	"github.com/dipto0321/nodeup/internal/platform"
)

// N is the tj/n implementation (https://github.com/tj/n) — a simple,
// no-subshell Node.js version manager distributed via npm. Unlike
// NVM (a shell function) or Volta (a Rust binary), n is a bash
// script that drops each version into $N_PREFIX/n/versions/<v>/
// and symlinks a single "active" version into $N_PREFIX/bin/.
//
// Note: n is documented as Unix-only. The README explicitly states
// it does not work in native Windows shells (PowerShell, Git for
// Windows BASH, or Cygwin). On Windows the user must use WSL or
// fall back to NVMWindows / Nodenv / Volta. We still implement
// the detector uniformly — Detect() on Windows simply returns
// false when there's no `n.exe` on PATH.
//
// Detection is intentionally simple:
//   - binary on PATH (`n`) via platform.LookupManagerBinary
//
// We do NOT check $N_PREFIX here for the same reason as Mise: the
// CLI is the authoritative source. Without the binary, we cannot
// query installed versions or invoke mutations.
//
// Phase 1 implements the detection surface only:
//   - Detect       : PATH lookup (platform.LookupManagerBinary)
//   - Version      : `n --version`, parsed (returns the script's
//     VERSION line verbatim — typically bare "10.2.0" with no "v"
//     prefix)
//   - ListInstalled: `n ls`, parsed from "node/<semver>" lines
//
// Mutation methods (Install, Uninstall, Use, SetDefault,
// GlobalNpmPrefix) return an explicit "not implemented" error so
// callers can detect them at runtime instead of getting a silent
// zero-value result.
type N struct{}

// NewN constructs a fresh n detector.
func NewN() *N { return &N{} }

func (n *N) Name() string { return "n" }

// runShell (declared in fnm.go) is the package-level seam used by
// N to invoke the `n` binary. Tests overwrite it to capture
// arguments and return canned output without spawning a
// subprocess. Production code never reassigns it.

// ErrNNotImplemented is returned by N mutation methods that have
// not yet been implemented in Phase 1 (Install, Uninstall, Use,
// SetDefault, GlobalNpmPrefix). Returning this error instead of a
// zero value lets callers distinguish "I haven't done it yet"
// from "user passed a bad version" via errors.Is.
var ErrNNotImplemented = errors.New("n mutation commands not yet implemented")

// Detect returns true when `n` appears to be installed — i.e., the
// binary is on PATH.
//
// We deliberately do NOT check $N_PREFIX alone: $N_PREFIX points
// at a directory layout, but the actual `n` script lives under
// $N_PREFIX/bin/n. If that script is not on PATH the user cannot
// invoke `n ls` / `n install` / etc., so the directory alone is
// not a usable install signal. PATH lookup is the only branch.
//
// Note: this is the inverse of asdf's Detect() which accepts
// either PATH OR the data dir. The asymmetry is intentional —
// asdf is a binary everywhere, while n is a binary only when its
// bin/ dir is on PATH.
//
// Per the Manager contract, Detect MUST be cheap — exec.LookPath
// is a single stat walk.
func (n *N) Detect() bool {
	return platform.LookupManagerBinary("n") != ""
}

// Version returns n's own version string.
//
// Per the upstream script (bin/n, display_n_version()), `n
// --version` and `n -V` both invoke `echo "$VERSION" && exit 0`.
// The VERSION variable is set at the top of the script (e.g.,
// `VERSION="10.2.0"`), so the output is the bare semver followed
// by a newline. No "v" prefix, no trailing metadata.
//
// We delegate parsing to parseNVersion for testability. The
// function is defensive against:
//
//   - trailing newline (always present from echo)
//   - leading/trailing whitespace (rare but possible if the
//     user pipes through xargs or similar)
//   - an optional "v" prefix (none observed upstream, but some
//     forks and pre-release builds prepend it)
//
// We deliberately do NOT validate semver here — `n`'s version
// line is a script-internal constant, and future upstream could
// switch to CalVer (matching the mise/mise pattern) without
// breaking the contract. Consistent with parseASDFVersion /
// parseMiseVersion.
func (n *N) Version() (string, error) {
	res, err := runShell(context.Background(), "n", "--version")
	if err != nil {
		return "", fmt.Errorf("n --version: %w", err)
	}
	return parseNVersion(res.Stdout)
}

// parseNVersion extracts the version token from `n --version`
// output. Exposed (lowercase) for direct unit testing.
//
// Real observed output (n 10.2.0):
//
//	10.2.0
//
// The script does `echo "$VERSION" && exit 0`, so the output is
// a single line followed by a newline. We split on whitespace,
// take the first token, and strip an optional "v" prefix.
func parseNVersion(stdout string) (string, error) {
	out := strings.TrimSpace(stdout)
	if out == "" {
		return "", errors.New("n --version returned empty output")
	}
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return "", errors.New("n --version returned no tokens")
	}
	return strings.TrimPrefix(fields[0], "v"), nil
}

// ListInstalled returns every Node.js version n has installed,
// sorted ascending. Source: `n ls`.
//
// Per the upstream script (bin/n, display_versions_paths), `n ls`
// internally runs:
//
//	find "$CACHE_DIR" -maxdepth 2 -type d \
//	  | sed 's|'"$CACHE_DIR"'/||g' \
//	  | grep -E "/[0-9]+\.[0-9]+\.[0-9]+" \
//	  | sed 's|/|.|' \
//	  | sort -k 1,1 -k 2,2n -k 3,3n -k 4,4n -t . \
//	  | sed 's|\.|/|'
//
// where $CACHE_DIR defaults to $N_PREFIX/n/versions (or
// $N_CACHE_PREFIX/n/versions if set).
//
// The output is a list of "node/<version>" lines, one per
// installed version. The grep regex requires "MAJOR.MINOR.PATCH"
// — so nightly, lts aliases, and other non-semver specifiers are
// filtered upstream before we ever see them.
//
// The sort key is numeric on the three semver components, so the
// list comes back pre-sorted by n itself. We re-sort defensively
// in case upstream's sort ever changes (the cost is negligible).
//
// Note: `n ls` exits with code 0 even when no versions are
// installed — in that case the find produces no output and we
// get an empty stdout. We map that to an empty (non-nil) slice
// rather than an error, matching asdf/mise behavior.
//
// Note: n does not have a "system" sentinel — we don't filter
// for one.
func (n *N) ListInstalled() ([]semver.Version, error) {
	res, err := runShell(context.Background(), "n", "ls")
	if err != nil {
		return nil, fmt.Errorf("n ls: %w", err)
	}
	return parseNInstalled(res.Stdout)
}

// parseNInstalled turns raw `n ls` output into a sorted-
// ascending []semver.Version. Exposed (lowercase) for direct unit
// testing.
//
// Expected input format (one entry per line):
//
//	node/20.11.1
//	node/22.5.0
//
// Lines that don't match the expected "tool/version" shape (e.g.,
// `node` alone, blank lines, or output from a future n version
// that adds metadata) are skipped rather than aborting the whole
// list. Forward-compatibility for upstream formatting changes.
//
// We use `path.Base` semantics (split on "/" and take the last
// element) so the parser is robust to:
//   - tool name prefixes other than "node" (mise's plugin model
//     could theoretically apply to n via wrapper scripts)
//   - extra path components in a hypothetical future n layout
//
// Returns a non-nil empty slice when no parseable versions are
// present — callers rely on this for "n installed, nothing
// managed yet".
func parseNInstalled(stdout string) ([]semver.Version, error) {
	versions := make([]semver.Version, 0)
	for _, raw := range strings.Split(stdout, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		// Extract the last "/" component. n emits "node/<v>", so
		// the version is the part after the slash. Using
		// strings.LastIndex is more robust than strings.SplitN
		// because it tolerates paths with more components.
		slash := strings.LastIndex(line, "/")
		if slash < 0 || slash == len(line)-1 {
			// No slash, or trailing slash with no version after
			// it — not a version line.
			continue
		}
		verStr := line[slash+1:]
		v, err := semver.NewVersion(verStr)
		if err != nil {
			// Skip unparseable versions rather than aborting the
			// whole list. Forward-compatibility for new metadata
			// formats n might add.
			continue
		}
		versions = append(versions, *v)
	}
	// n already sorts upstream, but re-sort defensively in case
	// the sort algorithm ever changes. Cost is negligible for
	// typical install counts (< 20).
	sort.Slice(versions, func(i, j int) bool {
		return versions[i].Compare(&versions[j]) < 0
	})
	return versions, nil
}

// --- Mutation methods ----------------------------------------------------
//
// Install, Uninstall, Use, SetDefault, GlobalNpmPrefix, and Current
// for n all shell out to either the `n` binary or to the active
// node binary through runShell.
//
// Important n specifics:
//   - Install / Use take a bare `<v>`: `n install <v>`, `n <v>`.
//   - Uninstall takes a bare `<v>`: `n uninstall <v>`.
//   - SetDefault is implicit: `n` always uses the latest installed
//     version as the active one. There's no "default" command.
//     We return nil (no-op) so callers can invoke SetDefault
//     uniformly without special-casing n.
//   - GlobalNpmPrefix follows n's on-disk layout: each version is
//     in $N_PREFIX/n/versions/node/<v>/ with the global prefix
//     at .../lib/node_modules.
//   - Current is derived from `<N_PREFIX>/bin/node --version` —
//     n's `activate` copies the active node binary into that
//     path, so its `--version` reports the active version. We
//     deliberately do NOT use `n current`: that string falls
//     through to upstream's default dispatch arm which calls
//     `install`, side-effecting a download of the latest
//     version. See #59.

// Install runs `n install <v>`. n auto-uses the newly-installed
// version, so there's no separate "default" step.
func (n *N) Install(ver semver.Version) error {
	res, err := runShell(context.Background(), "n", "install", ver.String())
	if err != nil {
		return fmt.Errorf("n install %s: %w", ver, err)
	}
	_ = res
	return nil
}

// Uninstall runs `n uninstall <v>`. n refuses to uninstall the
// active version (similar to fnm/nvm) — callers must `n <other>`
// first to switch active versions.
func (n *N) Uninstall(ver semver.Version) error {
	res, err := runShell(context.Background(), "n", "uninstall", ver.String())
	if err != nil {
		return fmt.Errorf("n uninstall %s: %w", ver, err)
	}
	_ = res
	return nil
}

// Use runs `n <v>` (the bare-form invocation) to switch the active
// version in the current shell. This sets the symlink in
// $N_PREFIX/bin/ via the n script's symlink_update logic.
func (n *N) Use(ver semver.Version) error {
	res, err := runShell(context.Background(), "n", ver.String())
	if err != nil {
		return fmt.Errorf("n %s: %w", ver, err)
	}
	_ = res
	return nil
}

// SetDefault is a no-op for n — n auto-uses the latest installed
// version, so there's no "default" concept to set. We intentionally
// return nil so callers can invoke SetDefault uniformly without
// special-casing n.
func (n *N) SetDefault(ver semver.Version) error {
	_ = ver // no-op
	return nil
}

// GlobalNpmPrefix returns the per-version global npm directory for
// the given version. n's on-disk layout is:
//
//	$N_PREFIX/n/versions/node/<v>/lib/node_modules
//
// Default $N_PREFIX is /usr/local.
func (n *N) GlobalNpmPrefix(ver semver.Version) (string, error) {
	dir := nPrefix()
	if dir == "" {
		return "", errors.New("n: cannot resolve N_PREFIX or /usr/local")
	}
	prefix := filepath.Join(dir, "n", "versions", "node", ver.String(), "lib", "node_modules")
	if _, err := os.Stat(prefix); err != nil {
		return "", fmt.Errorf("n global npm prefix for %s (looked at %s): %w", ver, prefix, err)
	}
	return prefix, nil
}

// nPrefix returns n's install root. Resolution order:
//  1. $N_PREFIX (the official override)
//  2. $N_CACHE_PREFIX (the alternative override)
//  3. /usr/local (the documented default)
//
// Returns "" if none resolve. The default is hard-coded rather than
// computed from $HOME because the upstream install script explicitly
// targets /usr/local.
func nPrefix() string {
	if p := strings.TrimSpace(os.Getenv("N_PREFIX")); p != "" {
		return p
	}
	if p := strings.TrimSpace(os.Getenv("N_CACHE_PREFIX")); p != "" {
		// N_CACHE_PREFIX is the *cache* prefix (where downloads live),
		// but if N_PREFIX isn't set, n defaults to using N_CACHE_PREFIX
		// as the install root too. Match that behavior.
		return p
	}
	return "/usr/local"
}

// Current returns the version n currently has active for the user.
//
// Strategy: invoke `<N_PREFIX>/bin/node --version` and parse the
// output. n's `activate` function (bin/n) copies the active
// version's `bin/node` into `$N_PREFIX/bin/node`, so the active
// node binary's `--version` IS the active version — without
// requiring a separate "what's active?" subcommand.
//
// Why not `n current`? That subcommand doesn't exist as a
// first-class dispatch arm in upstream `tj/n` (bin/n's case
// statement). `n current` falls through to the default `*)`
// arm which calls `install "$1"`, which resolves "current" as
// a label equivalent to "latest" and **downloads and installs
// the newest available Node.js version**. So calling
// `N.Current()` would have silently mutated the user's machine
// on every invocation — defeating the cleanup safety net on
// every n install, every run. See #59.
//
// On platforms where `$N_PREFIX` is empty (the helper returned
// ""), or `$N_PREFIX/bin/node` is missing, or the subprocess
// fails, we return an error — the caller treats that as
// "active version unknown, don't exclude it" (the safe-by-
// convention default per the Manager interface doc).
func (n *N) Current() (semver.Version, error) {
	prefix := nPrefix()
	if prefix == "" {
		return semver.Version{}, errors.New("n: cannot resolve N_PREFIX or /usr/local")
	}
	nodeBin := filepath.Join(prefix, "bin", "node")
	// We don't pre-Stat nodeBin here — let the shell-out error
	// surface the "not installed" case in a single round-trip
	// (the runShell error message will already name the missing
	// path), rather than paying for a stat that we'd race
	// against a concurrent uninstall.
	res, err := runShell(context.Background(), nodeBin, "--version")
	if err != nil {
		return semver.Version{}, fmt.Errorf("n current: %s --version: %w", nodeBin, err)
	}
	return parseNNodeVersion(res.Stdout)
}

// parseNNodeVersion extracts the active version from
// `<N_PREFIX>/bin/node --version` output. Exposed (lowercase)
// for direct unit testing.
//
// Real observed output (node 22.11.0):
//
//	v22.11.0
//
// We take the first non-empty line, trim whitespace, strip an
// optional "v" prefix, and feed the remainder to
// semver.NewVersion. node(1) has been emitting the
// `vX.Y.Z` form since its earliest public releases, so this is
// stable across every n-supported Node version.
func parseNNodeVersion(stdout string) (semver.Version, error) {
	for _, raw := range strings.Split(stdout, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		v, err := semver.NewVersion(strings.TrimPrefix(line, "v"))
		if err != nil {
			return semver.Version{}, fmt.Errorf("n current: parse %q: %w", line, err)
		}
		return *v, nil
	}
	return semver.Version{}, errors.New("n current: <node --version> returned empty output")
}
