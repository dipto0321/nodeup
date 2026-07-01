package detector

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// SystemNodeKind classifies how a `node` binary is installed. The zero
// value is SystemNodeUnknown.
type SystemNodeKind int

const (
	// SystemNodeUnknown is the zero value. Returned when the binary
	// exists on PATH but doesn't match any known layout, or when no
	// `node` binary is on PATH at all (ResolveSystemNode returns an
	// error in that case; the zero value is reserved for "not yet
	// classified").
	SystemNodeUnknown SystemNodeKind = iota

	// SystemNodeOSPackage: Node is installed by the OS package
	// manager (apt, dnf, pacman, scoop, MSI). Examples: /usr/bin/node
	// (Debian/Ubuntu/RHEL), C:\Program Files\nodejs\node.exe.
	SystemNodeOSPackage

	// SystemNodeSnap: Node installed via snap (Linux). The binary is
	// /snap/bin/node with the real files under /snap/node/<rev>/.
	SystemNodeSnap

	// SystemNodeFlatpak: Node installed via flatpak. Rare but
	// exists on Flathub — nodeup cannot reach into a flatpak
	// runtime to swap versions.
	SystemNodeFlatpak

	// SystemNodeHomebrewCore: Node is a homebrew-core formula.
	// The binary lives at /usr/local/bin/node (Intel Homebrew) or
	// /opt/homebrew/bin/node (Apple Silicon Homebrew), with the
	// real files under /usr/local/Cellar/node/<v> or
	// /opt/homebrew/Cellar/node/<v>. Distinct from a Homebrew
	// tap formula like `node@22`, which would surface under a
	// manager-specific path instead.
	SystemNodeHomebrewCore

	// SystemNodeManaged: The binary lives inside a version manager's
	// data directory. nodeup can upgrade it.
	SystemNodeManaged
)

// String returns a human-readable label for the kind, suitable for
// the `nodeup upgrade` and `nodeup check` output. SystemNodeUnknown
// stringifies to "unknown"; callers wanting a non-string comparison
// should switch on the enum value directly.
func (k SystemNodeKind) String() string {
	switch k {
	case SystemNodeOSPackage:
		return "os-package"
	case SystemNodeSnap:
		return "snap"
	case SystemNodeFlatpak:
		return "flatpak"
	case SystemNodeHomebrewCore:
		return "homebrew-core"
	case SystemNodeManaged:
		return "manager"
	case SystemNodeUnknown:
		return "unknown"
	default:
		return fmt.Sprintf("kind(%d)", int(k))
	}
}

// SystemNodeInfo describes the `node` binary nodeup found on the
// user's PATH. Path is the absolute on-disk location of the resolved
// binary. Kind is the classification (see SystemNodeKind). Manager is
// the name of the manager we believe owns the installation when Kind
// is SystemNodeManaged — empty otherwise.
//
// The zero value is meaningful: a SystemNodeInfo{} with an empty Path
// means ResolveSystemNode didn't find a `node` on PATH.
type SystemNodeInfo struct {
	Path    string
	Kind    SystemNodeKind
	Manager string
}

// ErrNoNodeOnPATH is returned by ResolveSystemNode when no `node`
// executable can be located. Callers can use errors.Is to detect this
// specifically and decide whether it's worth warning about.
var ErrNoNodeOnPATH = errors.New("`node` not found on PATH")

// whichNode is the package-level seam used by ResolveSystemNode to
// locate the `node` binary. Tests overwrite it to return canned paths
// without touching the real filesystem. Production code never
// reassigns it.
//
// Signature: returns the absolute path of `node` on PATH, or empty
// string if not found. Returning a path and an error is fine — the
// caller prefers the path when non-empty, the error when path is
// empty. (This matches the LookupManagerBinary convention.)
var whichNode = func(ctx context.Context) (string, error) {
	// exec.LookPath walks PATH itself, so we don't depend on the
	// `which`/`where` shell-outs being present. On minimal images
	// (e.g., distroless containers, alpine installs) those commands
	// may be absent even though `node` is on PATH — using LookPath
	// avoids that failure mode. It also returns an absolute path
	// directly, with no first-line parsing.
	if err := ctx.Err(); err != nil {
		return "", err
	}
	p, err := exec.LookPath("node")
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrNoNodeOnPATH, err)
	}
	if p == "" {
		return "", ErrNoNodeOnPATH
	}
	return p, nil
}

