package detector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"

	"github.com/dipto0321/nodeup/internal/platform"
)

// Mise is the mise (https://mise.jdx.dev) implementation. Mise is the
// spiritual successor to asdf-vm and rtx (the project it was forked
// from) — same overall shape, but a faster, Rust-implemented CLI with
// better defaults and a richer data model.
//
// Detection is intentionally simple:
//   - binary on PATH (`mise`) via platform.LookupManagerBinary
//
// Unlike asdf, mise stores installs under
// $MISE_DATA_DIR/installs/node/<version>/, but we do NOT walk that
// tree directly — the `mise ls --json` output is authoritative (it
// accounts for active-vs-installed, symlinks, etc.) and is cheaper
// to parse.
//
// Phase 1 implements the detection surface only:
//   - Detect       : PATH lookup (platform.LookupManagerBinary)
//   - Version      : `mise --version`, parsed (strips optional "v"
//     prefix; CalVer such as "2026.6.15" is returned as-is)
//   - ListInstalled: `mise ls --installed --json node`, parsed
//     from a JSON array of JSONToolVersion-like objects
//
// Mutation methods (Install, Uninstall, Use, SetDefault, GlobalNpmPrefix)
// return an explicit "not implemented" error so callers can detect
// them at runtime instead of getting a silent zero-value result.
type Mise struct{}

// NewMise constructs a fresh mise detector.
func NewMise() *Mise { return &Mise{} }

func (m *Mise) Name() string { return "mise" }

// runShell (declared in fnm.go) is the package-level seam used by
// Mise to invoke the `mise` binary. Tests overwrite it to capture
// arguments and return canned output without spawning a subprocess.
// Production code never reassigns it.

// ErrMiseNotImplemented is returned by Mise mutation methods that
// have not yet been implemented in Phase 1 (Install, Uninstall,
// Use, SetDefault, GlobalNpmPrefix). Returning this error instead
// of a zero value lets callers distinguish "I haven't done it yet"
// from "user passed a bad version" via errors.Is.
var ErrMiseNotImplemented = errors.New("mise mutation commands not yet implemented")

// miseToolVersion mirrors a single entry in the JSON array emitted
// by `mise ls --installed --json <tool>`.
//
// Confirmed against the upstream source (src/cli/ls.rs and the
// JSONToolVersion struct in src/toolset/mod.rs). We only model the
// fields we actually consume (version); the rest are kept as
// `omitempty` pointers so missing fields don't break parsing.
//
// All fields are pointers/optional because mise populates them
// opportunistically — e.g., `requested_version` is only present
// when a non-default version was requested, `source` is only
// present when the version came from a config file rather than
// the global default, etc.
type miseToolVersion struct {
	// Version is the installed version string (e.g., "20.11.1").
	// Required: mise always populates this.
	Version string `json:"version"`
	// RequestedVersion is the original spec the user asked for
	// (e.g., "lts", "20", "node@20"). Only present when the
	// resolved Version differs from the requested spec.
	RequestedVersion string `json:"requested_version,omitempty"`
	// InstallPath is the on-disk location of the install.
	InstallPath string `json:"install_path,omitempty"`
	// Source is the .toml file (and key) that declared this
	// version. Single source; superseded by Sources when mise
	// resolves the version from multiple files.
	Source *miseSource `json:"source,omitempty"`
	// Sources is the list of .toml files that contributed to
	// resolving this version. Empty when no config files were
	// consulted (e.g., version installed only via `mise install`
	// with no config).
	Sources []miseSource `json:"sources,omitempty"`
	// SymlinkedTo is the shim path the version is symlinked
	// from. Only present when the version is symlinked into the
	// active shim dir.
	SymlinkedTo string `json:"symlinked_to,omitempty"`
	// Installed is true when the install artifacts are on disk.
	// We filter on this implicitly by passing `--installed`, but
	// we also defend against upstream bugs where the flag is
	// ignored.
	Installed bool `json:"installed"`
	// Active is true when this version is the active default.
	// We do not currently use this — but we capture it for
	// forward-compatibility with the SetDefault mutation.
	Active bool `json:"active"`
}

