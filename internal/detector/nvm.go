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

// NVM is the Node Version Manager implementation. nvm is unusual because
// it is a SHELL FUNCTION, not a binary — NVM is a shell function
// wrapper around the real `nvm` script, so we must source it before
// running any subcommand.
//
// Strategy C is used for reads (parse ~/.nvm/versions/node/* directly).
// For mutating operations (install, uninstall, use) we will fall back to
// Strategy A in a later phase: `bash -c "source ~/.nvm/nvm.sh && nvm <cmd>"`.
//
// Phase 1 implements the detection surface only:
//   - Detect       : $NVM_DIR env var OR ~/.nvm/nvm.sh existence
//   - Version      : source nvm.sh, then `nvm --version` (Strategy A,
//     invoked through platform.RunShellScript so we get the
//     right shell per OS)
//   - ListInstalled: read ~/.nvm/versions/node/* entries (Strategy C, no
//     subprocess — fastest, deterministic, easy to test)
//
// Mutation methods (Install, Uninstall, Use, SetDefault, GlobalNpmPrefix)
// return an explicit "not implemented" error so callers can detect them
// at runtime instead of getting a silent zero-value result.
type NVM struct{}

// NewNVM constructs a fresh nvm detector. Returned by pointer so it
// satisfies the Manager interface uniformly with the other detectors.
func NewNVM() *NVM { return &NVM{} }

func (n *NVM) Name() string { return "nvm" }

// listDir is the package-level seam used by NVM to enumerate
// ~/.nvm/versions/node. Tests overwrite it to return canned DirEntry
// slices without touching the real filesystem. Production code never
// reassigns it.
//
// Signature matches os.ReadDir so a direct assignment works.
var listDir = os.ReadDir

// runScript is the package-level seam used by NVM to invoke shell scripts
// (specifically: source ~/.nvm/nvm.sh && nvm --version). Tests overwrite
// it to capture the script and return canned output without spawning a
// subprocess. Production code never reassigns it.
//
// Signature matches platform.RunShellScript so a direct assignment works.
var runScript = platform.RunShellScript

// ErrNVMNotImplemented is returned by NVM mutation methods that have not
// yet been implemented in Phase 1 (Install, Uninstall, Use, SetDefault,
// GlobalNpmPrefix). Returning this error instead of a zero value lets
// callers distinguish "I haven't done it yet" from "user passed a bad
// version" via errors.Is.
var ErrNVMNotImplemented = errors.New("nvm mutation commands not yet implemented")

// nvmDir returns the nvm install root. Resolution order:
//  1. $NVM_DIR environment variable (the official override)
//  2. ~/.nvm (the documented default)
//
// Returns "" if neither can be resolved (e.g., HOME unset on a stripped-
// down CI runner). Callers must treat "" as "nvm not installed".
func nvmDir() string {
	if d := strings.TrimSpace(os.Getenv("NVM_DIR")); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".nvm")
}

// nvmScriptPath returns the absolute path to nvm.sh inside the install
// root. Returns "" if the install root can't be resolved.
func nvmScriptPath() string {
	dir := nvmDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "nvm.sh")
}

// Detect returns true when nvm appears to be installed. nvm is a shell
// function, so we check for the script it gets sourced from rather than
// for an executable on PATH. We accept either $NVM_DIR pointing at a
// directory (faster — stat only) or the conventional ~/.nvm/nvm.sh
// location.
//
// Per the Manager contract, Detect MUST be cheap — this implementation
// never spawns a subprocess.
func (n *NVM) Detect() bool {
	script := nvmScriptPath()
	if script == "" {
		return false
	}
	// os.Stat returns an error for missing files; we collapse both "not
	// found" and "permission denied" into a false result so that an
	// unreadable nvm install is treated as "not present" rather than a
	// hard error from Detect. RunShellScript will surface the real
	// reason when the user actually tries to invoke nvm.
	_, err := os.Stat(script)
	return err == nil
}

