package detector

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Masterminds/semver/v3"
)

// --- SystemNodeKind.String --------------------------------------------------

func TestSystemNodeKindString(t *testing.T) {
	cases := []struct {
		in   SystemNodeKind
		want string
	}{
		{SystemNodeUnknown, "unknown"},
		{SystemNodeOSPackage, "os-package"},
		{SystemNodeSnap, "snap"},
		{SystemNodeFlatpak, "flatpak"},
		{SystemNodeHomebrewCore, "homebrew-core"},
		{SystemNodeManaged, "manager"},
		{SystemNodeKind(99), "kind(99)"},
	}
	for _, c := range cases {
		if got := c.in.String(); got != c.want {
			t.Errorf("SystemNodeKind(%d).String() = %q, want %q", int(c.in), got, c.want)
		}
	}
}

// --- classifySystemNodePath ------------------------------------------------

func TestClassifySystemNodePath(t *testing.T) {
	cases := []struct {
		name string
		path string
		want SystemNodeKind
	}{
		// Snap variants — /snap/bin/ wrapper, /snap/node/<rev>/, and
		// /var/lib/snapd/snap/node/<rev>/ (the snapd-internal layout).
		{name: "snap wrapper", path: "/snap/bin/node", want: SystemNodeSnap},
		{name: "snap internal", path: "/snap/node/1234/bin/node", want: SystemNodeSnap},
		{name: "snapd layout", path: "/var/lib/snapd/snap/node/1234/bin/node", want: SystemNodeSnap},

		// Flatpak runtimes.
		{name: "flatpak runtime", path: "/var/lib/flatpak/runtime/node/x86_64/stable/active/files/bin/node", want: SystemNodeFlatpak},
		{name: "flatpak libexec", path: "/usr/libexec/flatpak/abc/bin/node", want: SystemNodeFlatpak},
		{name: "flatpak lib", path: "/usr/lib/flatpak/abc/bin/node", want: SystemNodeFlatpak},

		// Homebrew core — Intel (/usr/local), Apple Silicon (/opt/homebrew),
		// Linuxbrew, and the Cellar layout under each. The Intel-mac
		// /usr/local/bin/node wrapper only classifies as Homebrew on
		// darwin; on Linux that path is overwhelmingly a manual
		// `make install` (Homebrew on Linux lives under
		// /home/linuxbrew/.linuxbrew, handled separately).
		{name: "homebrew intel cellar", path: "/usr/local/Cellar/node/22.0.0/bin/node", want: SystemNodeHomebrewCore},
		{name: "homebrew apple silicon cellar", path: "/opt/homebrew/Cellar/node/22.0.0/bin/node", want: SystemNodeHomebrewCore},
		{name: "homebrew apple silicon wrapper", path: "/opt/homebrew/bin/node", want: SystemNodeHomebrewCore},
		{name: "homebrew opt", path: "/opt/homebrew/opt/node/bin/node", want: SystemNodeHomebrewCore},
		{name: "linuxbrew", path: "/home/linuxbrew/.linuxbrew/bin/node", want: SystemNodeHomebrewCore},

		// OS-package — apt/dnf/pacman and the various vendor prefixes.
		{name: "debian usr bin", path: "/usr/bin/node", want: SystemNodeOSPackage},
		{name: "legacy bin", path: "/bin/node", want: SystemNodeOSPackage},
		{name: "suse usr sbin", path: "/usr/sbin/node", want: SystemNodeOSPackage},
		{name: "legacy sbin", path: "/sbin/node", want: SystemNodeOSPackage},
		{name: "vendor opt", path: "/opt/node/bin/node", want: SystemNodeOSPackage},
		{name: "macports", path: "/opt/local/bin/node", want: SystemNodeOSPackage},

		// Windows — official MSI and Scoop.
		{name: "windows msi", path: "C:/Program Files/nodejs/node.exe", want: SystemNodeOSPackage},
		{name: "windows msi x86", path: "C:/Program Files (x86)/nodejs/node.exe", want: SystemNodeOSPackage},
		{name: "windows scoop", path: "C:/Users/me/scoop/apps/nodejs/22.0.0/node.exe", want: SystemNodeOSPackage},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := classifySystemNodePath(c.path); got != c.want {
				t.Errorf("classifySystemNodePath(%q) = %s, want %s", c.path, got, c.want)
			}
		})
	}
}

