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

// ASDF is the asdf-vm implementation (https://asdf-vm.com) with the
// nodejs plugin installed. See internal/detector/detector.go for the
// Manager interface contract.
//
// ASDF is a true binary (unlike NVM). Its installed Node versions are
// queryable via `asdf list nodejs`, which is more reliable than parsing
// the on-disk layout (ASDF stores installs under
// $ASDF_DATA_DIR/installs/nodejs/<version>/).
//
// Phase 1 implements the detection surface only:
//   - Detect       : PATH lookup OR $ASDF_DIR env OR ~/.asdf on disk
//   - Version      : `asdf version`, parsed (strips optional "v" prefix)
//   - ListInstalled: `asdf list nodejs`, parsed for installed Node versions
//
// Mutation methods (Install, Uninstall, Use, SetDefault, GlobalNpmPrefix)
// return an explicit "not implemented" error so callers can detect them
// at runtime instead of getting a silent zero-value result.
type ASDF struct{}

// NewASDF constructs a fresh asdf detector.
func NewASDF() *ASDF { return &ASDF{} }

func (a *ASDF) Name() string { return "asdf" }

// runShell (declared in fnm.go) is the package-level seam used by ASDF
// to invoke the `asdf` binary. Both FNM and Volta wrap a binary on PATH
// for the --version call; ASDF follows the same pattern. Tests
// overwrite it to capture arguments and return canned output without
// spawning a subprocess. Production code never reassigns it.

// ErrASDFNotImplemented is returned by ASDF mutation methods that have
// not yet been implemented in Phase 1 (Install, Uninstall, Use,
// SetDefault, GlobalNpmPrefix). Returning this error instead of a zero
// value lets callers distinguish "I haven't done it yet" from "user
// passed a bad version" via errors.Is.
var ErrASDFNotImplemented = errors.New("asdf mutation commands not yet implemented")

// asdfDataDir returns the asdf data root — where installs and shims
// live. Resolution order:
//  1. $ASDF_DATA_DIR environment variable (the official override)
//  2. ~/.asdf (the documented default)
//
// Returns "" if neither can be resolved (e.g., HOME unset on a
// stripped-down CI runner). Callers must treat "" as "asdf not
// installed".
//
// Note: $ASDF_DIR is a separate variable pointing at the ASDF source
// checkout (for users who git-clone rather than brew install). It is
// NOT the same as the data dir. We deliberately do not use it here —
// nodeup cares about where versions are stored, not where the asdf
// source lives.
func asdfDataDir() string {
	if d := strings.TrimSpace(os.Getenv("ASDF_DATA_DIR")); d != "" {
		return d
	}
	home, err := homeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".asdf")
}

// Detect returns true when ASDF appears to be installed. We accept
// any of:
//  1. the binary is on PATH (via platform.LookupManagerBinary), OR
//  2. $ASDF_DATA_DIR env var is set (user has explicitly pointed us
//     at a custom data dir), OR
//  3. the conventional ~/.asdf directory exists on disk
//
// We use $ASDF_DATA_DIR rather than $ASDF_DIR here because that's the
// variable that proves asdf is actually configured (the dir exists with
// installs/ inside), not just source-cloned.
//
// Per the Manager contract, Detect MUST be cheap — none of these
// branches spawn a subprocess.
func (a *ASDF) Detect() bool {
	if platform.LookupManagerBinary("asdf") != "" {
		return true
	}
	if strings.TrimSpace(os.Getenv("ASDF_DATA_DIR")) != "" {
		return true
	}
	dir := asdfDataDir()
	if dir == "" {
		return false
	}
	// Same reasoning as NVM/Volta's Detect: collapse "not found" and
	// "permission denied" into a false result so that an unreadable
	// ASDF install is treated as "not present" rather than a hard
	// error from Detect. Version/ListInstalled will surface the real
	// reason when the user actually invokes them.
	_, err := os.Stat(dir)
	return err == nil
}

// Version returns ASDF's own version string, e.g. "0.18.0". Per the
// asdf-vm source (internal/cli/cli.go), `asdf version` prints a bare
// version line. Some builds prepend "v" (e.g., "v0.18.0"), some do
// not — we strip the optional "v" prefix to be defensive.
//
// Note the subcommand is `version` (not `--version`) — ASDF's CLI
// follows urfave/cli conventions.
func (a *ASDF) Version() (string, error) {
	res, err := runShell(context.Background(), "asdf", "version")
	if err != nil {
		return "", fmt.Errorf("asdf version: %w", err)
	}
	return parseASDFVersion(res.Stdout)
}