// miseSource identifies the config file + key that declared a
// version. We currently do nothing with this; modeled only so the
// JSON decoder doesn't choke on `source: {...}` / `sources: [...]`
// fields.
type miseSource struct {
	Path string `json:"path"`
}

// Detect returns true when mise appears to be installed — i.e., the
// `mise` binary is on PATH.
//
// We do NOT check $MISE_DATA_DIR (unlike asdf's $ASDF_DATA_DIR) for
// one reason: mise's CLI is the authoritative source of installed
// versions, so without the binary we can't query anything meaningful.
// The PATH lookup alone is sufficient signal.
//
// Per the Manager contract, Detect MUST be cheap — exec.LookPath is
// a single stat walk, well within the budget.
func (m *Mise) Detect() bool {
	return platform.LookupManagerBinary("mise") != ""
}

// Version returns mise's own version string.
//
// Real observed output (mise 2026.6.15):
//
//	2026.6.15 macos-arm64 (2026-06-26)
//
// Mise uses CalVer (YYYY.MM.PATCH). The first whitespace-separated
// token is the version; subsequent tokens are the target triple and
// the build date, which we discard. Some builds may prefix with
// "v" (e.g., "v2026.6.15"); we strip the optional "v" defensively.
//
// `semver.NewVersion` will reject CalVer strings (it expects
// "MAJOR.MINOR.PATCH" with non-zero-leading numeric segments).
// ParseMiseVersion returns the raw token and lets the caller decide
// whether to validate it — consistent with parseASDFVersion's
// policy of not enforcing semver on the manager-version string.
func (m *Mise) Version() (string, error) {
	res, err := runShell(context.Background(), "mise", "--version")
	if err != nil {
		return "", fmt.Errorf("mise --version: %w", err)
	}
	return parseMiseVersion(res.Stdout)
}

// parseMiseVersion extracts the version token from `mise --version`
// output. Exposed (lowercase) for direct unit testing.
//
// Real observed output:
//
//	2026.6.15 macos-arm64 (2026-06-26)
//	v2026.6.15 macos-arm64 (2026-06-26)
//
// We split on whitespace, take the first token, and strip an
// optional "v" prefix. We do NOT validate that the result is a
// semver or CalVer — the caller can decide whether to feed it into
// semver.NewVersion (which will reject CalVer).
func parseMiseVersion(stdout string) (string, error) {
	out := strings.TrimSpace(stdout)
	if out == "" {
		return "", errors.New("mise --version returned empty output")
	}
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return "", errors.New("mise --version returned no tokens")
	}
	// Take the first whitespace-separated token and strip an
	// optional "v" prefix. Defensive against "v2026.6.15" vs
	// "2026.6.15".
	return strings.TrimPrefix(fields[0], "v"), nil
}

// ListInstalled returns every Node.js version mise has installed,
// sorted ascending. Source: `mise ls --installed --json node`.
//
// We pass three flags:
//   - `--installed`  only emit entries whose install artifacts
//     are on disk (skip "requested but not installed" entries)
//   - `--json`       emit a top-level JSON array (the default
//     is a human-readable table that is hostile to parsing)
//   - `node`         the positional argument scoping the query
//     to the node tool — without this, mise prints ALL tools'
//     versions, which we cannot safely enumerate here
//
// Per upstream source (src/cli/ls.rs), with the `node` positional
// argument set, the JSON output is a top-level array (not an
// object keyed by tool name). Each element has at minimum a
// `version` string.
//
// Note: mise does not have an nvm-style "system" sentinel — we
// don't filter for one.
func (m *Mise) ListInstalled(ctx context.Context) ([]semver.Version, error) {
	res, err := runShell(ctx, "mise", "ls", "--installed", "--json", "node")
	if err != nil {
		return nil, fmt.Errorf("mise ls --installed --json node: %w", err)
	}
	return parseMiseInstalled(res.Stdout)
}

