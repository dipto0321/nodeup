package detector

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Masterminds/semver/v3"

	"github.com/dipto0321/nodeup/internal/platform"
)

// --- parseVitePlusVersion ------------------------------------------------

func TestParseVitePlusVersion_HeaderLine(t *testing.T) {
	// Observed on vp 1.4.0:
	//   $ vp --version
	//   vp v1.4.0
	//
	//   Local vite-plus:
	//     vite-plus  Not found
	//   ...
	got, err := parseVitePlusVersion("vp v1.4.0\n\nLocal vite-plus:\n  vite-plus  Not found\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "1.4.0" {
		t.Errorf("got %q, want %q", got, "1.4.0")
	}
}

func TestParseVitePlusVersion_BareVersion(t *testing.T) {
	// Defensive: some builds/wrappers print the version on its own.
	got, err := parseVitePlusVersion("1.4.0\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "1.4.0" {
		t.Errorf("got %q, want %q", got, "1.4.0")
	}
}

func TestParseVitePlusVersion_SkipsPreamble(t *testing.T) {
	// A blank first line must not stop the scan.
	got, err := parseVitePlusVersion("\n\nvp v1.4.0\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "1.4.0" {
		t.Errorf("got %q, want %q", got, "1.4.0")
	}
}

func TestParseVitePlusVersion_Empty(t *testing.T) {
	if _, err := parseVitePlusVersion(""); err == nil {
		t.Fatal("expected error for empty output, got nil")
	}
}

func TestParseVitePlusVersion_NoVersionToken(t *testing.T) {
	if _, err := parseVitePlusVersion("Local vite-plus:\n  vite-plus  Not found\n"); err == nil {
		t.Fatal("expected error for output without a version token, got nil")
	}
}

// --- parseVitePlusInstalled ----------------------------------------------

func TestParseVitePlusInstalled_HappyPath(t *testing.T) {
	// Real observed output of `vp env list node --json`:
	//   {"node":[{"version":"24.22.1","current":false,"default":false},
	//             {"version":"26.13.0","current":true,"default":true}]}
	out := `{"node":[{"version":"24.22.1","current":false,"default":false},{"version":"26.13.0","current":true,"default":true}]}`
	got, err := parseVitePlusInstalled(out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (%v)", len(got), got)
	}
	// Sorted ascending.
	if got[0].String() != "24.22.1" {
		t.Errorf("got[0] = %q, want %q", got[0].String(), "24.22.1")
	}
	if got[1].String() != "26.13.0" {
		t.Errorf("got[1] = %q, want %q", got[1].String(), "26.13.0")
	}
}

func TestParseVitePlusInstalled_SortsAscending(t *testing.T) {
	out := `{"node":[{"version":"26.13.0"},{"version":"20.18.0"},{"version":"24.22.1"}]}`
	got, err := parseVitePlusInstalled(out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"20.18.0", "24.22.1", "26.13.0"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].String() != w {
			t.Errorf("got[%d] = %q, want %q", i, got[i].String(), w)
		}
	}
}

func TestParseVitePlusInstalled_EmptyArray(t *testing.T) {
	got, err := parseVitePlusInstalled(`{"node":[]}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("got = nil, want a non-nil empty slice")
	}
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}

func TestParseVitePlusInstalled_MissingNodeKey(t *testing.T) {
	// Vite+ omits the "node" key entirely when no runtimes are
	// installed.
	got, err := parseVitePlusInstalled(`{}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("got = nil, want a non-nil empty slice")
	}
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}

func TestParseVitePlusInstalled_EmptyStdout(t *testing.T) {
	got, err := parseVitePlusInstalled("   \n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("got = nil, want a non-nil empty slice")
	}
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}

func TestParseVitePlusInstalled_SkipsUnparseableVersions(t *testing.T) {
	// A future metadata format must not break `nodeup list` — skip
	// the entries we can't parse and keep the rest.
	out := `{"node":[{"version":""},{"version":"not-a-version"},{"version":"22.11.0"}]}`
	got, err := parseVitePlusInstalled(out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1 (%v)", len(got), got)
	}
	if got[0].String() != "22.11.0" {
		t.Errorf("got[0] = %q, want %q", got[0].String(), "22.11.0")
	}
}

