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

// Volta is the Volta implementation (https://volta.sh). Volta is a true
// binary (unlike NVM, which is a shell function), but it stores Node
// installs in a fixed on-disk layout under $VOLTA_HOME rather than
// advertising them through a CLI query. See nodeup.md §5 for the full
// detection strategy.
//
// Volta's on-disk layout (v2.x, "v4" internal layout):
//
//	$VOLTA_HOME/
//	├── bin/                      # shim_dir — volta lives here
//	├── tools/
//	│   ├── inventory/            # package-level inventory
//	│   │   ├── node/
//	│   │   ├── npm/
//	│   │   ├── pnpm/
//	│   │   └── yarn/
//	│   └── image/                # fully resolved Node installs
//	│       └── node/
//	│           ├── v20.10.0/
//	│           └── v22.5.0/
//
// As of late 2024 Volta is in maintenance mode — the README itself
// recommends migrating to mise. nodeup still supports it because many
// users haven't migrated yet, but new installations should prefer mise.
//
// Phase 1 implements the detection surface only:
//   - Detect       : PATH lookup (platform.LookupManagerBinary) OR
//     $VOLTA_HOME/bin/volta existence on disk
//   - Version      : `volta --version`, parsed to drop the leading
//     "volta " if present
//   - ListInstalled: read <voltaHome>/tools/image/node/* entries
//
// Mutation methods (Install, Uninstall, Use, SetDefault, GlobalNpmPrefix)
// return an explicit "not implemented" error so callers can detect them
// at runtime instead of getting a silent zero-value result.
type Volta struct{}

// NewVolta constructs a fresh Volta detector.
func NewVolta() *Volta { return &Volta{} }

func (v *Volta) Name() string { return "volta" }

// runShell (declared in fnm.go) is the package-level seam used by
// Volta to invoke the `volta` binary. Both FNM and Volta wrap a
// binary on PATH for the --version call. Tests overwrite it to
// capture arguments and return canned output without spawning a
// subprocess. Production code never reassigns it.
//
// listDir (declared in nvm.go) is the package-level seam used by
// Volta to enumerate $VOLTA_HOME/tools/image/node. Both NVM and
// Volta read a known directory structure rather than parsing CLI
// output for the installed list. Tests overwrite it to return
// canned DirEntry slices without touching the real filesystem.
// Production code never reassigns it.

// ErrVoltaNotImplemented is returned by Volta mutation methods that
// have not yet been implemented in Phase 1 (Install, Uninstall, Use,
// SetDefault, GlobalNpmPrefix). Returning this error instead of a zero
// value lets callers distinguish "I haven't done it yet" from "user
// passed a bad version" via errors.Is.
var ErrVoltaNotImplemented = errors.New("volta mutation commands not yet implemented")

// homeDir is the package-level seam used by Volta to resolve the user
// home directory. Tests overwrite it to isolate the test from the
// developer's actual $HOME / %USERPROFILE% — which is critical on
// Windows, where os.UserHomeDir reads %USERPROFILE% and ignores
// $HOME, so t.Setenv("HOME", ...) has no effect. Production code
// never reassigns it.
//
// Signature matches os.UserHomeDir so a direct assignment works.
var homeDir = os.UserHomeDir

// voltaHome returns the Volta install root. Resolution order:
//  1. $VOLTA_HOME environment variable (the official override)
//  2. ~/.volta (the documented default)
//
// Returns "" if neither can be resolved (e.g., HOME unset on a
// stripped-down CI runner). Callers must treat "" as "volta not
// installed".
func voltaHome() string {
	if d := strings.TrimSpace(os.Getenv("VOLTA_HOME")); d != "" {
		return d
	}
	home, err := homeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".volta")
}

// voltaBinaryPath returns the absolute path to the volta binary
// inside the install root. Returns "" if the install root can't be
// resolved. We resolve via <home>/bin/volta to support the case
// where `volta` is on PATH but PATH is not what `exec.LookPath` sees
// (e.g. inside a shell snapshot, a freshly-extracted env, etc.).
func voltaBinaryPath() string {
	home := voltaHome()
	if home == "" {
		return ""
	}
	return filepath.Join(home, "bin", "volta")
}

// Detect returns true when Volta appears to be installed. Volta is a
// real binary (unlike NVM), so we accept either:
//  1. the binary is on PATH (via platform.LookupManagerBinary), OR
//  2. the conventional <voltaHome>/bin/volta exists on disk
//
// Per the Manager contract, Detect MUST be cheap — neither branch
// spawns a subprocess.
func (v *Volta) Detect() bool {
	if platform.LookupManagerBinary("volta") != "" {
		return true
	}
	bin := voltaBinaryPath()
	if bin == "" {
		return false
	}
	// Same reasoning as NVM's Detect: collapse "not found" and
	// "permission denied" into a false result so that an unreadable
	// Volta install is treated as "not present" rather than a hard
	// error from Detect.
	_, err := os.Stat(bin)
	return err == nil
}