// ResolveSystemNode locates `node` on PATH, classifies it by where it
// lives on disk, and returns the result. Returns ErrNoNodeOnPATH when
// no `node` binary exists.
//
// On macOS, Linux: invokes `which node` and reads the first line.
// On Windows: invokes `where node` and reads the first line.
//
// The classification is path-based and intentional: we never
// try to "use the manager's API" to identify it, because by
// definition a system node is one the manager does NOT see.
//
// If a manager is non-nil and its data directory contains the resolved
// path, Kind becomes SystemNodeManaged and Manager is set to m.Name().
// Otherwise the path-driven classifier decides.
func ResolveSystemNode(ctx context.Context, m Manager) (SystemNodeInfo, error) {
	p, err := whichNode(ctx)
	if err != nil {
		// Some shells return code 1 for "not found" with empty
		// output; collapse to our sentinel so callers can use
		// errors.Is cleanly.
		if p == "" {
			return SystemNodeInfo{}, fmt.Errorf("%w: %v", ErrNoNodeOnPATH, err)
		}
	}
	if p == "" {
		return SystemNodeInfo{}, ErrNoNodeOnPATH
	}

	// If the binary is inside the manager's data dir, it's clearly
	// managed. We try this first because the path-based classifier
	// can false-positive on a manager that happens to live under
	// /usr/local (rare but possible with self-hosted fnm).
	if m != nil {
		if managedRoots, ok := managerManagedRoots(m); ok {
			for _, root := range managedRoots {
				if isInside(p, root) {
					return SystemNodeInfo{
						Path:    p,
						Kind:    SystemNodeManaged,
						Manager: m.Name(),
					}, nil
				}
			}
		}
	}

	return SystemNodeInfo{
		Path: p,
		Kind: classifySystemNodePath(p),
	}, nil
}

// classifySystemNodePath classifies a path by structural cues.
// Pure function: same input → same output, no I/O. Tests pass
// absolute paths directly.
//
// Order matters: more-specific prefixes win. We check `/snap/bin/`
// before any generic `/usr/local` check, and the Homebrew Cellar
// before `/usr/local/bin/`.
func classifySystemNodePath(p string) SystemNodeKind {
	if p == "" {
		return SystemNodeUnknown
	}
	// Normalize for case-insensitive filesystems (Windows, default
	// macOS) without losing the original casing, since some Unix
	// systems genuinely have differently-cased directories (rare,
	// but mount points via casefold can introduce it).
	clean := filepath.Clean(p)

	// Use forward slashes for layout checks regardless of OS;
	// filepath.ToSlash handles the conversion. This lets us write one
	// table for both Windows and unix shapes.
	s := filepath.ToSlash(clean)

	switch {
	case strings.HasPrefix(s, "/snap/bin/"), isUnder(s, "/snap/node/"), isUnder(s, "/var/lib/snapd/snap/node/"):
		return SystemNodeSnap
	case isUnder(s, "/var/lib/flatpak/runtime/node"):
		// Flatpak runtimes live under /var/lib/flatpak/runtime/<id>/
		// <arch>/<branch>/active/files/...
		return SystemNodeFlatpak
	case strings.HasPrefix(s, "/usr/libexec/flatpak"), isUnder(s, "/usr/lib/flatpak/"):
		return SystemNodeFlatpak
	case isUnder(s, "/usr/local/Cellar/node/"), isUnder(s, "/opt/homebrew/Cellar/node/"):
		return SystemNodeHomebrewCore
	case strings.HasPrefix(s, "/usr/local/bin/") && runtime.GOOS == "darwin" && looksLikeHomebrewCoreLayout(s):
		// `/usr/local/bin/node` is the Homebrew wrapper symlink that
		// ultimately points at /usr/local/Cellar/node/<v>/bin/node.
		// We restrict this branch to macOS because on Linux,
		// `/usr/local/bin/node` is overwhelmingly a manual compile
		// (`make install`) — Homebrew on Linux lives at
		// /home/linuxbrew/.linuxbrew/bin/node (handled below), not
		// /usr/local/bin/. Misclassifying the manual install as
		// homebrew-core would tell the user to run `brew upgrade`,
		// which doesn't exist on their system.
		return SystemNodeHomebrewCore
	case strings.HasPrefix(s, "/opt/homebrew/bin/"), strings.HasPrefix(s, "/opt/homebrew/opt/node/"), strings.HasPrefix(s, "/home/linuxbrew/.linuxbrew/bin/"):
		// Apple Silicon Homebrew and Linuxbrew both land here.
		return SystemNodeHomebrewCore
	case strings.HasPrefix(s, "/usr/bin/"), strings.HasPrefix(s, "/bin/"):
		// Debian/Ubuntu/RHEL/Fedora/Arch all install Node here via
		// their package manager.
		return SystemNodeOSPackage
	case strings.HasPrefix(s, "/usr/sbin/"), strings.HasPrefix(s, "/sbin/"):
		// Some SUSE / Solaris layouts ship /usr/sbin/node. Same
		// caveat: don't manage it.
		return SystemNodeOSPackage
	case strings.HasPrefix(s, "/opt/node/"):
		// Some custom vendor packages drop Node into /opt/node/.
		return SystemNodeOSPackage
	case strings.HasPrefix(s, "/usr/local/bin/") && !looksLikeNVMInstall(clean):
		// /usr/local/bin/node on systems without Homebrew is usually
		// a manual compile (`make install` from source) — also not
		// safe for nodeup to touch. The nvm install path bypass
		// handles the rare NVM_DIR=/usr/local/nvm case.
		return SystemNodeOSPackage
	case strings.HasPrefix(s, "/opt/local/bin/"):
		// MacPorts default prefix. Same story as OS-package: don't
		// touch.
		return SystemNodeOSPackage
	}

	// Windows layouts. Checked last because Unix paths are far more
	// common in CI matrices; the cost of an extra strings.HasPrefix
	// on each call is irrelevant.
	if strings.HasPrefix(s, "C:/Program Files/nodejs/") || strings.HasPrefix(s, "C:/Program Files (x86)/nodejs/") {
		// The official Windows MSI drops node.exe here. Updates
		// come from the same MSI (or winget/scoop/choco).
		return SystemNodeOSPackage
	}
	if strings.Contains(s, "/scoop/apps/nodejs/") {
		// Scoop-installed node lands under ~/scoop/apps/nodejs/<ver>
		// (or any user's ~/scoop/...); Scoop upgrades handle this
		// — nodeup leaves it alone. We match on the directory
		// component rather than a leading-slash prefix because
		// the user's home is somewhere on the path.
		return SystemNodeOSPackage
	}

	return SystemNodeUnknown
}

