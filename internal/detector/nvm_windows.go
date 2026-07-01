//go:build windows

package detector

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"

	"github.com/dipto0321/nodeup/internal/platform"
)

// NVMWindows is the Windows-only nvm-windows implementation
// (https://github.com/coreybutler/nvm-windows). Unlike unix nvm,
// nvm-windows is a real Go binary (nvm.exe) — not a shell function —
// so we can invoke it like any other command. No source-this-script
// tricks required.
//
// nvm-windows stores installed versions under $NVM_HOME (e.g.,
// C:\Users\<user>\AppData\Roaming\nvm) and exposes the active version
// through $NVM_SYMLINK (e.g., C:\nvm4w\nodejs → versioned dir). Both
// env vars are set by the upstream installer and survive across
// shells, which is the same model asdf uses for $ASDF_DIR.
//
// Detection accepts any of:
//   - the nvm.exe binary is on PATH
//   - $NVM_HOME env var is set (the upstream install root)
//   - $NVM_SYMLINK env var is set (the active symlink dir)
//
// Phase 1 implements the detection surface only:
//   - Detect       : PATH lookup OR $NVM_HOME env OR $NVM_SYMLINK env
//   - Version      : `nvm version`, parsed (bare semver like "1.1.12"
//     — the literal NvmVersion build-time constant is printed via
//     fmt.Println in the dispatcher)
//   - ListInstalled: `nvm list`, parsed for installed Node versions
//     (lines look like "  * X.Y.Z (Currently using <arch>-bit
//     executable)" for the current version, "    X.Y.Z" for others,
//     and "No installations recognized." when the install is empty)
//
// Mutation methods (Install, Uninstall, Use, SetDefault,
// GlobalNpmPrefix) return an explicit "not implemented" error so
// callers can detect them at runtime instead of getting a silent
// zero-value result. They will be filled in when the upgrade command
// (Phase 4) needs to mutate state.
type NVMWindows struct{}

// NewNVMWindows constructs a fresh nvm-windows detector.
func NewNVMWindows() *NVMWindows { return &NVMWindows{} }

func (n *NVMWindows) Name() string { return "nvm-windows" }

// runShell (declared in fnm.go) is the package-level seam used by
// NVMWindows to invoke the `nvm` binary. nvm-windows is a real binary
// (unlike unix NVM, which is a shell function). Tests overwrite
// runShell to capture arguments and return canned output without
// spawning a subprocess. Production code never reassigns it.

// ErrNVMWindowsNotImplemented is returned by NVMWindows mutation
// methods that have not yet been implemented in Phase 1 (Install,
// Uninstall, Use, SetDefault, GlobalNpmPrefix). Returning this error
// instead of a zero value lets callers distinguish "I haven't done
// it yet" from "user passed a bad version" via errors.Is.
var ErrNVMWindowsNotImplemented = errors.New("nvm-windows mutation commands not yet implemented")

// nvmWindowsRoot returns the nvm-windows data root — where installed
// versions and settings live. Resolution order:
//  1. $NVM_HOME environment variable (the official override set by
//     the upstream installer)
//  2. "" — we deliberately do NOT fall back to a hard-coded
//     %APPDATA%\nvm path because that varies between Windows
//     releases and nodeup should not pretend to know where a given
//     user's installer dropped the files.
//
// Returns "" if neither can be resolved. Callers must treat "" as
// "nvm-windows not installed" — only the env var counts, since the
// binary might be on PATH while pointing at a moved install (rare
// but observed).
func nvmWindowsRoot() string {
	if r := strings.TrimSpace(os.Getenv("NVM_HOME")); r != "" {
		return r
	}
	return ""
}

// nvmWindowsSymlink returns the active-version symlink directory
// (typically C:\nvm4w\nodejs). Set by the upstream installer; users
// can override via the NVM_SYMLINK environment variable. Returns ""
// when unset — callers must treat that as "nvm-windows not
// installed".
func nvmWindowsSymlink() string {
	return strings.TrimSpace(os.Getenv("NVM_SYMLINK"))
}

