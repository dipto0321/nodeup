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

// Nodenv is the rbenv-style version manager for Node.js
// (https://github.com/nodenv/nodenv). Nodenv uses shims under
// $NODENV_ROOT/shims and stores installed versions in
// $NODENV_ROOT/versions/<version>/.
//
// Nodenv is structured almost identically to asdf — a binary on PATH,
// installed versions in a known directory — so the implementation
// mirrors asdf.go closely. The difference is in the surface area:
// nodenv is purpose-built for Node (asdf is multi-language via
// plugins), and its `versions` subcommand has a `system` sentinel we
// filter out (asdf does not).
//
// Detection accepts any of:
//   - the binary is on PATH
//   - $NODENV_ROOT env var is set (the official override)
//   - the conventional ~/.nodenv directory exists on disk
//
// Phase 1 implements the detection surface only:
//   - Detect       : PATH lookup OR $NODENV_ROOT env OR ~/.nodenv on disk
//   - Version      : `nodenv --version`, parsed (strips "nodenv "
//     prefix and optional "v" on the version)
//   - ListInstalled: `nodenv versions`, parsed for installed Node
//     versions ("* <v>" for current, "  <v>" for others, "system"
//     line is filtered out)
//
// Mutation methods (Install, Uninstall, Use, SetDefault,
// GlobalNpmPrefix) return an explicit "not implemented" error so
// callers can detect them at runtime instead of getting a silent
// zero-value result.
type Nodenv struct{}

// NewNodenv constructs a fresh nodenv detector.
func NewNodenv() *Nodenv { return &Nodenv{} }

func (nd *Nodenv) Name() string { return "nodenv" }

// runShell (declared in fnm.go) is the package-level seam used by
// Nodenv to invoke the `nodenv` binary. Nodenv is a binary (unlike
// NVM, which is a shell function). Tests overwrite runShell to
// capture arguments and return canned output without spawning a
// subprocess. Production code never reassigns it.

// ErrNodenvNotImplemented is returned by Nodenv mutation methods that
// have not yet been implemented in Phase 1 (Install, Uninstall, Use,
// SetDefault, GlobalNpmPrefix). Returning this error instead of a
// zero value lets callers distinguish "I haven't done it yet" from
// "user passed a bad version" via errors.Is.
var ErrNodenvNotImplemented = errors.New("nodenv mutation commands not yet implemented")

// nodenvRoot returns the nodenv data root — where installs and
// shims live. Resolution order:
//  1. $NODENV_ROOT environment variable (the official override)
//  2. ~/.nodenv (the documented default — set by the upstream
//     dispatcher when NODENV_ROOT is unset)
//
// Returns "" if neither can be resolved (e.g., HOME unset on a
// stripped-down CI runner). Callers must treat "" as "nodenv not
// installed".
//
// Note: NODENV_ROOT is set by the nodenv dispatcher (libexec/nodenv)
// to "$HOME/.nodenv" when the user did not export it themselves. We
// do not reproduce that shell-side defaulting here because env-var
// lookup is cheaper than a homeDir() call and lets users override the
// location without touching $HOME.
func nodenvRoot() string {
	if r := strings.TrimSpace(os.Getenv("NODENV_ROOT")); r != "" {
		return r
	}
	home, err := homeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".nodenv")
}

// Detect returns true when Nodenv appears to be installed. We accept
// any of:
//  1. the binary is on PATH (via platform.LookupManagerBinary), OR
//  2. $NODENV_ROOT env var is set (user has explicitly pointed us
//     at a custom install root), OR
//  3. the conventional ~/.nodenv directory exists on disk
//
// Per the Manager contract, Detect MUST be cheap — none of these
// branches spawn a subprocess.
func (nd *Nodenv) Detect() bool {
	if platform.LookupManagerBinary("nodenv") != "" {
		return true
	}
	if strings.TrimSpace(os.Getenv("NODENV_ROOT")) != "" {
		return true
	}
	dir := nodenvRoot()
	if dir == "" {
		return false
	}
	// Collapse "not found" and "permission denied" into a false
	// result, matching asdf. An unreadable Nodenv install is
	// treated as "not present" rather than a hard error from
	// Detect. Version/ListInstalled will surface the real reason
	// when the user actually invokes them.
	_, err := os.Stat(dir)
	return err == nil
}

// Version returns Nodenv's own version string, e.g. "1.6.2".
//
// Per the upstream source (libexec/nodenv---version), the script
// echoes `nodenv ${git_revision:-$version}`, where $version is
// "1.6.2" and $git_revision is populated by `git describe` when the
// install is a git checkout (e.g., "1.6.2-12-gabc1234" — the "+"
// suffix gets rewritten by semver_compliant() to "+12.abc1234").
//
// We strip the literal "nodenv " prefix and take the first
// whitespace-separated token, then strip an optional "v" to match
// the asdf / mise / n parsing contract.
func (nd *Nodenv) Version() (string, error) {
	res, err := runShell(context.Background(), "nodenv", "--version")
	if err != nil {
		return "", fmt.Errorf("nodenv --version: %w", err)
	}
	return parseNodenvVersion(res.Stdout)
}