// parseMiseInstalled turns raw `mise ls --installed --json node`
// output into a sorted-ascending []semver.Version. Exposed
// (lowercase) for direct unit testing.
//
// Expected input is a JSON array of objects, e.g.:
//
//	[
//	  {"version": "20.11.1", "installed": true,  "active": false},
//	  {"version": "22.5.0",  "installed": true,  "active": true}
//	]
//
// An empty array `[]` is valid — mise installed but no node
// versions yet. We return a non-nil empty slice in that case so
// callers can rely on `len(result) == 0` rather than nil-checks.
//
// We only model the fields we need (version, installed). mise
// populates other fields (requested_version, source, etc.)
// opportunistically; we use `omitempty` to ignore them rather
// than rejecting the JSON.
//
// `installed` is checked as a defensive sanity check: we passed
// `--installed`, so every entry SHOULD have installed=true, but
// if upstream has a bug where the flag is ignored we filter here
// rather than returning ghost installs.
func parseMiseInstalled(stdout string) ([]semver.Version, error) {
	out := strings.TrimSpace(stdout)
	if out == "" {
		// Empty stdout = no versions installed. Treat as an
		// empty list rather than a JSON parse error.
		return make([]semver.Version, 0), nil
	}

	var entries []miseToolVersion
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		return nil, fmt.Errorf("mise ls JSON: %w", err)
	}

	versions := make([]semver.Version, 0, len(entries))
	for _, e := range entries {
		// Defensive: skip entries that --installed should have
		// filtered out. Also skip empty version strings (malformed
		// upstream).
		if !e.Installed || e.Version == "" {
			continue
		}
		v, err := semver.NewVersion(e.Version)
		if err != nil {
			// Skip unparseable versions rather than aborting the
			// whole list. Forward-compatibility for new metadata
			// formats mise might add.
			continue
		}
		versions = append(versions, *v)
	}

	// Sort ascending by semver. semver.Collection in v3 requires
	// []*Version, so we use sort.Slice with semver.Compare.
	sort.Slice(versions, func(i, j int) bool {
		return versions[i].Compare(&versions[j]) < 0
	})
	return versions, nil
}

// --- Mutation methods ----------------------------------------------------
//
// Install, Uninstall, Use, SetDefault, GlobalNpmPrefix, and Current
// for Mise all shell out to the `mise` binary through runShell.
//
// Important Mise specifics:
//   - Mise takes `node@<v>` rather than `<v>`: `mise install node@<v>`.
//   - SetDefault runs `mise use --global node@<v>` which writes the
//     pin to ~/.config/mise/config.toml. This is what `nodeup
//     upgrade` calls after Install.
//   - Use runs `mise use node@<v>` which writes the pin to the
//     local ./mise.toml (NOT what we want for a system-wide upgrade —
//     `nodeup upgrade` calls SetDefault instead, but the per-shell
//     Use is exposed for completeness).
//   - GlobalNpmPrefix points at $MISE_DATA_DIR/installs/node/<v>/lib/node_modules.
//   - Current runs `mise current node` which emits just `<v>`.

// Install runs `mise install node@<v>`. Mise takes the tool name as
// part of the spec (`node@<v>`), not as a separate positional arg.
func (m *Mise) Install(ver semver.Version) error {
	res, err := runShell(context.Background(), "mise", "install", "node@"+ver.String())
	if err != nil {
		return fmt.Errorf("mise install node@%s: %w", ver, err)
	}
	_ = res
	return nil
}