func TestParseVitePlusInstalled_InvalidJSON(t *testing.T) {
	if _, err := parseVitePlusInstalled("not json"); err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

// --- parseVitePlusCurrent ------------------------------------------------

func TestParseVitePlusCurrent_ManagedMode(t *testing.T) {
	// Real observed output of `vp env current node --json`:
	//   {"node":{"version":"26.13.0","source":"lts","installed":true,"mode":"managed"}}
	out := `{"node":{"version":"26.13.0","source":"lts","installed":true,"mode":"managed"}}`
	got, err := parseVitePlusCurrent(out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.String() != "26.13.0" {
		t.Errorf("got %q, want %q", got.String(), "26.13.0")
	}
}

func TestParseVitePlusCurrent_SystemFirstModeFails(t *testing.T) {
	// In system-first mode the reported version is the SYSTEM Node,
	// not a Vite+-managed runtime. Reporting it would let nodeup
	// wrongly treat a system install as the active managed version,
	// so Current() must fail (nodeup then fails closed — issue #58).
	out := `{"node":{"version":"22.11.0","source":"system PATH","installed":false,"mode":"system_first"}}`
	if _, err := parseVitePlusCurrent(out); err == nil {
		t.Fatal("expected error in system_first mode, got nil")
	}
}

func TestParseVitePlusCurrent_MissingNodeObject(t *testing.T) {
	if _, err := parseVitePlusCurrent(`{"node":null}`); err == nil {
		t.Fatal("expected error for null node object, got nil")
	}
	if _, err := parseVitePlusCurrent(`{}`); err == nil {
		t.Fatal("expected error for missing node object, got nil")
	}
}

func TestParseVitePlusCurrent_EmptyOutput(t *testing.T) {
	if _, err := parseVitePlusCurrent(""); err == nil {
		t.Fatal("expected error for empty output, got nil")
	}
}

func TestParseVitePlusCurrent_InvalidJSON(t *testing.T) {
	if _, err := parseVitePlusCurrent("not json"); err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestParseVitePlusCurrent_EmptyVersion(t *testing.T) {
	out := `{"node":{"version":"","source":"lts","installed":true,"mode":"managed"}}`
	if _, err := parseVitePlusCurrent(out); err == nil {
		t.Fatal("expected error for empty version, got nil")
	}
}

func TestParseVitePlusCurrent_NonSemverVersion(t *testing.T) {
	out := `{"node":{"version":"not-a-version","source":"lts","installed":true,"mode":"managed"}}`
	if _, err := parseVitePlusCurrent(out); err == nil {
		t.Fatal("expected error for non-semver version, got nil")
	}
}

// --- Version / ListInstalled methods -------------------------------------

func TestVitePlus_Version(t *testing.T) {
	var captured string
	withStubShell(t, nil, func(req string) (*platform.RunResult, error) {
		captured = req
		return &platform.RunResult{Stdout: "vp v1.4.0\n"}, nil
	})

	got, err := NewVitePlus().Version()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "1.4.0" {
		t.Errorf("got %q, want %q", got, "1.4.0")
	}
	if captured != "vp --version" {
		t.Errorf("invoked %q, want %q", captured, "vp --version")
	}
}

func TestVitePlus_Version_PropagatesError(t *testing.T) {
	wantErr := errors.New("simulated vp failure")
	withStubShell(t, nil, func(req string) (*platform.RunResult, error) {
		return nil, wantErr
	})

	_, err := NewVitePlus().Version()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("error %v should wrap %v", err, wantErr)
	}
}

func TestVitePlus_ListInstalled(t *testing.T) {
	var captured string
	withStubShell(t, nil, func(req string) (*platform.RunResult, error) {
		captured = req
		return &platform.RunResult{Stdout: `{"node":[{"version":"26.13.0"}]}`}, nil
	})

	got, err := NewVitePlus().ListInstalled(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].String() != "26.13.0" {
		t.Fatalf("got %v, want [26.13.0]", got)
	}
	if captured != "vp env list node --json" {
		t.Errorf("invoked %q, want %q", captured, "vp env list node --json")
	}
}

func TestVitePlus_ListInstalled_PropagatesError(t *testing.T) {
	wantErr := errors.New("simulated vp failure")
	withStubShell(t, nil, func(req string) (*platform.RunResult, error) {
		return nil, wantErr
	})

	_, err := NewVitePlus().ListInstalled(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("error %v should wrap %v", err, wantErr)
	}
}

func TestVitePlus_Current(t *testing.T) {
	var captured string
	withStubShell(t, nil, func(req string) (*platform.RunResult, error) {
		captured = req
		return &platform.RunResult{Stdout: `{"node":{"version":"26.13.0","mode":"managed"}}`}, nil
	})

	got, err := NewVitePlus().Current(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.String() != "26.13.0" {
		t.Errorf("got %q, want %q", got.String(), "26.13.0")
	}
	if captured != "vp env current node --json" {
		t.Errorf("invoked %q, want %q", captured, "vp env current node --json")
	}
}

func TestVitePlus_Current_PropagatesError(t *testing.T) {
	wantErr := errors.New("simulated vp failure")
	withStubShell(t, nil, func(req string) (*platform.RunResult, error) {
		return nil, wantErr
	})

	_, err := NewVitePlus().Current(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("error %v should wrap %v", err, wantErr)
	}
}

// --- Mutation methods ----------------------------------------------------

func TestVitePlus_MutationMethodsInvokeShell(t *testing.T) {
	// Vite+ takes the qualified `node@<v>` spec form for every
	// mutation. A bare version would be ambiguous.
	v := NewVitePlus()
	ver := semver.MustParse("22.11.0")

	type tc struct {
		name    string
		call    func() error
		wantArg string
	}
	cases := []tc{
		{"Install", func() error { return v.Install(*ver) }, "env install node@22.11.0"},
		{"Uninstall", func() error { return v.Uninstall(*ver) }, "env uninstall node@22.11.0"},
		{"Use", func() error { return v.Use(*ver) }, "env use node@22.11.0"},
		{"SetDefault", func() error { return v.SetDefault(*ver) }, "env default node@22.11.0"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var captured string
			withStubShell(t, nil, func(req string) (*platform.RunResult, error) {
				captured = req
				return &platform.RunResult{}, nil
			})
			if err := c.call(); err != nil {
				t.Fatalf("%s: unexpected error: %v", c.name, err)
			}
			want := "vp " + c.wantArg
			if captured != want {
				t.Errorf("%s invoked %q, want %q", c.name, captured, want)
			}
		})
	}
}

func TestVitePlus_MutationMethodsPropagateError(t *testing.T) {
	wantErr := errors.New("simulated vp failure")
	v := NewVitePlus()
	ver := semver.MustParse("22.11.0")

	cases := []struct {
		name string
		call func() error
	}{
		{"Install", func() error { return v.Install(*ver) }},
		{"Uninstall", func() error { return v.Uninstall(*ver) }},
		{"Use", func() error { return v.Use(*ver) }},
		{"SetDefault", func() error { return v.SetDefault(*ver) }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			withStubShell(t, nil, func(req string) (*platform.RunResult, error) {
				return nil, wantErr
			})
			err := c.call()
			if err == nil {
				t.Fatalf("%s: expected error, got nil", c.name)
			}
			if !errors.Is(err, wantErr) {
				t.Errorf("%s: error %v should wrap %v", c.name, err, wantErr)
			}
		})
	}
}

// --- Detect --------------------------------------------------------------

func TestVitePlus_Detect_NoBinaryOnPath(t *testing.T) {
	// No `vp` binary on PATH and no Vite+ data dir. Detect() must
	// return false — and must not shell out.
	//
	// We point every candidate data dir at an empty temp dir so a
	// developer machine (or CI runner) with Vite+ installed doesn't
	// trip the on-disk branch.
	t.Setenv("PATH", t.TempDir())
	clearVitePlusEnv(t)
	empty := t.TempDir()
	t.Setenv("VP_HOME", empty)
	t.Setenv("VP_DATA_DIR", empty)
	t.Setenv("XDG_DATA_HOME", empty)
	t.Setenv("LOCALAPPDATA", empty)
	isolateVitePlusHome(t, empty)

	orig := runShell
	runShell = func(ctx context.Context, name string, a ...string) (*platform.RunResult, error) {
		t.Fatalf("Detect() must not invoke runShell (was called with %s %v)", name, a)
		return nil, nil
	}
	t.Cleanup(func() { runShell = orig })

	if NewVitePlus().Detect() {
		t.Error("Detect() = true with no vp installed, want false")
	}
}

func TestVitePlus_Detect_FindsBinaryOnPath(t *testing.T) {
	binDir := t.TempDir()
	binName := "vp"
	if runtime.GOOS == "windows" {
		binName = "vp.exe"
	}
	if err := os.WriteFile(filepath.Join(binDir, binName), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	clearVitePlusEnv(t)
	isolateVitePlusHome(t, t.TempDir())

	orig := runShell
	called := false
	runShell = func(ctx context.Context, name string, a ...string) (*platform.RunResult, error) {
		called = true
		return nil, nil
	}
	t.Cleanup(func() { runShell = orig })

	if !NewVitePlus().Detect() {
		t.Error("Detect() = false with vp on PATH, want true")
	}
	if called {
		t.Error("Detect() invoked runShell — must be a pure on-disk check")
	}
}

func TestVitePlus_Detect_FindsBinaryInDataDir(t *testing.T) {
	// `vp` isn't on PATH, but <VP_HOME>/bin/vp exists on disk.
	binDir := t.TempDir()
	t.Setenv("PATH", t.TempDir())
	clearVitePlusEnv(t)
	t.Setenv("VP_HOME", binDir)

	binName := "vp"
	if runtime.GOOS == "windows" {
		binName = "vp.exe"
	}
	if err := os.MkdirAll(filepath.Join(binDir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "bin", binName), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	if !NewVitePlus().Detect() {
		t.Error("Detect() = false with vp in the data dir, want true")
	}
}

func TestVitePlusBinaryExists(t *testing.T) {
	// The data-dir lookup is a plain filesystem stat, so it has to
	// apply the Windows executable suffix itself (exec.LookPath does
	// that via PATHEXT, but a direct stat does not).
	binDir := t.TempDir()

	if vitePlusBinaryExists(binDir) {
		t.Error("vitePlusBinaryExists = true for an empty dir, want false")
	}

	// A bare "vp" is found on unix. On Windows the real binary is
	// "vp.exe" — check both spellings so the helper's behavior is
	// pinned identically on every OS.
	for _, name := range []string{"vp", "vp.exe"} {
		path := filepath.Join(binDir, name)
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		if !vitePlusBinaryExists(binDir) {
			t.Errorf("vitePlusBinaryExists = false with %s present, want true", name)
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
}

// --- vitePlusDataDirs ----------------------------------------------------

// clearVitePlusEnv blanks every env var vitePlusDataDirs reads so a
// test runner with Vite+ installed doesn't make assertions noisy.
func clearVitePlusEnv(t *testing.T) {
	t.Helper()
	for _, v := range []string{"VP_HOME", "VP_DATA_DIR", "XDG_DATA_HOME", "LOCALAPPDATA"} {
		t.Setenv(v, "")
	}
}

// isolateVitePlusHome points the userHomeDir seam at a throwaway
// directory so the legacy <home>/.vite-plus candidate never resolves
// against the developer's real home.
func isolateVitePlusHome(t *testing.T, dir string) {
	t.Helper()
	orig := userHomeDir
	t.Cleanup(func() { userHomeDir = orig })
	userHomeDir = func() (string, error) { return dir, nil }
}

func TestVitePlusDataDirs_VPHomeWins(t *testing.T) {
	clearVitePlusEnv(t)
	t.Setenv("VP_HOME", "/srv/vite-plus")
	t.Setenv("VP_DATA_DIR", "/srv/vite-plus-data")

	dirs := vitePlusDataDirs()
	if len(dirs) == 0 {
		t.Fatal("dirs = empty, want at least one entry")
	}
	if dirs[0] != "/srv/vite-plus" {
		t.Errorf("dirs[0] = %q, want %q", dirs[0], "/srv/vite-plus")
	}
	if !containsString(dirs, "/srv/vite-plus-data") {
		t.Errorf("dirs = %v, want it to contain %q", dirs, "/srv/vite-plus-data")
	}
}

func TestVitePlusDataDirs_XDGDefault(t *testing.T) {
	clearVitePlusEnv(t)

	dirs := vitePlusDataDirs()
	if len(dirs) == 0 {
		t.Fatal("dirs = empty, want at least one entry")
	}
	want := filepath.Join(homeDirForTest(t), ".local", "share", "vite-plus")
	if platform.IsWindows() {
		want = filepath.Join(homeDirForTest(t), "AppData", "Local", "vite-plus", "data")
	}
	if !containsString(dirs, want) {
		t.Errorf("dirs = %v, want it to contain %q", dirs, want)
	}
}

func TestVitePlusDataDirs_XDGDataHomeOverride(t *testing.T) {
	clearVitePlusEnv(t)
	if platform.IsWindows() {
		t.Skip("XDG_DATA_HOME does not apply on Windows")
	}
	t.Setenv("XDG_DATA_HOME", "/srv/xdg")

	dirs := vitePlusDataDirs()
	want := filepath.Join("/srv/xdg", "vite-plus")
	if !containsString(dirs, want) {
		t.Errorf("dirs = %v, want it to contain %q", dirs, want)
	}
}

func TestVitePlusDataDirs_LegacyHomeOnlyWhenCurrentLinkExists(t *testing.T) {
	clearVitePlusEnv(t)

	home := t.TempDir()
	origHome := userHomeDir
	t.Cleanup(func() { userHomeDir = origHome })
	userHomeDir = func() (string, error) { return home, nil }

	// No "current" link → the legacy root must NOT be reported.
	legacy := filepath.Join(home, vitePlusLegacyDirName)
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	dirs := vitePlusDataDirs()
	if containsString(dirs, legacy) {
		t.Errorf("dirs = %v, want it to omit %q (no current link)", dirs, legacy)
	}

	// With a "current" entry present → the legacy root is a candidate.
	if err := os.WriteFile(filepath.Join(legacy, "current"), []byte("1.4.0"), 0o644); err != nil {
		t.Fatal(err)
	}
	dirs = vitePlusDataDirs()
	if !containsString(dirs, legacy) {
		t.Errorf("dirs = %v, want it to contain %q", dirs, legacy)
	}
}

// --- GlobalNpmPrefix -----------------------------------------------------

func TestVitePlus_GlobalNpmPrefix(t *testing.T) {
	clearVitePlusEnv(t)
	root := t.TempDir()
	t.Setenv("VP_HOME", root)

	ver := semver.MustParse("22.11.0")
	prefix := filepath.Join(root, "js_runtime", "node", "22.11.0", "lib", "node_modules")
	if err := os.MkdirAll(prefix, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := NewVitePlus().GlobalNpmPrefix(*ver)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != prefix {
		t.Errorf("got %q, want %q", got, prefix)
	}
}

func TestVitePlus_GlobalNpmPrefix_NotFound(t *testing.T) {
	clearVitePlusEnv(t)
	t.Setenv("VP_HOME", t.TempDir())

	ver := semver.MustParse("22.11.0")
	if _, err := NewVitePlus().GlobalNpmPrefix(*ver); err == nil {
		t.Fatal("expected error when the prefix doesn't exist, got nil")
	}
}

// --- Registry ------------------------------------------------------------

func TestVitePlus_RegisteredInRegistry(t *testing.T) {
	if got := NewVitePlus().Name(); got != "vite-plus" {
		t.Errorf("Name() = %q, want %q", got, "vite-plus")
	}
	if !IsAllowedManagerName("vite-plus") {
		t.Error("IsAllowedManagerName(\"vite-plus\") = false, want true")
	}
	found := false
	for _, m := range All() {
		if m.Name() == "vite-plus" {
			found = true
			break
		}
	}
	if !found {
		t.Error("All() does not include the vite-plus manager")
	}
}

// --- helpers -------------------------------------------------------------

// homeDirForTest returns the effective home directory for the current
// platform, mirroring the fallback vitePlusDataDirs uses.
func homeDirForTest(t *testing.T) string {
	t.Helper()
	h, err := userHomeDir()
	if err != nil || h == "" {
		t.Skipf("no home directory available: %v", err)
	}
	return h
}

// containsString reports whether haystack contains needle.
func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