// parseNodenvVersion extracts the version token from `nodenv
// --version` output. Exposed (lowercase) for direct unit testing.
//
// Real observed output (nodenv 1.6.2, packaged install):
//
//	nodenv 1.6.2
//
// Git-checkout installs emit:
//
//	nodenv 1.6.2-12-gabc1234
//
// Some forks prepend "v" (e.g., "nodenv v1.6.2"). We accept all
// three shapes — strip the "nodenv " prefix, take the first
// whitespace-separated token, strip an optional "v".
//
// We deliberately do NOT validate semver here — upstream's
// git-revision format (e.g., "1.6.2-12-gabc1234") is valid semver
// after the upstream sed rewrite, but future revisions could
// introduce new shapes that don't parse. Returning the raw token
// matches asdf/mise/n policy: the caller decides what to do with
// non-semver output.
func parseNodenvVersion(stdout string) (string, error) {
	out := strings.TrimSpace(stdout)
	if out == "" {
		return "", errors.New("nodenv --version returned empty output")
	}
	// Drop the literal "nodenv " prefix. TrimPrefix is case-
	// sensitive, so "Nodenv v1.6.2" from a fork won't match —
	// that's intentional: we want to fail loudly on unexpected
	// branding rather than silently return the wrong token.
	stripped, found := strings.CutPrefix(out, "nodenv ")
	if !found {
		// No "nodenv " prefix at all. Either upstream changed
		// its format or a fork uses different branding. Either
		// way, we can't trust the output.
		return "", fmt.Errorf("nodenv --version output missing 'nodenv ' prefix: %q", out)
	}
	out = strings.TrimSpace(stripped)
	if out == "" {
		return "", errors.New("nodenv --version returned no version after prefix")
	}
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return "", errors.New("nodenv --version returned no tokens")
	}
	// Take the first token and strip an optional "v" prefix.
	return strings.TrimPrefix(fields[0], "v"), nil
}

// ListInstalled returns every Node.js version Nodenv has installed,
// sorted ascending. Source: `nodenv versions`.
//
// Per the upstream source (libexec/nodenv-versions), each line is
// formatted as either:
//
//	*18.20.4 (set by /home/user/.nodenv/version)
//	 20.11.1
//	system
//
// The marker is " * " (two spaces + star, no space after star) for
// the current version, and "   " (two spaces, no star) for the
// others. The current-version line ALSO appends " (set by
// <origin>)" — we strip that before handing the version to semver.
//
// The `system` line is filtered out — it is not a managed Node
// version and is not semver-parseable. (If we ever support a
// `--include-system` flag for upgrade plans, we'd pass it through
// separately.)
//
// There is no "v" prefix on the version. The git-revision builds
// (e.g., "1.6.2-12-gabc1234") do NOT appear here — `nodenv versions`
// only lists installed node versions, not Nodenv itself.
//
// The upstream script exits with code 1 when no versions are
// installed and emits a "Warning: no Node detected on the system"
// line to stderr. We rely on the shell invocation's exit code
// propagation through runShell for that case — an empty stdout with
// a non-zero exit becomes an error from ListInstalled rather than a
// silent empty slice. (This matches asdf's behavior with an empty
// install.)
func (nd *Nodenv) ListInstalled() ([]semver.Version, error) {
	res, err := runShell(context.Background(), "nodenv", "versions")
	if err != nil {
		return nil, fmt.Errorf("nodenv versions: %w", err)
	}
	return parseNodenvInstalled(res.Stdout)
}

// parseNodenvInstalled turns raw `nodenv versions` output into a
// sorted-ascending []semver.Version. Exposed (lowercase) for direct
// unit testing.
//
// Lines look like:
//
//	*18.20.4 (set by /home/user/.nodenv/version)
//	 20.11.1
//	system
//
// The script also prints a blank line when no versions are present
// (along with the stderr warning we filter at the shell layer).
//
// We:
//  1. skip blank lines and the "system" sentinel
//  2. find the first digit and take everything from there to the
//     next whitespace (drops the " (set by ...)" suffix that
//     current-version lines carry)
//  3. hand the remainder to semver.NewVersion
//  4. skip lines that don't parse (forward-compat for future
//     metadata formats)
//
// Returns a non-nil empty slice when no parseable versions are
// present — callers rely on this for "nodenv installed, nothing
// managed yet".
func parseNodenvInstalled(stdout string) ([]semver.Version, error) {
	versions := make([]semver.Version, 0)
	for _, raw := range strings.Split(stdout, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		// Filter the "system" sentinel. It's not a managed
		// version and shouldn't appear in upgrade plans.
		if line == "system" {
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
		// The current-version line ends with " (set by
		// <origin>)". We don't want that in the semver token,
		// so stop at the first whitespace after the digits.
		// Asdf doesn't emit this suffix, which is why the asdf
		// parser can hand the raw slice to semver.NewVersion.
		if sp := strings.IndexFunc(verStr, func(r rune) bool {
			return r == ' ' || r == '\t'
		}); sp >= 0 {
			verStr = verStr[:sp]
		}
		v, err := semver.NewVersion(verStr)
		if err != nil {
			// Skip unparseable lines rather than aborting the
			// whole list. Forward-compatibility for new
			// metadata formats Nodenv might add.
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
// ErrNodenvNotImplemented. They will be filled in when the upgrade
// command (Phase 4) needs to mutate state. Returning an explicit
// sentinel error (rather than nil) makes "not implemented" provably
// distinguishable from "succeeded".

func (nd *Nodenv) Install(ver semver.Version) error    { return ErrNodenvNotImplemented }
func (nd *Nodenv) Uninstall(ver semver.Version) error  { return ErrNodenvNotImplemented }
func (nd *Nodenv) Use(ver semver.Version) error        { return ErrNodenvNotImplemented }
func (nd *Nodenv) SetDefault(ver semver.Version) error { return ErrNodenvNotImplemented }
func (nd *Nodenv) GlobalNpmPrefix(ver semver.Version) (string, error) {
	return "", ErrNodenvNotImplemented
}