func TestClassifySystemNodePath_PlatformSpecific(t *testing.T) {
	// `/usr/local/bin/node` is the Homebrew wrapper on macOS but a
	// manual `make install` on Linux. The classifier branches on
	// runtime.GOOS, so the expected result depends on the host.
	cases := []struct {
		goos string
		want SystemNodeKind
	}{
		{"darwin", SystemNodeHomebrewCore},
		{"linux", SystemNodeOSPackage},
	}
	for _, c := range cases {
		t.Run(c.goos, func(t *testing.T) {
			if runtime.GOOS != c.goos {
				t.Skipf("skipping: host is %s, not %s", runtime.GOOS, c.goos)
			}
			if got := classifySystemNodePath("/usr/local/bin/node"); got != c.want {
				t.Errorf("classifySystemNodePath(%q) on %s = %s, want %s",
					"/usr/local/bin/node", runtime.GOOS, got, c.want)
			}
		})
	}
}

func TestClassifySystemNodePath_Unknown(t *testing.T) {
	// Paths that don't match any known layout should resolve to
	// SystemNodeUnknown so the caller can decide what to do.
	cases := []string{
		"",                  // empty
		"/random/path/node", // not under any known prefix
		"/home/user/.nvm/versions/node/22.0.0/bin/node", // nvm-managed; classifier leaves it Unknown (manager-override is ResolveSystemNode's job)
	}
	for _, p := range cases {
		if got := classifySystemNodePath(p); got != SystemNodeUnknown {
			t.Errorf("classifySystemNodePath(%q) = %s, want %s", p, got, SystemNodeUnknown)
		}
	}
}

// --- isInside / isUnder ----------------------------------------------------

func TestIsInside(t *testing.T) {
	cases := []struct {
		child, parent string
		want          bool
	}{
		{"/a/b", "/a/b", true},     // same path
		{"/a/b/c", "/a/b", true},   // direct descendant
		{"/a/b/c/d", "/a/b", true}, // grand-descendant
		{"/a/bb", "/a/b", false},   // sibling-prefix false-positive guard
		{"/a", "/a/b", false},      // parent is descendant, not ancestor
		{"", "/a", false},          // empty child
		{"/a", "", false},          // empty parent
		{"/a/../b", "/a", false},   // ".." segments
		{"/a/b/c/..", "/a", true},  // child cleaned: /a/b → still inside /a
		{"/a/b/./c", "/a/b", true}, // child cleaned: "." segment collapses
	}
	for _, c := range cases {
		if got := isInside(c.child, c.parent); got != c.want {
			t.Errorf("isInside(%q, %q) = %v, want %v", c.child, c.parent, got, c.want)
		}
	}
}

func TestIsUnder(t *testing.T) {
	if !isUnder("/a/b/c", "/a/b") {
		t.Error("isUnder should treat missing trailing slash as prefix-with-slash")
	}
	if !isUnder("/a/b/c", "/a/b/") {
		t.Error("isUnder should match direct descendant")
	}
	if isUnder("/a/bb/c", "/a/b") {
		t.Error("isUnder should NOT match sibling-prefix")
	}
	if isUnder("/a/b", "/a/b") {
		t.Error("isUnder requires the trailing slash; /a/b is NOT under /a/b")
	}
}

// --- ResolveSystemNode -----------------------------------------------------

// stubWhichNode replaces the package-level whichNode seam for the
// duration of the test, returning (path, err) when the test function
// runs and restoring the production value via t.Cleanup.
func stubWhichNode(t *testing.T, fn func(ctx context.Context) (string, error)) {
	t.Helper()
	orig := whichNode
	t.Cleanup(func() { whichNode = orig })
	whichNode = fn
}

func TestResolveSystemNode_NoNodeOnPATH(t *testing.T) {
	stubWhichNode(t, func(ctx context.Context) (string, error) {
		return "", nil // which silently returned empty
	})
	_, err := ResolveSystemNode(context.Background(), nil)
	if !errors.Is(err, ErrNoNodeOnPATH) {
		t.Errorf("err = %v, want ErrNoNodeOnPATH", err)
	}
}