// Detect returns true when nvm-windows appears to be installed. We
// accept any of:
//  1. the nvm.exe binary is on PATH (via platform.LookupManagerBinary
//     — it adds the .exe suffix automatically on Windows), OR
//  2. $NVM_HOME env var is set (user has explicitly pointed us at
//     an install root), OR
//  3. $NVM_SYMLINK env var is set (the upstream installer sets both
//     together, but either one alone is a strong signal)
//
// Per the Manager contract, Detect MUST be cheap — none of these
// branches spawn a subprocess.
func (n *NVMWindows) Detect() bool {
	if platform.LookupManagerBinary("nvm") != "" {
		return true
	}
	if nvmWindowsRoot() != "" {
		return true
	}
	if nvmWindowsSymlink() != "" {
		return true
	}
	return false
}

// Version returns nvm-windows' own version string, e.g. "1.1.12".
//
// Per the upstream dispatcher (src/nvm.go), the case for "version"
// (and its aliases "v", "-v", "--version", "-version", "--v") runs
//
//	fmt.Println(NvmVersion)
//
// where NvmVersion is a Go string variable set at build time via
// ldflags (default: "1.1.12"). The output is a bare semver token
// followed by a single newline — no "v" prefix, no branding.
//
// We take the first whitespace-separated token from stdout and
// strip an optional "v" prefix defensively in case a fork re-adds
// one. We do NOT validate semver here — the upstream constant is a
// plain version string today, but a future fork could emit a
// git-describe-style "1.1.12-4-gabc1234" which still parses as
// semver. Returning the raw token matches the other manager
// detectors (asdf, nodenv, mise, n).
func (n *NVMWindows) Version() (string, error) {
	res, err := runShell(context.Background(), "nvm", "version")
	if err != nil {
		return "", fmt.Errorf("nvm version: %w", err)
	}
	return parseNVMWindowsVersion(res.Stdout)
}

// parseNVMWindowsVersion extracts the version token from `nvm
// version` output. Exposed (lowercase) for direct unit testing.
//
// Real observed output (nvm-windows 1.1.12):
//
//	$ nvm version
//	1.1.12
//
// (Trailing newline from fmt.Println.)
//
// Some forks prepend "v" (e.g., "v1.1.12") — we strip it defensively.
//
// Empty stdout → error. This can happen if the binary is on PATH
// but cannot read its own embedded metadata (very rare; usually a
// corrupted install). Either way, an empty version string is not
// actionable for callers, so we surface it as an error rather than
// silently returning "".
func parseNVMWindowsVersion(stdout string) (string, error) {
	out := strings.TrimSpace(stdout)
	if out == "" {
		return "", errors.New("nvm version returned empty output")
	}
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return "", errors.New("nvm version returned no tokens")
	}
	// Take the first token and strip an optional "v" prefix.
	return strings.TrimPrefix(fields[0], "v"), nil
}

// ListInstalled returns every Node.js version nvm-windows has
// installed, sorted ascending. Source: `nvm list` (alias `nvm ls`).
//
// Per the upstream source (src/nvm.go list() function), the installed
// branch emits a leading blank line, then for each installed version:
//
//	\n
//	  * X.Y.Z (Currently using 64-bit executable)   <- the active one
//	    X.Y.Z                                       <- the rest
//
// or, when nothing is installed:
//
//	No installations recognized.
//
// The "v" prefix that nvm-windows stores internally is stripped via
// regex before printing (`regexp.MustCompile("v").ReplaceAllString`
// replaces every "v" with "" — which can in pathological cases
// strip a "v" out of the middle of a version, but real-world Node
// semver never contains a "v" between digits, so the substitution is
// safe in practice). We re-strip a leading "v" defensively in case a
// fork skips that step.
//
// The current-version line carries a trailing " (Currently using
// <arch>-bit executable)" suffix where <arch> is "32" or "64"
// (upstream hard-codes those two values in the format string; arm64
// is supported but the printed marker stays 64 on 64-bit Windows).
// We strip everything after the first whitespace after the digits
// before handing the token to semver.
//
// There is no "system" sentinel line (unlike nodenv) — nvm-windows
// is exclusively a managed install.
func (n *NVMWindows) ListInstalled() ([]semver.Version, error) {
	res, err := runShell(context.Background(), "nvm", "list")
	if err != nil {
		return nil, fmt.Errorf("nvm list: %w", err)
	}
	return parseNVMWindowsInstalled(res.Stdout)
}