// looksLikeHomebrewCoreLayout returns true when a path under
// /usr/local looks like it's the Homebrew wrapper rather than a
// plain "compiled and dropped into /usr/local" install. The
// caller is responsible for restricting this to darwin — Linux's
// /usr/local/bin is overwhelmingly a manual-install location.
//
// Without evaluating the symlink itself (which is sensitive to the
// user's actual filesystem), we approximate with the directory
// layout: `…/bin/node` is too generic to call, so we return true
// only when the path matches patterns we know Homebrew uses:
//
//   - /usr/local/bin/node                       (wrapper symlink)
//
// The earlier Cellar prefix check in classifySystemNodePath has
// already handled the real-binary case (/usr/local/Cellar/node/...);
// this helper exists to confirm the wrapper. On Intel macOS,
// Homebrew always uses /usr/local/bin/node as its wrapper, so any
// path ending in /bin/node under /usr/local is taken to mean the
// Homebrew shim.
func looksLikeHomebrewCoreLayout(s string) bool {
	// Any path ending in /bin/node under /usr/local is the
	// Homebrew wrapper. Without that suffix (e.g., /usr/local/bin/
	// other-tool), we don't make a claim.
	return strings.HasSuffix(s, "/bin/node") || strings.HasSuffix(s, "/node")
}

// looksLikeNVMInstall is a best-effort guard for the rare case
// where NVM_DIR is set to /usr/local/nvm. We don't want to classify
// /usr/local/nvm/versions/node/<v>/bin/node as an OS-package install
// just because its ancestor is /usr/local. Today this is more
// diagnostic than enforced — the path-prefix check is the primary
// discriminator — but the function is here so a future improvement
// can hook this signal without churning callers.
//
// The actual detection: if the path contains "/.nvm/versions/node/"
// anywhere, it's an nvm install regardless of where NVM_DIR points.
// Same for fnm, volta, asdf, mise, n, nodenv.
func looksLikeNVMInstall(cleanPath string) bool {
	return strings.Contains(cleanPath, string(filepath.Separator)+".nvm"+string(filepath.Separator)) ||
		strings.Contains(cleanPath, string(filepath.Separator)+"nvm"+string(filepath.Separator)+"versions")
}