// Version returns Volta's own version string, e.g. "2.0.2". The
// binary emits something like "volta 2.0.2\n" (older releases) or
// just "2.0.2\n" (some patched builds); we accept both.
//
// We invoke the binary through runShell so the production binary
// lookup goes through platform.LookupManagerBinary / exec.LookPath
// rather than relying on the absolute <voltaHome>/bin/volta path
// (which only exists if Volta was installed via its installer rather
// than Homebrew).
func (v *Volta) Version() (string, error) {
	res, err := runShell(context.Background(), "volta", "--version")
	if err != nil {
		return "", fmt.Errorf("volta --version: %w", err)
	}
	return parseVoltaVersion(res.Stdout)
}

// parseVoltaVersion extracts the version token from
// `volta --version` output.
//
// Real observed output (volta 2.0.2):
//
//	volta 2.0.2
//
// We accept either "volta X.Y.Z" or bare "X.Y.Z" (defensive — the
// exact format has shifted across releases and patches). Leading
// whitespace and a trailing newline are trimmed.
func parseVoltaVersion(stdout string) (string, error) {
	out := strings.TrimSpace(stdout)
	if out == "" {
		return "", errors.New("volta --version returned empty output")
	}
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return "", errors.New("volta --version returned no tokens")
	}
	// If the first token is literally "volta" take the next one.
	if fields[0] == "volta" && len(fields) >= 2 {
		return strings.TrimSpace(fields[1]), nil
	}
	// Otherwise assume the whole first token is the version.
	return strings.TrimSpace(fields[0]), nil
}

// ListInstalled returns every Node.js version Volta has installed,
// sorted ascending. Source: directory entries under
// <voltaHome>/tools/image/node/.
//
// Each subdirectory of that directory is a full Node install. Volta
// names them like "v20.10.0" (with v prefix), but we accept both
// with and without — semver.NewVersion normalizes.
//
// Non-directory entries (which Volta doesn't currently emit, but we
// guard against) are skipped. Volta does NOT have an nvm-style
// "system" sentinel, so no special-case is needed.
func (v *Volta) ListInstalled() ([]semver.Version, error) {
	home := voltaHome()
	if home == "" {
		return nil, errors.New("volta not detected: cannot resolve VOLTA_HOME or ~/.volta")
	}
	versionsDir := filepath.Join(home, "tools", "image", "node")
	entries, err := listDir(versionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			// volta is installed but has never installed a Node version
			// — return an empty (non-nil) slice, not an error. Callers
			// distinguish this from "volta not installed" via Detect().
			return []semver.Version{}, nil
		}
		return nil, fmt.Errorf("read %s: %w", versionsDir, err)
	}
	return parseVoltaInstalledEntries(entries)
}

// parseVoltaInstalledEntries turns a list of directory entries under
// <voltaHome>/tools/image/node into a sorted-ascending
// []semver.Version. Exposed (lowercase) for direct unit testing.
//
// Returns a non-nil empty slice when no parseable versions are
// present — callers rely on this for "volta installed, nothing
// managed yet".
func parseVoltaInstalledEntries(entries []os.DirEntry) ([]semver.Version, error) {
	versions := make([]semver.Version, 0)
	for _, e := range entries {
		// Volta doesn't have a "system" sentinel — skip the check
		// that NVM needs.
		// Must be a directory — Volta stores installs as real dirs.
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		// Strip a leading "v" if present so semver.NewVersion accepts it.
		v, err := semver.NewVersion(strings.TrimPrefix(name, "v"))
		if err != nil {
			// Skip unparseable names rather than aborting the whole
			// list. Future Volta versions could add new metadata dirs
			// we don't recognize; we want to be forward-compatible.
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

// --- Mutation stubs -----------------------------------------------------
//
// Install, Uninstall, Use, SetDefault, and GlobalNpmPrefix return
// ErrVoltaNotImplemented. They will be filled in when the upgrade
// command (Phase 4) needs to mutate state. Returning an explicit
// sentinel error (rather than nil) makes "not implemented" provably
// distinguishable from "succeeded".

func (v *Volta) Install(ver semver.Version) error    { return ErrVoltaNotImplemented }
func (v *Volta) Uninstall(ver semver.Version) error  { return ErrVoltaNotImplemented }
func (v *Volta) Use(ver semver.Version) error        { return ErrVoltaNotImplemented }
func (v *Volta) SetDefault(ver semver.Version) error { return ErrVoltaNotImplemented }
func (v *Volta) GlobalNpmPrefix(ver semver.Version) (string, error) {
	return "", ErrVoltaNotImplemented
}