// parseASDFVersion extracts the version token from `asdf version`
// output.
//
// Real observed output (asdf 0.18.0):
//
//	v0.18.0
//
// Older releases and some forks emit bare "0.18.0". We accept both,
// stripping an optional "v" prefix. Leading whitespace and a trailing
// newline are trimmed.
func parseASDFVersion(stdout string) (string, error) {
	out := strings.TrimSpace(stdout)
	if out == "" {
		return "", errors.New("asdf version returned empty output")
	}
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return "", errors.New("asdf version returned no tokens")
	}
	// Take the first whitespace-separated token and strip an optional
	// "v" prefix. Defensive against "v0.18.0", "0.18.0", and the
	// urfave/cli version-string format which may include build
	// metadata (e.g., "0.18.0-abc1234") — semver.NewVersion will
	// reject those, but parseASDFVersion doesn't enforce semver; it
	// just returns the raw token. The caller can decide what to do
	// with a non-semver string.
	return strings.TrimPrefix(fields[0], "v"), nil
}

// ListInstalled returns every Node.js version ASDF has installed via
// the nodejs plugin, sorted ascending. Source: `asdf list nodejs`.
//
// Per the asdf-vm source (internal/cli/cli.go listLocalCommand), each
// line is formatted as either:
//
//	*18.20.4       (current version, marked with " *")
//	 20.11.1       (other installed versions, "  " indent)
//
// There is no "v" prefix on the version. Some ASDF builds may print
// a header line or error message on stderr — we ignore those because
// RunShell's Stdout is what we parse.
//
// ASDF does NOT have an nvm-style "system" sentinel — we don't filter
// for one.
func (a *ASDF) ListInstalled() ([]semver.Version, error) {
	res, err := runShell(context.Background(), "asdf", "list", "nodejs")
	if err != nil {
		return nil, fmt.Errorf("asdf list nodejs: %w", err)
	}
	return parseASDFInstalled(res.Stdout)
}

// parseASDFInstalled turns raw `asdf list nodejs` output into a
// sorted-ascending []semver.Version. Exposed (lowercase) for direct
// unit testing.
//
// Lines look like:
//
//	*18.20.4
//	 20.11.1
//
// We strip everything up to the first digit, then hand the remainder
// to semver.NewVersion. Lines that don't contain a parseable version
// (e.g., "No compatible versions installed (nodejs)") are skipped
// rather than aborting the whole list.
//
// Returns a non-nil empty slice when no parseable versions are
// present — callers rely on this for "asdf installed, nothing
// managed yet".
func parseASDFInstalled(stdout string) ([]semver.Version, error) {
	versions := make([]semver.Version, 0)
	for _, raw := range strings.Split(stdout, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		// Skip the human-readable "no versions installed" message —
		// it's not a version. Defensive against translation / format
		// changes.
		if strings.Contains(line, "No compatible versions installed") {
			continue
		}
		// Find the first digit. Everything before it is the
		// prefix decoration ("*", " *", "  ", etc.).
		idx := strings.IndexFunc(line, func(r rune) bool {
			return r >= '0' && r <= '9'
		})
		if idx < 0 {
			// No digits at all — not a version line.
			continue
		}
		verStr := line[idx:]
		v, err := semver.NewVersion(verStr)
		if err != nil {
			// Skip unparseable lines rather than aborting the
			// whole list. Forward-compatibility for new metadata
			// formats ASDF might add.
			continue
		}
		versions = append(versions, *v)
	}
	// semver.Collection in v3.5.0 is []*Version (pointers), so a
	// value slice doesn't satisfy it. Use sort.Slice with
	// semver.Compare.
	sort.Slice(versions, func(i, j int) bool {
		return versions[i].Compare(&versions[j]) < 0
	})
	return versions, nil
}

// --- Mutation methods ----------------------------------------------------
//
// Install, Uninstall, Use, SetDefault, GlobalNpmPrefix, and Current
// for ASDF all shell out to the `asdf` binary through runShell.
//
// Important ASDF specifics:
//   - ASDF takes the plugin name as a positional arg: `asdf install
//     nodejs <v>`, not `asdf install <v>`. The plugin is `nodejs`
//     (not `node` — Volta/mise use `node` but ASDF uses `nodejs`).
//   - SetDefault runs `asdf global nodejs <v>` which writes the
//     `nodejs X.Y.Z` line to ~/.tool-versions. This is what `nodeup
//     upgrade` calls after Install.
//   - Use runs `asdf shell nodejs <v>` which sets the version only
//     for the current shell (via $ASDF_NODEJS_VERSION). Persistence
//     comes from SetDefault.
//   - GlobalNpmPrefix points at the per-version global npm modules
//     directory under $ASDF_DATA_DIR/installs/nodejs/<v>/lib/node_modules.
//   - Current reads `asdf current nodejs` and parses the version
//     from the second column.