// isInside reports whether child (an absolute path) is the same as
// or a descendant of parent (also absolute). It uses filepath.Rel
// so leading ".." segments produced by a non-absolute parent
// surface as "not inside". Both arguments must be cleaned first
// (filepath.Rel would otherwise be confused by trailing slash).
func isInside(child, parent string) bool {
	if child == "" || parent == "" {
		return false
	}
	cleanParent := filepath.Clean(parent)
	rel, err := filepath.Rel(cleanParent, child)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	if strings.HasPrefix(rel, "..") || strings.HasPrefix(rel, string(filepath.Separator)+"..") {
		return false
	}
	// Any remaining ".." segment also counts as outside.
	for _, seg := range strings.Split(rel, string(filepath.Separator)) {
		if seg == ".." {
			return false
		}
	}
	return true
}

// isUnder is the slash-normalized version of isInside. Useful when
// comparing the post-ToSlash path string against hard-coded prefixes
// containing "/".
func isUnder(s, prefix string) bool {
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	return strings.HasPrefix(s, prefix)
}

// managerManagedRoots returns the filesystem roots under which a
// given manager stores its Node installs. We try to resolve at
// runtime via environment variables when the manager doesn't
// expose a direct getter; for paths the manager doesn't expose,
// this function returns ok=false and the classifier falls back to
// path patterns.
//
// Each root is a directory: a resolved Node binary whose path
// lives at or under this root is considered manager-managed.
//
// We keep this conservative: an empty slice with ok=true means
// "this manager exists, but we don't know its root — fall through
// to path-based classification". An ok=false result is reserved
// for "manager is nil, please don't try to attribute".
func managerManagedRoots(m Manager) (roots []string, ok bool) {
	if m == nil {
		return nil, false
	}
	switch m.Name() {
	case "fnm":
		// fnm defaults to $XDG_DATA_HOME/fnm on Linux and
		// ~/Library/Application Support/fnm on macOS, with
		// ~/.fnm still respected as a legacy path. We enumerate
		// every plausible root so the path classifier still
		// recognizes an fnm-managed install when FNM_DIR is
		// unset. The env override is checked first.
		if d := strings.TrimSpace(getenv("FNM_DIR")); d != "" {
			return []string{d}, true
		}
		roots := []string{}
		if h, err := userHomeDir(); err == nil {
			roots = append(roots,
				filepath.Join(h, ".fnm"),
				filepath.Join(h, "Library", "Application Support", "fnm"),
			)
			if xdg := strings.TrimSpace(getenv("XDG_DATA_HOME")); xdg != "" {
				roots = append(roots, filepath.Join(xdg, "fnm"))
			} else {
				roots = append(roots, filepath.Join(h, ".local", "share", "fnm"))
			}
		}
		return roots, true
	case "nvm":
		if d := strings.TrimSpace(getenv("NVM_DIR")); d != "" {
			return []string{d}, true
		}
		if h, err := userHomeDir(); err == nil {
			return []string{filepath.Join(h, ".nvm")}, true
		}
		return []string{}, true
	case "volta":
		if d := strings.TrimSpace(getenv("VOLTA_HOME")); d != "" {
			return []string{d}, true
		}
		if h, err := userHomeDir(); err == nil {
			return []string{filepath.Join(h, ".volta")}, true
		}
		return []string{}, true
	case "asdf":
		if d := strings.TrimSpace(getenv("ASDF_DIR")); d != "" {
			return []string{d}, true
		}
		if h, err := userHomeDir(); err == nil {
			return []string{filepath.Join(h, ".asdf")}, true
		}
		return []string{}, true
	case "mise":
		// mise stores installs under XDG_DATA_HOME/mise (default
		// ~/.local/share/mise). Override via MISE_DATA_DIR.
		if d := strings.TrimSpace(getenv("MISE_DATA_DIR")); d != "" {
			return []string{d}, true
		}
		if h, err := userHomeDir(); err == nil {
			return []string{filepath.Join(h, ".local", "share", "mise")}, true
		}
		return []string{}, true
	case "n":
		// n defaults to ~/n; override via N_PREFIX.
		if d := strings.TrimSpace(getenv("N_PREFIX")); d != "" {
			return []string{d}, true
		}
		if h, err := userHomeDir(); err == nil {
			return []string{filepath.Join(h, "n")}, true
		}
		return []string{}, true
	case "nodenv":
		// nodenv defaults to ~/.nodenv; override via NODENV_ROOT.
		if d := strings.TrimSpace(getenv("NODENV_ROOT")); d != "" {
			return []string{d}, true
		}
		if h, err := userHomeDir(); err == nil {
			return []string{filepath.Join(h, ".nodenv")}, true
		}
		return []string{}, true
	}
	// nvm-windows, scoop, and any unknown manager: no recognized
	// root pattern; fall through to path-based classification.
	return []string{}, false
}

