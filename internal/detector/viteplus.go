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

// VitePlus is the Vite+ implementation (https://viteplus.dev). Vite+ is
// the unified toolchain from VoidZero: a single `vp` binary that manages
// the Node.js runtime, the package manager, and the frontend toolchain.
//
// Vite+ manages Node runtimes through its `vp env` subcommand, which
// exposes the same operations nodeup needs from a version manager:
//
//   - Detect        : `vp` on PATH (or <data>/bin/vp on disk)
//   - Version       : `vp --version`, parsed to drop the leading "vp "
//   - ListInstalled : `vp env list node --json`
//   - Current       : `vp env current node --json`
//   - Install       : `vp env install node@<v>`
//   - Uninstall     : `vp env uninstall node@<v>`
//   - Use           : `vp env use node@<v>`
//   - SetDefault    : `vp env default node@<v>`
//   - GlobalNpmPrefix : <data>/js_runtime/node/<v>/lib/node_modules
//
// Vite+ has two on-disk layouts.
//
// The split layout is the default for fresh installs. The data
// directory holds the Node runtimes, and the bin directory holds the
// `vp` binary and the shims:
//
//	<data>/
//	├── bin/                   # vp binary + node/npm/pnpm shims
//	├── current -> <version>/  # the active vp CLI release
//	└── js_runtime/
//	    └── node/
//	        └── 24.22.1/      # one directory per Node runtime
//	            ├── bin/node
//	            └── lib/node_modules
//
// The single-root layout is the legacy one, still produced by VP_HOME
// and by pre-0.2.x installs. Everything lives under one root, with the
// data directory mapped to the root itself:
//
//	<root>/
//	├── bin/vp
//	└── js_runtime/node/<v>/...
//
// The data directory resolution mirrors upstream's chain (see
// crates/vp_shared/src/dirs/resolution.rs): VP_HOME → <home>/.vite-plus
// → VP_DATA_DIR → XDG / platform default. We resolve all candidates
// rather than just the first hit because nodeup has to find Node
// installs without shelling out to `vp` — see vitePlusDataDirs.
//
// Vite+ is Node-version-manager-shaped but with one important twist: it
// can run in "system-first" mode, where the shims prefer the system Node
// over a managed runtime. In that mode the active Node is NOT a
// Vite+-managed runtime, so Current() reports an error rather than a
// version. nodeup treats a failed Current() as "active version unknown"
// and fails closed on cleanup (see issue #58).
type VitePlus struct{}

// NewVitePlus constructs a fresh Vite+ detector.
func NewVitePlus() *VitePlus { return &VitePlus{} }

func (v *VitePlus) Name() string { return "vite-plus" }

// runShell (declared in fnm.go) is the package-level seam used by
// VitePlus to invoke the `vp` binary. Tests overwrite it to capture
// arguments and return canned output without spawning a subprocess.
// Production code never reassigns it.

// vitePlusDirName is Vite+'s directory name under the XDG data home
// and the shared name for its bin/cache/config/state directories.
const vitePlusDirName = "vite-plus"

// vitePlusLegacyDirName is the single-root directory name Vite+ used
// before the split layout. It only counts as an install root when it
// contains a "current" link — a stray directory must not flip the
// layout, or a later `vp upgrade` could move the roots out from under
// us (upstream makes the same check).
const vitePlusLegacyDirName = ".vite-plus"

// vitePlusInstalledVersion mirrors one entry in the `node` array of
// `vp env list node --json`.
//
//	{"version": "26.13.0", "current": true, "default": false}
//
// We only model the fields we consume. Unknown fields are ignored by
// encoding/json, so a newer Vite+ that adds metadata doesn't break
// parsing.
type vitePlusInstalledVersion struct {
	// Version is the installed runtime version (e.g., "26.13.0").
	Version string `json:"version"`
	// Current is true when this version is the one the shims resolve
	// to right now. It can be false for every entry when the active
	// Node is a system install (system-first mode).
	Current bool `json:"current"`
	// Default is true when this version is the machine-wide default
	// set via `vp env default`.
	Default bool `json:"default"`
}