// Install runs `asdf install nodejs <v>`. ASDF's plugin name is
// `nodejs` (per asdf-nodejs plugin convention).
func (a *ASDF) Install(ver semver.Version) error {
	res, err := runShell(context.Background(), "asdf", "install", "nodejs", ver.String())
	if err != nil {
		return fmt.Errorf("asdf install nodejs %s: %w", ver, err)
	}
	_ = res
	return nil
}

// Uninstall runs `asdf uninstall nodejs <v>`. ASDF allows
// uninstalling the active version (the next shell call falls back
// to the system Node or the previous tool-versions pin).
func (a *ASDF) Uninstall(ver semver.Version) error {
	res, err := runShell(context.Background(), "asdf", "uninstall", "nodejs", ver.String())
	if err != nil {
		return fmt.Errorf("asdf uninstall nodejs %s: %w", ver, err)
	}
	_ = res
	return nil
}

// Use runs `asdf shell nodejs <v>` for the current shell. The
// `asdf shell` subcommand sets the version via the
// $ASDF_NODEJS_VERSION env var, which only the current shell sees —
// to persist across sessions, call SetDefault.
func (a *ASDF) Use(ver semver.Version) error {
	res, err := runShell(context.Background(), "asdf", "shell", "nodejs", ver.String())
	if err != nil {
		return fmt.Errorf("asdf shell nodejs %s: %w", ver, err)
	}
	_ = res
	return nil
}

// SetDefault runs `asdf global nodejs <v>` which writes the line
// `nodejs <v>` to ~/.tool-versions. This is what `nodeup upgrade`
// calls after Install.
func (a *ASDF) SetDefault(ver semver.Version) error {
	res, err := runShell(context.Background(), "asdf", "global", "nodejs", ver.String())
	if err != nil {
		return fmt.Errorf("asdf global nodejs %s: %w", ver, err)
	}
	_ = res
	return nil
}

// GlobalNpmPrefix returns the per-version global npm directory for
// the given version. ASDF's on-disk layout is:
//
//	$ASDF_DATA_DIR/installs/nodejs/<v>/lib/node_modules
//
// We probe the conventional path rather than calling `asdf exec` to
// avoid spawning a subprocess for a pure path computation. The
// directory may exist but be unreadable; in that case we return a
// wrapped error so the migration step can fall back.
func (a *ASDF) GlobalNpmPrefix(ver semver.Version) (string, error) {
	dir := asdfDataDir()
	if dir == "" {
		return "", errors.New("asdf: cannot resolve ASDF_DATA_DIR or ~/.asdf")
	}
	prefix := filepath.Join(dir, "installs", "nodejs", ver.String(), "lib", "node_modules")
	if _, err := os.Stat(prefix); err != nil {
		return "", fmt.Errorf("asdf global npm prefix for %s (looked at %s): %w", ver, prefix, err)
	}
	return prefix, nil
}

// Current returns the version ASDF currently has selected. Source:
// `asdf current nodejs`. The output looks like:
//
//	nodejs          22.11.0    (set by /home/user/.tool-versions)
//	nodejs          20.18.0    (set by /home/user/project/.tool-versions)
//	nodejs          system     (no installed version)
//
// We take the second token (the version column) and feed it to
// semver.NewVersion. We treat "system" / "None" as errors so the
// cleanup prompt doesn't try to exclude them.
func (a *ASDF) Current() (semver.Version, error) {
	res, err := runShell(context.Background(), "asdf", "current", "nodejs")
	if err != nil {
		return semver.Version{}, fmt.Errorf("asdf current nodejs: %w", err)
	}
	return parseASDFCurrent(res.Stdout)
}

// parseASDFCurrent extracts the active version from `asdf current
// nodejs` output. Exposed (lowercase) for direct unit testing.
//
// Real observed output:
//
//	nodejs          22.11.0    (set by /home/user/.tool-versions)
//	nodejs          20.18.0    (set by /home/user/project/.tool-versions)
//
// We split each line into tokens. The version is the second token
// (after `nodejs`). Anything that doesn't parse (including the
// literal "system") is returned as an error.
func parseASDFCurrent(stdout string) (semver.Version, error) {
	for _, raw := range strings.Split(stdout, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if fields[0] != "nodejs" {
			continue
		}
		if fields[1] == "system" || fields[1] == "None" {
			// ASDF prints "None" when the tool is unset. Both
			// cases mean "no active version", so we error out
			// and let the cleanup prompt skip exclusion.
			return semver.Version{}, fmt.Errorf("asdf current: %q is not a managed version", fields[1])
		}
		v, err := semver.NewVersion(fields[1])
		if err != nil {
			return semver.Version{}, fmt.Errorf("asdf current: parse %q: %w", fields[1], err)
		}
		return *v, nil
	}
	return semver.Version{}, errors.New("asdf current nodejs did not contain a version row")
}