// WarnSystemNode writes a one-paragraph user-facing warning to w when
// info identifies a node binary that nodeup cannot (and should not)
// manage. Returns the warning text — same string sent to w — so the
// caller can choose to record it (e.g., in JSON output) instead of
// printing it.
//
// Returns ("", false) when no warning is warranted:
//
//   - info.Kind == SystemNodeManaged: this is exactly what nodeup
//     upgrades; no warning needed.
//   - info.Kind == SystemNodeUnknown and info.Path == "": nothing
//     detected; that's a different problem (no node at all) and is
//     handled elsewhere (ErrNoNodeOnPATH).
//   - info.Kind == SystemNodeUnknown and info.Path != "": path
//     didn't match any known layout; we emit a soft warning so the
//     user can decide.
//
// The text is plain prose by design: integration tests assert on
// substrings, and `nodeup upgrade` and `nodeup check` both render
// it as-is into their tabular output.
func WarnSystemNode(w io.Writer, info SystemNodeInfo) (string, bool) {
	if info.Kind == SystemNodeManaged {
		return "", false
	}
	if info.Path == "" {
		return "", false
	}

	var (
		header string
		why    string
		how    string
	)

	switch info.Kind {
	case SystemNodeOSPackage:
		header = "Warning: detected an OS-installed Node.js."
		why = fmt.Sprintf(
			"`node` on PATH (%s) was installed by the operating system's package manager.\nnodeup will not overwrite it — your package manager owns this binary and would\nreplace it on the next system update.",
			info.Path,
		)
		how = systemNodeOSPackageHint()
	case SystemNodeSnap:
		header = "Warning: detected a snap-installed Node.js."
		why = fmt.Sprintf(
			"`node` on PATH (%s) is a snap package. snap confinement prevents nodeup\nfrom swapping in a different version.",
			info.Path,
		)
		how = "Run `snap refresh node` (or `sudo snap refresh node`) to upgrade."
	case SystemNodeFlatpak:
		header = "Warning: detected a flatpak Node.js runtime."
		why = fmt.Sprintf(
			"`node` on PATH (%s) is a flatpak runtime. flatpak install sandboxes prevent\nnodeup from managing the runtime's internal Node version.",
			info.Path,
		)
		how = "Update via `flatpak update` or remove the flatpak and use a manager instead."
	case SystemNodeHomebrewCore:
		header = "Warning: detected a Homebrew-core Node.js."
		why = fmt.Sprintf(
			"`node` on PATH (%s) is the homebrew-core formula. nodeup cannot replace\nit because Homebrew owns the symlink chain and would recreate it on `brew update`.",
			info.Path,
		)
		how = "Run `brew upgrade node`, or `brew uninstall node` and let nodeup manage a\nmanager-installed Node going forward."
	default:
		header = "Warning: detected a Node.js nodeup does not recognize."
		why = fmt.Sprintf(
			"`node` on PATH (%s) did not match a known version-manager or system install.\nnodeup cannot determine whether it is safe to overwrite.",
			info.Path,
		)
		how = "If this is a system-managed Node, use your OS package manager to upgrade it.\nIf it belongs to a version manager nodeup doesn't know about, please open an issue."
	}

	text := fmt.Sprintf("%s\n\n%s\n\n%s\n", header, why, how)
	_, _ = io.WriteString(w, text)
	return text, true
}

// systemNodeOSPackageHint returns a hint string tailored to the
// platform: brew on macOS, apt on Debian/Ubuntu, dnf on Fedora,
// winget/scoop/choco on Windows.
func systemNodeOSPackageHint() string {
	switch runtime.GOOS {
	case "darwin":
		return "If you used Homebrew to install Node, run `brew upgrade node`.\nOtherwise the OS-bundled binary will come back on the next macOS update."
	case "windows":
		return "Re-run the official Node.js MSI from nodejs.org to upgrade, or use\n`winget upgrade Node.js`. If you used scoop/choco, run `scoop update node`\nor `choco upgrade nodejs`."
	case "linux":
		return "Use your distribution's package manager: `sudo apt upgrade nodejs`,\n`sudo dnf upgrade nodejs`, or your distro's equivalent."
	default:
		return "Use your operating system's package manager to upgrade."
	}
}

// Marker consts used by tests to assert the warning text contains the
// expected fragments. These are package-private so they don't leak
// into the public API surface.
const (
	_systemNodeOSPkgHintDarwin = "brew upgrade node"
	_systemNodeOSPkgHintLinux  = "sudo apt upgrade nodejs"
	_systemNodeOSPkgHintWin    = "winget upgrade Node.js"
)