// vitePlusEnvList is the top-level JSON shape of
// `vp env list node --json`.
type vitePlusEnvList struct {
	// Node holds the installed Node runtimes. The field is omitted
	// entirely when Vite+ has no node runtimes installed.
	Node []vitePlusInstalledVersion `json:"node"`
}

// vitePlusCurrentNode mirrors the `node` object of
// `vp env current node --json`.
//
//	{
//	  "version": "26.13.0",
//	  "source": "lts",
//	  "source_path": "/home/me/.local/share/vite-plus/js_runtime/node/26.13.0/bin/node",
//	  "project_root": "/home/me/project",
//	  "bin_path": "/home/me/.local/share/vite-plus/js_runtime/node/26.13.0/bin/node",
//	  "installed": true,
//	  "mode": "managed"
//	}
type vitePlusCurrentNode struct {
	// Version is the resolved Node version.
	Version string `json:"version"`
	// Source explains where the version came from: "lts",
	// "default", ".node-version", ".nvmrc", "devEngines.runtime",
	// "engines.node", or "system PATH".
	Source string `json:"source"`
	// Installed is true when the runtime is present on disk.
	Installed bool `json:"installed"`
	// Mode is "managed" when the shims resolve through Vite+, or
	// "system_first" when they prefer the system Node.
	Mode string `json:"mode"`
}

// vitePlusEnvCurrent is the top-level JSON shape of
// `vp env current node --json`.
type vitePlusEnvCurrent struct {
	// Node is null when Vite+ cannot resolve a Node version at all.
	Node *vitePlusCurrentNode `json:"node"`
}

// vitePlusDataDirs returns the candidate Vite+ data directories, in
// upstream's own resolution order. The first entry that exists is the
// one Vite+ itself would use; the rest are fallbacks we keep so that
// nodeup can still find Node installs when the environment differs
// from the one `vp` would resolve (e.g. a stale shell without the
// Vite+ env vars exported).
//
// Resolution order (mirrors crates/vp_shared/src/dirs/resolution.rs):
//  1. $VP_HOME — pins the single-root layout; data maps to the root.
//  2. <home>/.vite-plus — the legacy single-root install, but only
//     when it contains a "current" link.
//  3. $VP_DATA_DIR — the split-layout data directory.
//  4. The XDG / platform default: $XDG_DATA_HOME/vite-plus or
//     ~/.local/share/vite-plus on unix, %LOCALAPPDATA%\vite-plus\data
//     on Windows.
//
// Upstream requires VP_BIN_DIR and VP_CACHE_DIR alongside VP_DATA_DIR
// for the override group to take effect. We don't enforce that here:
// we only ever read the data directory, and a user who pinned
// VP_DATA_DIR wants us to look there.
func vitePlusDataDirs() []string {
	var dirs []string

	if d := strings.TrimSpace(getenv("VP_HOME")); d != "" {
		// Single-root layout: data maps to the root itself.
		dirs = append(dirs, d)
	}

	if h, err := userHomeDir(); err == nil && h != "" {
		legacy := filepath.Join(h, vitePlusLegacyDirName)
		// A "current" link identifies a real install. A bare
		// directory could be a leftover from a pre-split local CLI
		// and must not be treated as an install root.
		if _, err := os.Stat(filepath.Join(legacy, "current")); err == nil {
			dirs = append(dirs, legacy)
		}
	}

	if d := strings.TrimSpace(getenv("VP_DATA_DIR")); d != "" {
		dirs = append(dirs, d)
	}

	if platform.IsWindows() {
		if d := strings.TrimSpace(getenv("LOCALAPPDATA")); d != "" {
			dirs = append(dirs, filepath.Join(d, vitePlusDirName, "data"))
		} else if h, err := userHomeDir(); err == nil && h != "" {
			dirs = append(dirs, filepath.Join(h, "AppData", "Local", vitePlusDirName, "data"))
		}
	} else {
		if xdg := strings.TrimSpace(getenv("XDG_DATA_HOME")); xdg != "" {
			dirs = append(dirs, filepath.Join(xdg, vitePlusDirName))
		} else if h, err := userHomeDir(); err == nil && h != "" {
			dirs = append(dirs, filepath.Join(h, ".local", "share", vitePlusDirName))
		}
	}

	return dirs
}