// Version returns nvm's own version string, e.g. "0.40.5". nvm only
// exists as a shell function, so we have to source nvm.sh before calling
// it. We delegate the shell choice to platform.RunShellScript — on unix
// it prefers bash (nvm.sh is bash-only), on Windows it uses cmd.exe
// (where nvm is uncommon; this still gives a sensible error rather than
// a hang).
//
// We quote the script path via platform.QuotePath so Windows profiles
// like "C:\Program Files\..." don't get word-split.
func (n *NVM) Version() (string, error) {
	script := nvmScriptPath()
	if script == "" {
		return "", errors.New("nvm not detected: cannot resolve nvm.sh path")
	}
	// `source` is bash syntax. RunShellScript on unix selects bash
	// first, so this is portable on macOS / Linux. On Windows the
	// cmd.exe branch will fail with a syntax error, which is the
	// expected outcome — nvm-windows is a separate manager.
	cmd := fmt.Sprintf("source %s && nvm --version", platform.QuotePath(script))
	res, err := runScript(context.Background(), cmd)
	if err != nil {
		return "", fmt.Errorf("nvm --version: %w", err)
	}
	return parseNVMVersion(res.Stdout)
}

// parseNVMVersion extracts the version token from nvm's --version output.
//
// Observed real output (nvm 0.40.5):
//
//	0.40.5
//
// Older nvm versions printed `nvm 0.x.y` — we accept both. Leading
// whitespace and a trailing newline are trimmed.
func parseNVMVersion(stdout string) (string, error) {
	out := strings.TrimSpace(stdout)
	if out == "" {
		return "", errors.New("nvm --version returned empty output")
	}
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return "", errors.New("nvm --version returned no tokens")
	}
	// If the first token is literally "nvm" take the next one.
	if fields[0] == "nvm" && len(fields) >= 2 {
		return strings.TrimSpace(fields[1]), nil
	}
	// Otherwise assume the whole first token is the version.
	return strings.TrimSpace(fields[0]), nil
}

// ListInstalled returns every Node.js version nvm has installed, sorted
// ascending. Source: directory entries under <nvmDir>/versions/node/.
//
// Each subdirectory of that directory is a full Node install. nvm names
// them like "v18.14.0" (with v prefix), but we accept both with and
// without — semver.NewVersion normalizes.
//
// Non-directory entries (e.g. the "lts" symlink that some nvm versions
// drop here) are skipped. The literal name "system" is a sentinel for
// the system Node and not a managed install, so we skip it too.
func (n *NVM) ListInstalled() ([]semver.Version, error) {
	dir := nvmDir()
	if dir == "" {
		return nil, errors.New("nvm not detected: cannot resolve NVM_DIR or ~/.nvm")
	}
	versionsDir := filepath.Join(dir, "versions", "node")
	entries, err := listDir(versionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			// nvm is installed but has never installed a Node version —
			// return an empty (non-nil) slice, not an error. Callers
			// distinguish this from "nvm not installed" via Detect().
			return []semver.Version{}, nil
		}
		return nil, fmt.Errorf("read %s: %w", versionsDir, err)
	}
	return parseNVMInstalledEntries(entries)
}

// parseNVMInstalledEntries turns a list of directory entries under
// ~/.nvm/versions/node into a sorted-ascending []semver.Version.
// Exposed (lowercase) for direct unit testing.
//
// Returns a non-nil empty slice when no parseable versions are present
// — callers rely on this for "nvm installed, nothing managed yet".
func parseNVMInstalledEntries(entries []os.DirEntry) ([]semver.Version, error) {
	versions := make([]semver.Version, 0)
	for _, e := range entries {
		name := e.Name()
		// Skip "system" sentinel and anything that doesn't look like a
		// versioned directory (e.g. nvm's "lts" alias symlink).
		if name == "system" {
			continue
		}
		// Must be a directory — nvm stores installs as real dirs.
		if !e.IsDir() {
			continue
		}
		// Strip a leading "v" if present so semver.NewVersion accepts it.
		v, err := semver.NewVersion(strings.TrimPrefix(name, "v"))
		if err != nil {
			// Skip unparseable names rather than aborting the whole list.
			// Future nvm versions could add new metadata dirs we don't
			// recognize; we want to be forward-compatible.
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
// ErrNVMNotImplemented. They will be filled in when the upgrade command
// (Phase 3) needs to mutate state. Returning an explicit sentinel error
// (rather than nil) makes "not implemented" provably distinguishable
// from "succeeded".

func (n *NVM) Install(v semver.Version) error    { return ErrNVMNotImplemented }
func (n *NVM) Uninstall(v semver.Version) error  { return ErrNVMNotImplemented }
func (n *NVM) Use(v semver.Version) error        { return ErrNVMNotImplemented }
func (n *NVM) SetDefault(v semver.Version) error { return ErrNVMNotImplemented }
func (n *NVM) GlobalNpmPrefix(v semver.Version) (string, error) {
	return "", ErrNVMNotImplemented
}