// parseNVMWindowsInstalled turns raw `nvm list` output into a
// sorted-ascending []semver.Version. Exposed (lowercase) for direct
// unit testing.
//
// Lines look like:
//
//   - 18.20.4 (Currently using 64-bit executable)
//     20.11.1
//     22.5.0
//
// or, when empty:
//
//	No installations recognized.
//
// The leading blank line that upstream emits is just whitespace and
// gets trimmed to empty.
//
// We:
//  1. skip blank lines and the "No installations recognized."
//     sentinel
//  2. find the first digit and take everything from there to the
//     next whitespace (drops the " (Currently using <arch>-bit
//     executable)" suffix on the current-version line)
//  3. hand the remainder to semver.NewVersion
//  4. skip lines that don't parse (forward-compat for future
//     metadata formats)
//
// Returns a non-nil empty slice when no parseable versions are
// present — callers rely on this for "nvm-windows installed,
// nothing managed yet".
func parseNVMWindowsInstalled(stdout string) ([]semver.Version, error) {
	versions := make([]semver.Version, 0)
	for _, raw := range strings.Split(stdout, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		// Filter the "No installations recognized." sentinel.
		// We compare the trimmed prefix because upstream prints
		// it on its own line; we don't need exact-match.
		if strings.HasPrefix(line, "No installations recognized") {
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
		// The current-version line ends with " (Currently using
		// <arch>-bit executable)". We don't want that in the
		// semver token, so stop at the first whitespace after
		// the digits.
		if sp := strings.IndexFunc(verStr, func(r rune) bool {
			return r == ' ' || r == '\t'
		}); sp >= 0 {
			verStr = verStr[:sp]
		}
		// Defensive: strip a leading "v" in case a fork skipped
		// the regex substitution. Upstream's "1.1.12" build of
		// nvm-windows strips the "v" itself, so this is normally
		// a no-op.
		verStr = strings.TrimPrefix(verStr, "v")
		v, err := semver.NewVersion(verStr)
		if err != nil {
			// Skip unparseable lines rather than aborting the
			// whole list. Forward-compatibility for new
			// metadata formats nvm-windows might add.
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

// --- Mutation stubs -----------------------------------------------------
//
// Install, Uninstall, Use, SetDefault, and GlobalNpmPrefix return
// ErrNVMWindowsNotImplemented. They will be filled in when the
// upgrade command (Phase 4) needs to mutate state. Returning an
// explicit sentinel error (rather than nil) makes "not implemented"
// provably distinguishable from "succeeded".
//
// Current() also returns ErrNVMWindowsNotImplemented — nvm-windows
// has a `nvm current` subcommand but it's notoriously flaky on
// newer builds (returns "" or "Unknown" with non-zero exit). The
// cleanup prompt treats Current() errors as "active version
// unknown" and proceeds without exclusion, so the sentinel is
// fine here.

func (n *NVMWindows) Install(ver semver.Version) error    { return ErrNVMWindowsNotImplemented }
func (n *NVMWindows) Uninstall(ver semver.Version) error  { return ErrNVMWindowsNotImplemented }
func (n *NVMWindows) Use(ver semver.Version) error        { return ErrNVMWindowsNotImplemented }
func (n *NVMWindows) SetDefault(ver semver.Version) error { return ErrNVMWindowsNotImplemented }
func (n *NVMWindows) GlobalNpmPrefix(ver semver.Version) (string, error) {
	return "", ErrNVMWindowsNotImplemented
}

// Current is not implemented for nvm-windows. Returning the
// sentinel error lets the upgrade prompt treat the active version
// as unknown and proceed without excluding it from the cleanup
// candidates.
func (n *NVMWindows) Current() (semver.Version, error) {
	return semver.Version{}, ErrNVMWindowsNotImplemented
}