func TestResolveSystemNode_WhichErrorWrapped(t *testing.T) {
	// When whichNode returns both an error and an empty path, the
	// error must be wrapped with ErrNoNodeOnPATH so callers can
	// errors.Is it.
	stubWhichNode(t, func(ctx context.Context) (string, error) {
		return "", errors.New("which failed")
	})
	_, err := ResolveSystemNode(context.Background(), nil)
	if !errors.Is(err, ErrNoNodeOnPATH) {
		t.Errorf("err = %v, want wrap of ErrNoNodeOnPATH", err)
	}
	if !strings.Contains(err.Error(), "which failed") {
		t.Errorf("err message should preserve original: %v", err)
	}
}

func TestResolveSystemNode_PathClassifiedNoManager(t *testing.T) {
	stubWhichNode(t, func(ctx context.Context) (string, error) {
		return "/usr/bin/node", nil
	})
	info, err := ResolveSystemNode(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Path != "/usr/bin/node" {
		t.Errorf("Path = %q, want %q", info.Path, "/usr/bin/node")
	}
	if info.Kind != SystemNodeOSPackage {
		t.Errorf("Kind = %s, want %s", info.Kind, SystemNodeOSPackage)
	}
	if info.Manager != "" {
		t.Errorf("Manager = %q, want empty", info.Manager)
	}
}

func TestResolveSystemNode_ManagerOverrideWins(t *testing.T) {
	// Even though /usr/local/bin/node classifies as HomebrewCore on
	// its own, if it lives under NVM_DIR (a manager-data-dir), the
	// manager-override should win and Kind becomes SystemNodeManaged.
	t.Setenv("NVM_DIR", "/usr/local/nvm")

	stubWhichNode(t, func(ctx context.Context) (string, error) {
		return "/usr/local/nvm/versions/node/v22.0.0/bin/node", nil
	})

	// Build a fake manager that names itself "nvm". We don't need
	// the full Manager interface — just Name() is consulted by
	// managerManagedRoots.
	mgr := fakeManager{name: "nvm"}

	info, err := ResolveSystemNode(context.Background(), mgr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Kind != SystemNodeManaged {
		t.Errorf("Kind = %s, want %s (manager-override should win)", info.Kind, SystemNodeManaged)
	}
	if info.Manager != "nvm" {
		t.Errorf("Manager = %q, want %q", info.Manager, "nvm")
	}
}

func TestResolveSystemNode_ManagerOverrideSkippedWhenPathOutsideRoot(t *testing.T) {
	// The path doesn't actually live under NVM_DIR, so the
	// manager-override should NOT fire — the path classifier
	// decides.
	t.Setenv("NVM_DIR", "/home/me/.nvm")

	stubWhichNode(t, func(ctx context.Context) (string, error) {
		return "/usr/bin/node", nil
	})

	mgr := fakeManager{name: "nvm"}
	info, err := ResolveSystemNode(context.Background(), mgr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Kind != SystemNodeOSPackage {
		t.Errorf("Kind = %s, want %s", info.Kind, SystemNodeOSPackage)
	}
	if info.Manager != "" {
		t.Errorf("Manager = %q, want empty (manager root not matched)", info.Manager)
	}
}

func TestResolveSystemNode_NilManagerSafe(t *testing.T) {
	// Pass nil manager explicitly. Should classify by path only
	// (no manager-override step).
	stubWhichNode(t, func(ctx context.Context) (string, error) {
		return "/usr/bin/node", nil
	})
	info, err := ResolveSystemNode(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Kind != SystemNodeOSPackage {
		t.Errorf("Kind = %s, want %s", info.Kind, SystemNodeOSPackage)
	}
}

// --- managerManagedRoots ---------------------------------------------------

func TestManagerManagedRoots(t *testing.T) {
	// Save and restore userHomeDir seam. We override to a known
	// home dir so the ~/.<name> fallback produces deterministic
	// output regardless of the test runner's HOME.
	origHome := userHomeDir
	t.Cleanup(func() { userHomeDir = origHome })
	userHomeDir = func() (string, error) { return "/home/tester", nil }

	// Clear every env var the production code reads via getenv().
	// A test runner that has fnm/volta/asdf/etc. installed will
	// have these set in the real env, which would make the
	// "fall back to home" assertions noisy. t.Setenv saves and
	// restores each one to its pre-test value.
	for _, v := range []string{
		"FNM_DIR", "NVM_DIR", "VOLTA_HOME", "ASDF_DIR", "ASDF_DATA_DIR",
		"MISE_DATA_DIR", "N_PREFIX", "NODENV_ROOT",
		"XDG_DATA_HOME",
	} {
		t.Setenv(v, "")
	}

	cases := []struct {
		name        string
		mgr         string
		envKey      string
		envVal      string
		wantRoots   []string
		wantOK      bool
		description string
	}{
		{
			name: "fnm env wins over home fallback",
			mgr:  "fnm", envKey: "FNM_DIR", envVal: "/custom/fnm",
			wantRoots:   []string{"/custom/fnm"},
			wantOK:      true,
			description: "env override takes precedence",
		},
		{
			name: "fnm falls back to home (no env)",
			mgr:  "fnm",
			wantRoots: []string{
				filepath.Join("/home/tester", ".fnm"),
				filepath.Join("/home/tester", "Library", "Application Support", "fnm"),
				filepath.Join("/home/tester", ".local", "share", "fnm"),
			},
			wantOK:      true,
			description: "no env → enumerate plausible XDG + legacy roots",
		},
		{
			name: "fnm with XDG_DATA_HOME override",
			mgr:  "fnm", envKey: "XDG_DATA_HOME", envVal: "/srv/data",
			wantRoots: []string{
				filepath.Join("/home/tester", ".fnm"),
				filepath.Join("/home/tester", "Library", "Application Support", "fnm"),
				filepath.Join("/srv/data", "fnm"),
			},
			wantOK:      true,
			description: "XDG_DATA_HOME wins over ~/.local/share",
		},
		{
			name: "nvm env wins",
			mgr:  "nvm", envKey: "NVM_DIR", envVal: "/usr/local/nvm",
			wantRoots:   []string{"/usr/local/nvm"},
			wantOK:      true,
			description: "NVM_DIR is the canonical override",
		},
		{
			name: "volta env wins",
			mgr:  "volta", envKey: "VOLTA_HOME", envVal: "/srv/volta",
			wantRoots:   []string{"/srv/volta"},
			wantOK:      true,
			description: "VOLTA_HOME override",
		},
		{
			name: "asdf env wins",
			mgr:  "asdf", envKey: "ASDF_DATA_DIR", envVal: "/opt/asdf",
			wantRoots:   []string{"/opt/asdf"},
			wantOK:      true,
			description: "ASDF_DATA_DIR override (canonical asdf data-dir env)",
		},
		{
			name: "mise env wins",
			mgr:  "mise", envKey: "MISE_DATA_DIR", envVal: "/srv/mise",
			wantRoots:   []string{"/srv/mise"},
			wantOK:      true,
			description: "MISE_DATA_DIR override",
		},
		{
			name: "n env wins",
			mgr:  "n", envKey: "N_PREFIX", envVal: "/opt/n",
			wantRoots:   []string{"/opt/n"},
			wantOK:      true,
			description: "N_PREFIX override",
		},
		{
			name: "nodenv env wins",
			mgr:  "nodenv", envKey: "NODENV_ROOT", envVal: "/srv/nodenv",
			wantRoots:   []string{"/srv/nodenv"},
			wantOK:      true,
			description: "NODENV_ROOT override",
		},
		{
			name:        "mise falls back to XDG default",
			mgr:         "mise",
			wantRoots:   []string{filepath.Join("/home/tester", ".local", "share", "mise")},
			wantOK:      true,
			description: "no env → ~/.local/share/mise (XDG default)",
		},
		{
			name:        "unknown manager returns ok=false",
			mgr:         "futuremgr",
			wantRoots:   nil,
			wantOK:      false,
			description: "we don't recognize the manager, so the classifier falls through to path patterns",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.envKey != "" {
				t.Setenv(c.envKey, c.envVal)
			}
			gotRoots, gotOK := managerManagedRoots(fakeManager{name: c.mgr})
			if gotOK != c.wantOK {
				t.Errorf("ok = %v, want %v (%s)", gotOK, c.wantOK, c.description)
			}
			if !equalStringSlices(gotRoots, c.wantRoots) {
				t.Errorf("roots = %v, want %v (%s)", gotRoots, c.wantRoots, c.description)
			}
		})
	}
}

func TestManagerManagedRoots_NilManager(t *testing.T) {
	gotRoots, gotOK := managerManagedRoots(nil)
	if gotOK {
		t.Errorf("ok = true, want false for nil manager")
	}
	if gotRoots != nil {
		t.Errorf("roots = %v, want nil for nil manager", gotRoots)
	}
}

// --- WarnSystemNode --------------------------------------------------------

func TestWarnSystemNode_ManagedReturnsFalse(t *testing.T) {
	var buf bytes.Buffer
	text, ok := WarnSystemNode(&buf, SystemNodeInfo{
		Path:    "/home/me/.fnm/node-versions/v22.0.0/installation/bin/node",
		Kind:    SystemNodeManaged,
		Manager: "fnm",
	})
	if ok {
		t.Errorf("ok = true, want false for SystemNodeManaged")
	}
	if text != "" {
		t.Errorf("text = %q, want empty", text)
	}
	if buf.Len() != 0 {
		t.Errorf("buf = %q, want empty writer output", buf.String())
	}
}

func TestWarnSystemNode_EmptyPathReturnsFalse(t *testing.T) {
	var buf bytes.Buffer
	text, ok := WarnSystemNode(&buf, SystemNodeInfo{Path: "", Kind: SystemNodeUnknown})
	if ok {
		t.Errorf("ok = true, want false for empty path")
	}
	if text != "" {
		t.Errorf("text = %q, want empty", text)
	}
}

func TestWarnSystemNode_OSPackage(t *testing.T) {
	var buf bytes.Buffer
	text, ok := WarnSystemNode(&buf, SystemNodeInfo{
		Path: "/usr/bin/node",
		Kind: SystemNodeOSPackage,
	})
	if !ok {
		t.Fatalf("ok = false, want true")
	}
	if text == "" {
		t.Fatalf("text = empty, want non-empty warning")
	}
	// The text should round-trip: same string written to the
	// buffer as returned to the caller.
	if buf.String() != text {
		t.Errorf("buf.String() != text; buf=%q text=%q", buf.String(), text)
	}
	// Header is stable.
	if !strings.Contains(text, "Warning: detected an OS-installed Node.js.") {
		t.Errorf("text missing OS-package header: %q", text)
	}
	// Path appears verbatim.
	if !strings.Contains(text, "/usr/bin/node") {
		t.Errorf("text missing path: %q", text)
	}
	// The platform-tailored hint must match the current OS.
	switch runtime.GOOS {
	case "darwin":
		if !strings.Contains(text, _systemNodeOSPkgHintDarwin) {
			t.Errorf("darwin hint missing: %q", text)
		}
	case "windows":
		if !strings.Contains(text, _systemNodeOSPkgHintWin) {
			t.Errorf("windows hint missing: %q", text)
		}
	default:
		if !strings.Contains(text, _systemNodeOSPkgHintLinux) {
			t.Errorf("linux hint missing: %q", text)
		}
	}
}

func TestWarnSystemNode_Snap(t *testing.T) {
	var buf bytes.Buffer
	text, ok := WarnSystemNode(&buf, SystemNodeInfo{
		Path: "/snap/bin/node",
		Kind: SystemNodeSnap,
	})
	if !ok {
		t.Fatalf("ok = false, want true")
	}
	if !strings.Contains(text, "snap package") {
		t.Errorf("text missing 'snap package': %q", text)
	}
	if !strings.Contains(text, "snap refresh node") {
		t.Errorf("text missing snap refresh hint: %q", text)
	}
}

func TestWarnSystemNode_Flatpak(t *testing.T) {
	var buf bytes.Buffer
	text, ok := WarnSystemNode(&buf, SystemNodeInfo{
		Path: "/var/lib/flatpak/runtime/node/x86_64/stable/active/files/bin/node",
		Kind: SystemNodeFlatpak,
	})
	if !ok {
		t.Fatalf("ok = false, want true")
	}
	if !strings.Contains(text, "flatpak runtime") {
		t.Errorf("text missing 'flatpak runtime': %q", text)
	}
	if !strings.Contains(text, "flatpak update") {
		t.Errorf("text missing flatpak update hint: %q", text)
	}
}

func TestWarnSystemNode_HomebrewCore(t *testing.T) {
	var buf bytes.Buffer
	text, ok := WarnSystemNode(&buf, SystemNodeInfo{
		Path: "/usr/local/bin/node",
		Kind: SystemNodeHomebrewCore,
	})
	if !ok {
		t.Fatalf("ok = false, want true")
	}
	if !strings.Contains(text, "homebrew-core formula") {
		t.Errorf("text missing homebrew-core reference: %q", text)
	}
	if !strings.Contains(text, "brew upgrade node") {
		t.Errorf("text missing brew upgrade hint: %q", text)
	}
}

func TestWarnSystemNode_UnknownKindSoftWarning(t *testing.T) {
	var buf bytes.Buffer
	text, ok := WarnSystemNode(&buf, SystemNodeInfo{
		Path: "/totally/unexpected/node",
		Kind: SystemNodeUnknown,
	})
	if !ok {
		t.Fatalf("ok = false, want true (soft warning)")
	}
	if !strings.Contains(text, "does not recognize") {
		t.Errorf("text missing soft-warning header: %q", text)
	}
	// Should NOT contain the platform-specific hint, since the
	// classifier doesn't know which package manager the user has.
	if strings.Contains(text, "brew upgrade") {
		t.Errorf("unknown-kind warning should not assume brew: %q", text)
	}
}

func TestWarnSystemNode_WritesToWriter(t *testing.T) {
	// Use io.Discard as a sanity check that the function actually
	// writes something rather than just returning a string.
	written := captureWriter(func(w io.Writer) {
		_, _ = WarnSystemNode(w, SystemNodeInfo{
			Path: "/usr/bin/node",
			Kind: SystemNodeOSPackage,
		})
	})
	if written == "" {
		t.Error("WarnSystemNode wrote nothing to the writer")
	}
}

// --- helpers ---------------------------------------------------------------

// fakeManager is a minimal Manager implementation that returns
// whatever Name() was given. We only need Name() for the
// manager-override path; every other method is required by the
// interface but never called by the system-node tests, so they
// return zero values.
type fakeManager struct{ name string }

func (f fakeManager) Name() string                                   { return f.name }
func (f fakeManager) Detect() bool                                   { return true }
func (f fakeManager) Version() (string, error)                       { return "0.0.0-test", nil }
func (f fakeManager) ListInstalled() ([]semver.Version, error)       { return nil, nil }
func (f fakeManager) Install(semver.Version) error                   { return nil }
func (f fakeManager) Uninstall(semver.Version) error                 { return nil }
func (f fakeManager) Use(semver.Version) error                       { return nil }
func (f fakeManager) SetDefault(semver.Version) error                { return nil }
func (f fakeManager) GlobalNpmPrefix(semver.Version) (string, error) { return "", nil }
func (f fakeManager) Current() (semver.Version, error)               { return semver.Version{}, nil }

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func captureWriter(fn func(io.Writer)) string {
	var buf bytes.Buffer
	fn(&buf)
	return buf.String()
}

// --- smoke test: ensure the production whichNode seam still
// compiles and reads first-line output correctly when invoked
// against a fake script.

func TestWhichNode_DefaultsToLookPath(t *testing.T) {
	// Sanity-check the production whichNode seam (which uses
	// exec.LookPath under the hood): look up `node` on PATH and
	// assert it returns a non-empty absolute path. We skip rather
	// than fail when `node` is absent — minimal CI images and the
	// `go test` host may not have node installed.
	if runtime.GOOS == "windows" {
		t.Skip("path semantics differ on windows; the resolution path is exercised in CI")
	}
	p, err := whichNode(context.Background())
	if err != nil {
		t.Skipf("whichNode failed (no `node` on PATH?): %v", err)
	}
	if p == "" {
		t.Skip("whichNode returned empty (no `node` on PATH)")
	}
	if !filepath.IsAbs(p) {
		t.Errorf("whichNode path is not absolute: %q", p)
	}
}