// Uninstall runs `mise uninstall node@<v>`. Mise silently allows
// uninstalling the active version — the next shell call resolves to
// the previous tool-versions pin or fails if there is none.
func (m *Mise) Uninstall(ver semver.Version) error {
	res, err := runShell(context.Background(), "mise", "uninstall", "node@"+ver.String())
	if err != nil {
		return fmt.Errorf("mise uninstall node@%s: %w", ver, err)
	}
	_ = res
	return nil
}

// Use runs `mise use node@<v>` for the current shell. Note this
// writes the pin to the LOCAL ./mise.toml (per-project), not the
// global config — `nodeup upgrade` calls SetDefault instead.
func (m *Mise) Use(ver semver.Version) error {
	res, err := runShell(context.Background(), "mise", "use", "node@"+ver.String())
	if err != nil {
		return fmt.Errorf("mise use node@%s: %w", ver, err)
	}
	_ = res
	return nil
}

// SetDefault runs `mise use --global node@<v>` which writes the
// `node = "<v>"` line to ~/.config/mise/config.toml. This is what
// `nodeup upgrade` calls after Install.
func (m *Mise) SetDefault(ver semver.Version) error {
	res, err := runShell(context.Background(), "mise", "use", "--global", "node@"+ver.String())
	if err != nil {
		return fmt.Errorf("mise use --global node@%s: %w", ver, err)
	}
	_ = res
	return nil
}

// GlobalNpmPrefix returns the per-version global npm directory for
// the given version. Mise's on-disk layout is:
//
//	$MISE_DATA_DIR/installs/node/<v>/lib/node_modules
//
// Default $MISE_DATA_DIR is ~/.local/share/mise.
func (m *Mise) GlobalNpmPrefix(ver semver.Version) (string, error) {
	dir := miseDataDir()
	if dir == "" {
		return "", errors.New("mise: cannot resolve MISE_DATA_DIR or ~/.local/share/mise")
	}
	prefix := filepath.Join(dir, "installs", "node", ver.String(), "lib", "node_modules")
	if _, err := os.Stat(prefix); err != nil {
		return "", fmt.Errorf("mise global npm prefix for %s (looked at %s): %w", ver, prefix, err)
	}
	return prefix, nil
}

// miseDataDir returns Mise's data root — where installs live.
// Resolution order:
//  1. $MISE_DATA_DIR (the official override)
//  2. ~/.local/share/mise (the documented default per mise install)
//
// Returns "" if neither resolves.
func miseDataDir() string {
	if d := strings.TrimSpace(os.Getenv("MISE_DATA_DIR")); d != "" {
		return d
	}
	home, err := homeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".local", "share", "mise")
}

// Current returns the version Mise currently has active for the
// user. Source: `mise current node`. The output is a bare semver
// like "22.11.0" (with optional "v" prefix on some builds).
func (m *Mise) Current(ctx context.Context) (semver.Version, error) {
	res, err := runShell(ctx, "mise", "current", "node")
	if err != nil {
		return semver.Version{}, fmt.Errorf("mise current node: %w", err)
	}
	return parseMiseCurrent(res.Stdout)
}

// parseMiseCurrent extracts the active version from `mise current
// node` output. Exposed (lowercase) for direct unit testing.
//
// Real observed output (mise 2026.6.15):
//
//	22.11.0
//
// or, when no version is active:
//
//	# some mise builds print nothing or "system"
//
// We take the first non-empty line, strip whitespace, strip an
// optional "v" prefix, and feed the remainder to semver.NewVersion.
// The literal "system" is not a managed version and we return an
// error so callers skip the active-version exclusion.
func parseMiseCurrent(stdout string) (semver.Version, error) {
	for _, raw := range strings.Split(stdout, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if line == "system" {
			return semver.Version{}, errors.New("mise current: 'system' is not a managed version")
		}
		v, err := semver.NewVersion(strings.TrimPrefix(line, "v"))
		if err != nil {
			return semver.Version{}, fmt.Errorf("mise current: parse %q: %w", line, err)
		}
		return *v, nil
	}
	return semver.Version{}, errors.New("mise current node returned empty output")
}