// Detect returns true when Vite+ appears to be installed. Vite+ is a
// real binary, so we accept either:
//  1. the `vp` binary is on PATH (via platform.LookupManagerBinary), OR
//  2. a <data>/bin/vp exists on disk
//
// Per the Manager contract, Detect MUST be cheap — neither branch
// spawns a subprocess.
func (v *VitePlus) Detect() bool {
	if platform.LookupManagerBinary("vp") != "" {
		return true
	}
	for _, dir := range vitePlusDataDirs() {
		if dir == "" {
			continue
		}
		bin := filepath.Join(dir, "bin", "vp")
		if _, err := os.Stat(bin); err == nil {
			return true
		}
	}
	return false
}

// Version returns Vite+'s own version string.
//
// Real observed output (vp 1.4.0):
//
//	vp v1.4.0
//
//	Local vite-plus:
//	  vite-plus  Not found
//	...
//
// The version is on the first line; the rest is a toolchain report we
// don't need. We parse the first line and accept both the "vp <v>"
// form and a bare version.
func (v *VitePlus) Version() (string, error) {
	res, err := runShell(context.Background(), "vp", "--version")
	if err != nil {
		return "", fmt.Errorf("vp --version: %w", err)
	}
	return parseVitePlusVersion(res.Stdout)
}

// parseVitePlusVersion extracts the version token from `vp --version`
// output. Exposed (lowercase) for direct unit testing.
//
// Real observed output:
//
//	vp v1.4.0
//
//	Local vite-plus:
//	  vite-plus  Not found
//
// We scan the output line by line and take the first line that looks
// like a version: either a "vp <version>" header or a bare version
// token. We deliberately do not require the "vp " prefix — some builds
// and wrappers print the version on its own.
func parseVitePlusVersion(stdout string) (string, error) {
	for _, raw := range strings.Split(stdout, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		// "vp v1.4.0" — the header line.
		if strings.EqualFold(fields[0], "vp") {
			if len(fields) < 2 {
				continue
			}
			return strings.TrimPrefix(fields[1], "v"), nil
		}
		// A bare version token on its own line.
		if isSemverToken(fields[0]) {
			return strings.TrimPrefix(fields[0], "v"), nil
		}
	}
	return "", errors.New("vp --version returned no version token")
}

// isSemverToken reports whether s looks like a semantic version. We
// don't use semver.NewVersion here because the callers want the raw
// token (with any "v" prefix stripped), not a normalized struct.
func isSemverToken(s string) bool {
	s = strings.TrimPrefix(s, "v")
	parts := strings.Split(s, ".")
	if len(parts) < 2 {
		return false
	}
	for _, p := range parts {
		if p == "" {
			return false
		}
		for _, r := range p {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return true
}

// ListInstalled returns every Node.js version Vite+ has installed,
// sorted ascending. Source: `vp env list node --json`.
//
// We scope the query to `node` so Vite+ doesn't also enumerate npm,
// pnpm, Yarn, and Bun — we only care about runtimes. The JSON output
// (rather than the human-readable table) is what we parse; the table
// is hostile to scripting.
//
// Vite+ has no nvm-style "system" sentinel: an uninstalled system
// Node simply doesn't appear in the list.
func (v *VitePlus) ListInstalled(ctx context.Context) ([]semver.Version, error) {
	res, err := runShell(ctx, "vp", "env", "list", "node", "--json")
	if err != nil {
		return nil, fmt.Errorf("vp env list node --json: %w", err)
	}
	return parseVitePlusInstalled(res.Stdout)
}

// parseVitePlusInstalled turns raw `vp env list node --json` output
// into a sorted-ascending []semver.Version. Exposed (lowercase) for
// direct unit testing.
//
// Expected input:
//
//	{
//	  "node": [
//	    {"version": "24.22.1", "current": false, "default": false},
//	    {"version": "26.13.0", "current": true,  "default": true}
//	  ]
//	}
//
// An empty array — or a missing "node" key — is valid: Vite+ is
// installed but has no managed runtimes yet. We return a non-nil empty
// slice in that case so callers can rely on len(result) == 0 rather
// than nil-checks.
//
// Unparseable versions are skipped rather than aborting the whole
// list, so a future Vite+ metadata format can't break `nodeup list`.
func parseVitePlusInstalled(stdout string) ([]semver.Version, error) {
	out := strings.TrimSpace(stdout)
	if out == "" {
		// Empty stdout = nothing installed. Treat as an empty list
		// rather than a JSON parse error.
		return make([]semver.Version, 0), nil
	}

	var list vitePlusEnvList
	if err := json.Unmarshal([]byte(out), &list); err != nil {
		return nil, fmt.Errorf("vp env list JSON: %w", err)
	}

	versions := make([]semver.Version, 0, len(list.Node))
	for _, e := range list.Node {
		if e.Version == "" {
			continue
		}
		v, err := semver.NewVersion(strings.TrimPrefix(e.Version, "v"))
		if err != nil {
			// Skip unparseable versions rather than aborting the whole
			// list. Forward-compatibility for new metadata formats.
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
// for Vite+ all shell out to the `vp` binary through runShell.
//
// Important Vite+ specifics:
//   - Vite+ takes `node@<v>` rather than a bare `<v>`: `vp env install
//     node@<v>`. The bare form also works upstream, but the qualified
//     form is unambiguous — a bare `22` could be read as a
//     package-manager selector.
//   - SetDefault runs `vp env default node@<v>`, which persists the
//     machine-wide default in Vite+'s config.json. This is what
//     `nodeup upgrade` calls after Install.
//   - Use runs `vp env use node@<v>`, which writes a per-shell session
//     file (e.g. .session-node-version). It does NOT reconfigure the
//     user's already-running shell — Vite+'s shell wrapper does that
//     by eval'ing the command's output, and nodeup can't. The session
//     file does affect later shim calls made from the same job.
//   - GlobalNpmPrefix points at <data>/js_runtime/node/<v>/lib/node_modules.
//   - Current runs `vp env current node --json` which emits a JSON
//     object with the resolved version and the shim mode.

// Install runs `vp env install node@<v>`. Vite+ downloads the runtime
// into <data>/js_runtime/node/<v> and verifies the release checksum.
func (v *VitePlus) Install(ver semver.Version) error {
	res, err := runShell(context.Background(), "vp", "env", "install", "node@"+ver.String())
	if err != nil {
		return fmt.Errorf("vp env install node@%s: %w", ver, err)
	}
	_ = res
	return nil
}

// Uninstall runs `vp env uninstall node@<v>`. Vite+ refuses to remove
// a version that isn't installed ("Node.js v<v> is not installed") and
// exits non-zero, so the error surfaces to the caller rather than
// leaving a silent no-op behind.
func (v *VitePlus) Uninstall(ver semver.Version) error {
	res, err := runShell(context.Background(), "vp", "env", "uninstall", "node@"+ver.String())
	if err != nil {
		return fmt.Errorf("vp env uninstall node@%s: %w", ver, err)
	}
	_ = res
	return nil
}

// Use runs `vp env use node@<v>` for the current shell session.
//
// Note this writes a session file rather than reconfiguring the user's
// live shell — Vite+'s shell wrapper (installed by `vp env setup`) is
// what evals the output and exports VP_NODE_VERSION. nodeup shells out
// to the binary directly, so the effect is limited to shims invoked
// later from the same job.
func (v *VitePlus) Use(ver semver.Version) error {
	res, err := runShell(context.Background(), "vp", "env", "use", "node@"+ver.String())
	if err != nil {
		return fmt.Errorf("vp env use node@%s: %w", ver, err)
	}
	_ = res
	return nil
}

// SetDefault runs `vp env default node@<v>`, which persists the
// machine-wide default Node version in Vite+'s config.json. This is
// what `nodeup upgrade` calls after Install.
//
// Note that upstream stores dist-tags ("lts", "latest") unresolved so
// they follow the tag. We always pass an exact version, so the stored
// value is pinned.
func (v *VitePlus) SetDefault(ver semver.Version) error {
	res, err := runShell(context.Background(), "vp", "env", "default", "node@"+ver.String())
	if err != nil {
		return fmt.Errorf("vp env default node@%s: %w", ver, err)
	}
	_ = res
	return nil
}

// GlobalNpmPrefix returns the per-version global npm directory for
// the given version. Vite+'s on-disk layout is:
//
//	<data>/js_runtime/node/<v>/lib/node_modules
//
// The data directory is resolved through vitePlusDataDirs() and we
// return the first candidate whose prefix actually exists.
func (v *VitePlus) GlobalNpmPrefix(ver semver.Version) (string, error) {
	dirs := vitePlusDataDirs()
	if len(dirs) == 0 {
		return "", errors.New("vite-plus: cannot resolve the Vite+ data directory")
	}
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		prefix := filepath.Join(dir, "js_runtime", "node", ver.String(), "lib", "node_modules")
		if _, err := os.Stat(prefix); err == nil {
			return prefix, nil
		}
	}
	return "", fmt.Errorf("vite-plus global npm prefix for %s not found under %s", ver, dirs[0])
}

// Current returns the version Vite+ currently has active for the user.
// Source: `vp env current node --json`.
//
// Real observed output:
//
//	{
//	  "node": {
//	    "version": "26.13.0",
//	    "source": "lts",
//	    "bin_path": "/home/me/.local/share/vite-plus/js_runtime/node/26.13.0/bin/node",
//	    "installed": true,
//	    "mode": "managed"
//	  }
//	}
//
// When the shims are in system-first mode, Vite+ reports
// `"mode": "system_first"` and the version it reports is the *system*
// Node's version — not a Vite+-managed runtime. We return an error in
// that case: the active Node isn't one Vite+ manages, so it must not
// be excluded from (or attributed to) the managed set. nodeup treats a
// failed Current() as "active version unknown" and fails closed on
// cleanup (issue #58).
func (v *VitePlus) Current(ctx context.Context) (semver.Version, error) {
	res, err := runShell(ctx, "vp", "env", "current", "node", "--json")
	if err != nil {
		return semver.Version{}, fmt.Errorf("vp env current node --json: %w", err)
	}
	return parseVitePlusCurrent(res.Stdout)
}

// parseVitePlusCurrent extracts the active version from `vp env
// current node --json` output. Exposed (lowercase) for direct unit
// testing.
//
// Real observed output:
//
//	{
//	  "node": {
//	    "version": "26.13.0",
//	    "source": "lts",
//	    "installed": true,
//	    "mode": "managed"
//	  }
//	}
//
// We return an error when:
//   - the output is empty or not valid JSON,
//   - the "node" object is missing or null (Vite+ can't resolve one),
//   - the mode is "system_first" (the active Node is the system one),
//   - the version is empty or isn't a semver.
func parseVitePlusCurrent(stdout string) (semver.Version, error) {
	out := strings.TrimSpace(stdout)
	if out == "" {
		return semver.Version{}, errors.New("vp env current node returned empty output")
	}

	var cur vitePlusEnvCurrent
	if err := json.Unmarshal([]byte(out), &cur); err != nil {
		return semver.Version{}, fmt.Errorf("vp env current JSON: %w", err)
	}
	if cur.Node == nil {
		return semver.Version{}, errors.New("vp env current did not report a node version")
	}
	if cur.Node.Mode == "system_first" {
		return semver.Version{}, errors.New("vp env current: 'system_first' mode — the active Node is the system install, not a Vite+-managed runtime")
	}
	if cur.Node.Version == "" {
		return semver.Version{}, errors.New("vp env current reported an empty node version")
	}
	v, err := semver.NewVersion(strings.TrimPrefix(cur.Node.Version, "v"))
	if err != nil {
		return semver.Version{}, fmt.Errorf("vp env current: parse %q: %w", cur.Node.Version, err)
	}
	return *v, nil
}
