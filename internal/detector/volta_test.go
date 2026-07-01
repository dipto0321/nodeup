package detector

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Masterminds/semver/v3"

	"github.com/dipto0321/nodeup/internal/platform"
)

// --- parseVoltaVersion --------------------------------------------------

func TestParseVoltaVersion_StandardOutput(t *testing.T) {
	// Observed on volta 2.0.2:
	//   $ volta --version
	//   volta 2.0.2
	got, err := parseVoltaVersion("volta 2.0.2\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "2.0.2" {
		t.Errorf("got %q, want %q", got, "2.0.2")
	}
}

func TestParseVoltaVersion_BareVersion(t *testing.T) {
	// Some Volta builds (or patched forks) emit just the version.
	got, err := parseVoltaVersion("2.0.2\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "2.0.2" {
		t.Errorf("got %q, want %q", got, "2.0.2")
	}
}

func TestParseVoltaVersion_TrailingWhitespace(t *testing.T) {
	got, err := parseVoltaVersion("   volta 1.1.1   \n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "1.1.1" {
		t.Errorf("got %q, want %q", got, "1.1.1")
	}
}

func TestParseVoltaVersion_Empty(t *testing.T) {
	_, err := parseVoltaVersion("")
	if err == nil {
		t.Error("expected error for empty input")
	}
}

func TestParseVoltaVersion_WhitespaceOnly(t *testing.T) {
	// Whitespace-only output: TrimSpace produces "", we must error.
	_, err := parseVoltaVersion("\n   \n")
	if err == nil {
		t.Error("expected error for whitespace-only input")
	}
}

// --- parseVoltaInstalledEntries -----------------------------------------

func TestParseVoltaInstalledEntries_HappyPath(t *testing.T) {
	// Volta stores installs as dirs under
	// <voltaHome>/tools/image/node/. We don't care about the
	// "inventory" entries for Node (they reference the resolved
	// version, not individual installs).
	entries := []os.DirEntry{
		fakeEntry{name: "v20.10.0", isDir: true},
		fakeEntry{name: "v22.5.0", isDir: true},
		fakeEntry{name: "v18.19.0", isDir: true},
	}
	got, err := parseVoltaInstalledEntries(entries)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"18.19.0", "20.10.0", "22.5.0"} // sorted asc
	if len(got) != len(want) {
		t.Fatalf("got %d versions, want %d (%v)", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].String() != w {
			t.Errorf("got[%d] = %s, want %s", i, got[i], w)
		}
	}
}

func TestParseVoltaInstalledEntries_NoVPrefix(t *testing.T) {
	// Defensive: some Volta forks or migrated layouts might drop
	// the "v" prefix on directory names. semver.NewVersion handles
	// both forms.
	entries := []os.DirEntry{
		fakeEntry{name: "20.10.0", isDir: true},
		fakeEntry{name: "v22.5.0", isDir: true},
	}
	got, err := parseVoltaInstalledEntries(entries)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"20.10.0", "22.5.0"}
	if len(got) != len(want) {
		t.Fatalf("got %d versions, want %d (%v)", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].String() != w {
			t.Errorf("got[%d] = %s, want %s", i, got[i], w)
		}
	}
}

func TestParseVoltaInstalledEntries_SkipsNonDirs(t *testing.T) {
	// Volta shouldn't emit non-dirs in this directory, but if it
	// ever does (e.g. a stray README, a future inventory pointer),
	// we must skip them silently.
	entries := []os.DirEntry{
		fakeEntry{name: "v20.10.0", isDir: true},
		fakeEntry{name: "README.md", isDir: false},
		fakeEntry{name: "v22.5.0", isDir: true},
	}
	got, err := parseVoltaInstalledEntries(entries)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"20.10.0", "22.5.0"}
	if len(got) != len(want) {
		t.Fatalf("got %d versions, want %d (%v)", len(got), len(want), got)
	}
}

func TestParseVoltaInstalledEntries_SkipsUnparseable(t *testing.T) {
	// Stray directories we don't understand must not abort the list.
	entries := []os.DirEntry{
		fakeEntry{name: "v20.10.0", isDir: true},
		fakeEntry{name: "not-a-version", isDir: true},
		fakeEntry{name: "lts-hydrogen", isDir: true}, // plausible Volta alias dir
		fakeEntry{name: "v22.5.0", isDir: true},
	}
	got, err := parseVoltaInstalledEntries(entries)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"20.10.0", "22.5.0"}
	if len(got) != len(want) {
		t.Fatalf("got %d versions, want %d (%v)", len(got), len(want), got)
	}
}

func TestParseVoltaInstalledEntries_Empty(t *testing.T) {
	got, err := parseVoltaInstalledEntries(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Error("expected non-nil slice, got nil")
	}
	if len(got) != 0 {
		t.Errorf("expected empty slice, got %v", got)
	}
}

// --- Volta method tests -------------------------------------------------

func TestVolta_Name(t *testing.T) {
	if got := NewVolta().Name(); got != "volta" {
		t.Errorf("Name() = %q, want %q", got, "volta")
	}
}

func TestVolta_Version_Success(t *testing.T) {
	var captured []string
	withStubShell(t,
		func(name string, a []string) { captured = append(captured, name); captured = append(captured, a...) },
		func(req string) (*platform.RunResult, error) {
			if req != "volta --version" {
				t.Errorf("unexpected runShell call: %q", req)
			}
			return &platform.RunResult{Stdout: "volta 2.0.2\n"}, nil
		},
	)

	got, err := NewVolta().Version()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "2.0.2" {
		t.Errorf("got %q, want %q", got, "2.0.2")
	}
	if len(captured) < 2 || captured[0] != "volta" || captured[1] != "--version" {
		t.Errorf("expected `volta --version` invocation, got %v", captured)
	}
}

func TestVolta_Version_BareVersion(t *testing.T) {
	// Defensive parser coverage: bare version output (no "volta " prefix).
	withStubShell(t, nil, func(req string) (*platform.RunResult, error) {
		return &platform.RunResult{Stdout: "2.0.2\n"}, nil
	})

	got, err := NewVolta().Version()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "2.0.2" {
		t.Errorf("got %q, want %q", got, "2.0.2")
	}
}

func TestVolta_Version_RunShellError(t *testing.T) {
	wantErr := errors.New("simulated subprocess failure")
	withStubShell(t, nil, func(req string) (*platform.RunResult, error) {
		return nil, wantErr
	})

	_, err := NewVolta().Version()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("error %v should wrap %v", err, wantErr)
	}
}

func TestVolta_Version_ParsingError(t *testing.T) {
	// runShell succeeded but the body is unparseable.
	withStubShell(t, nil, func(req string) (*platform.RunResult, error) {
		return &platform.RunResult{Stdout: ""}, nil
	})

	_, err := NewVolta().Version()
	if err == nil {
		t.Error("expected parsing error from blank output, got nil")
	}
}

func TestVolta_Detect_NeitherPathNorHome(t *testing.T) {
	// Both branches false: stub LookupManagerBinary to return empty
	// (via the package var used by Detect), unset VOLTA_HOME and stub
	// homeDir to return "" so voltaHome() returns "". Detect() must
	// return false without spawning runShell.
	t.Setenv("VOLTA_HOME", "")
	withStubHomeDir(t, "", errSentinelForTest)

	// Force runShell to fail loudly if Detect() touches it.
	orig := runShell
	runShell = func(ctx context.Context, name string, a ...string) (*platform.RunResult, error) {
		t.Fatalf("Detect() must not invoke runShell (was called with %s %v)", name, a)
		return nil, nil
	}
	t.Cleanup(func() { runShell = orig })

	if NewVolta().Detect() {
		t.Error("Detect() = true with no PATH and no $VOLTA_HOME, want false")
	}
}

func TestVolta_Detect_FindsBinaryOnDisk(t *testing.T) {
	// Stub LookupManagerBinary to return "" (PATH miss), but place a
	// real file at <homeDir-returned-path>/.volta/bin/volta. We use
	// the homeDir seam rather than t.Setenv("HOME", ...) because on
	// Windows os.UserHomeDir reads %USERPROFILE% and ignores $HOME,
	// so the env-var approach alone wouldn't redirect on Windows.
	tmp := t.TempDir()
	withStubHomeDir(t, tmp, nil)

	t.Setenv("VOLTA_HOME", "")

	// Force runShell to fail loudly if Detect() touches it.
	orig := runShell
	runShell = func(ctx context.Context, name string, a ...string) (*platform.RunResult, error) {
		t.Fatalf("Detect() must not invoke runShell (was called with %s %v)", name, a)
		return nil, nil
	}
	t.Cleanup(func() { runShell = orig })

	voltaRoot := filepath.Join(tmp, ".volta")
	if err := os.MkdirAll(filepath.Join(voltaRoot, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(voltaRoot, "bin", "volta"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	if !NewVolta().Detect() {
		t.Error("Detect() = false with <HOME>/.volta/bin/volta present, want true")
	}
}

func TestVolta_Detect_HonorsVOLTA_HOME(t *testing.T) {
	// VOLTA_HOME overrides the default. Place a file there.
	tmp := t.TempDir()
	t.Setenv("VOLTA_HOME", tmp)

	orig := runShell
	runShell = func(ctx context.Context, name string, a ...string) (*platform.RunResult, error) {
		t.Fatalf("Detect() must not invoke runShell (was called with %s %v)", name, a)
		return nil, nil
	}
	t.Cleanup(func() { runShell = orig })

	if err := os.MkdirAll(filepath.Join(tmp, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "bin", "volta"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	if !NewVolta().Detect() {
		t.Error("Detect() = false with $VOLTA_HOME/bin/volta present, want true")
	}
}

func TestVolta_ListInstalled_Success(t *testing.T) {
	// Stub listDir to return canned Volta-style entries. We don't
	// need a real $VOLTA_HOME because we override voltaHome() resolution
	// via t.Setenv so ListInstalled can compute the image path.
	home := t.TempDir()
	t.Setenv("VOLTA_HOME", home)

	// Image dir is <VOLTA_HOME>/tools/image/node — it doesn't need
	// to exist; listDir is stubbed to ignore the path argument.
	entries := []os.DirEntry{
		fakeEntry{name: "v22.5.0", isDir: true},
		fakeEntry{name: "v20.10.0", isDir: true},
	}
	withStubListDir(t, entries, nil)

	got, err := NewVolta().ListInstalled()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"20.10.0", "22.5.0"}
	if len(got) != len(want) {
		t.Fatalf("got %d versions, want %d (%v)", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].String() != w {
			t.Errorf("got[%d] = %s, want %s", i, got[i], w)
		}
	}
}

func TestVolta_ListInstalled_NoImageDir(t *testing.T) {
	// Volta is installed but has never installed a Node version —
	// tools/image/node doesn't exist. Must return an empty (non-nil)
	// slice, not an error.
	home := t.TempDir()
	t.Setenv("VOLTA_HOME", home)

	// Stub listDir to simulate ENOENT (real path doesn't exist).
	withStubListDir(t, nil, os.ErrNotExist)

	got, err := NewVolta().ListInstalled()
	if err != nil {
		t.Fatalf("expected nil error for missing image dir, got %v", err)
	}
	if got == nil {
		t.Error("expected non-nil empty slice, got nil")
	}
	if len(got) != 0 {
		t.Errorf("expected empty slice, got %v", got)
	}
}

func TestVolta_ListInstalled_ReadsExpectedPath(t *testing.T) {
	// Verify ListInstalled constructs the canonical image path:
	// <VOLTA_HOME>/tools/image/node.
	home := t.TempDir()
	t.Setenv("VOLTA_HOME", home)

	var captured string
	orig := listDir
	listDir = func(p string) ([]os.DirEntry, error) {
		captured = p
		return []os.DirEntry{
			fakeEntry{name: "v20.10.0", isDir: true},
		}, nil
	}
	t.Cleanup(func() { listDir = orig })

	if _, err := NewVolta().ListInstalled(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(home, "tools", "image", "node")
	if captured != want {
		t.Errorf("listDir called with %q, want %q", captured, want)
	}
}

func TestVolta_ListInstalled_ListDirError(t *testing.T) {
	// Anything other than ENOENT must surface as a real error so the
	// user can debug permissions, corruption, etc.
	home := t.TempDir()
	t.Setenv("VOLTA_HOME", home)

	wantErr := errors.New("simulated readdir failure")
	withStubListDir(t, nil, wantErr)

	_, err := NewVolta().ListInstalled()
	if err == nil {
		t.Fatal("expected error from listDir failure, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("error %v should wrap %v", err, wantErr)
	}
}

func TestVolta_MutationMethodsInvokeShell(t *testing.T) {
	// Volta's mutation commands all take `node@<v>` rather than `<v>`
	// directly. We verify each call wraps the version with the tool
	// prefix so the CLI doesn't accidentally call `volta install 22.5.0`.
	v := NewVolta()
	ver, err := semver.NewVersion("22.5.0")
	if err != nil {
		t.Fatal(err)
	}

	type tc struct {
		name    string
		call    func() error
		wantArg string
	}
	cases := []tc{
		{"Install", func() error { return v.Install(*ver) }, "install node@22.5.0"},
		{"Uninstall", func() error { return v.Uninstall(*ver) }, "uninstall node@22.5.0"},
		{"Use", func() error { return v.Use(*ver) }, "use node@22.5.0"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var captured string
			withStubShell(t,
				nil,
				func(req string) (*platform.RunResult, error) {
					captured = req
					return &platform.RunResult{}, nil
				},
			)
			if err := c.call(); err != nil {
				t.Fatalf("%s: unexpected error: %v", c.name, err)
			}
			want := "volta " + c.wantArg
			if captured != want {
				t.Errorf("%s invoked %q, want %q", c.name, captured, want)
			}
		})
	}
}

func TestVolta_SetDefaultIsNoOp(t *testing.T) {
	// Volta pins versions per-project, not per-machine. SetDefault
	// must return nil without invoking the shell — callers can use
	// the same code path regardless of which manager is active.
	v := NewVolta()
	ver, err := semver.NewVersion("22.5.0")
	if err != nil {
		t.Fatal(err)
	}

	// Force a fail if runShell is invoked.
	orig := runShell
	runShell = func(ctx context.Context, name string, a ...string) (*platform.RunResult, error) {
		t.Fatalf("SetDefault must not invoke runShell for Volta (called %s %v)", name, a)
		return nil, nil
	}
	t.Cleanup(func() { runShell = orig })

	if err := v.SetDefault(*ver); err != nil {
		t.Errorf("SetDefault = %v, want nil", err)
	}
}

func TestVolta_Uninstall_PropagatesError(t *testing.T) {
	wantErr := errors.New("simulated volta failure")
	withStubShell(t, nil, func(req string) (*platform.RunResult, error) {
		return nil, wantErr
	})

	v := semver.MustParse("20.0.0")
	err := NewVolta().Uninstall(*v)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("error %v should wrap %v", err, wantErr)
	}
}

// --- parseVoltaCurrent --------------------------------------------------

func TestParseVoltaCurrent_ActiveNode(t *testing.T) {
	// Real observed output of `volta list --format=plain`:
	stdout := "node@v20.18.0 (active)\nnpm@10.5.0 (active)\npnpm@9.0.0 (default)\n"
	v, err := parseVoltaCurrent(stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.String() != "20.18.0" {
		t.Errorf("got %q, want %q", v.String(), "20.18.0")
	}
}

func TestParseVoltaCurrent_NoActiveNode(t *testing.T) {
	// Output contains no node@ row.
	_, err := parseVoltaCurrent("npm@10.5.0 (active)\n")
	if err == nil {
		t.Error("expected error when no active node row present")
	}
}

func TestParseVoltaCurrent_EmptyOutput(t *testing.T) {
	_, err := parseVoltaCurrent("")
	if err == nil {
		t.Error("expected error on empty input")
	}
}

func TestVoltaCurrent_InvokesShell(t *testing.T) {
	var captured string
	withStubShell(t,
		nil,
		func(req string) (*platform.RunResult, error) {
			captured = req
			if req != "volta list --format=plain" {
				t.Errorf("unexpected runShell call: %q", req)
			}
			return &platform.RunResult{Stdout: "node@v22.11.0 (active)\n"}, nil
		},
	)

	got, err := NewVolta().Current()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.String() != "22.11.0" {
		t.Errorf("got %q, want %q", got.String(), "22.11.0")
	}
	if captured == "" {
		t.Error("expected volta list to be invoked")
	}
}

func TestVolta_DetectDoesNotInvokeRunShell(t *testing.T) {
	// Belt-and-suspenders: even if both PATH lookup and on-disk
	// detection would normally return false, Detect() must never
	// spawn runShell. We force runShell to fail the test if called.
	orig := runShell
	runShell = func(ctx context.Context, name string, a ...string) (*platform.RunResult, error) {
		t.Fatalf("Detect() must not invoke runShell (was called with %s %v)", name, a)
		return nil, nil
	}
	t.Cleanup(func() { runShell = orig })

	_ = NewVolta().Detect()
}

// --- voltaHome / voltaBinaryPath ----------------------------------------

// withStubHomeDir swaps the package-level homeDir var for the
// duration of one test, returning a cleanup hook via t.Cleanup.
// The stub ignores the env/lookup context and returns the supplied
// (path, error). This is the home-resolution twin of withStubListDir
// (nvm_test.go) and withStubShell (fnm_test.go) — necessary because
// on Windows os.UserHomeDir reads %USERPROFILE% and ignores $HOME,
// so t.Setenv("HOME", ...) does not redirect there. Stubbing at the
// function-seam level is the only portable way to inject a temp
// home directory across all OSes.
func withStubHomeDir(t *testing.T, path string, err error) {
	t.Helper()
	orig := homeDir
	homeDir = func() (string, error) { return path, err }
	t.Cleanup(func() { homeDir = orig })
}

func TestVoltaHome_OverridesWithEnv(t *testing.T) {
	// Sanity check: $VOLTA_HOME takes precedence over the homeDir
	// seam. We stub homeDir so any accidental fall-through would
	// surface immediately instead of silently hitting the
	// developer's real $HOME.
	t.Setenv("VOLTA_HOME", "/custom/volta/root")
	withStubHomeDir(t, "/should/be/ignored", nil)

	got := voltaHome()
	if got != "/custom/volta/root" {
		t.Errorf("got %q, want %q", got, "/custom/volta/root")
	}
}

func TestVoltaHome_FallsBackToDotVolta(t *testing.T) {
	t.Setenv("VOLTA_HOME", "")
	home := t.TempDir()
	withStubHomeDir(t, home, nil)

	got := voltaHome()
	want := filepath.Join(home, ".volta")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestVoltaHome_TrimsWhitespace(t *testing.T) {
	// A path with leading/trailing whitespace is invalid; we treat
	// it as "not set" and fall through to the default. This matches
	// how nvmDir handles the same case.
	t.Setenv("VOLTA_HOME", "   ")
	trimmed := strings.TrimSpace(os.Getenv("VOLTA_HOME"))
	if trimmed != "" {
		t.Fatalf("sanity: env not whitespace-stripped, got %q", trimmed)
	}
	// Even with VOLTA_HOME="   ", fallback should kick in.
	home := t.TempDir()
	withStubHomeDir(t, home, nil)

	got := voltaHome()
	want := filepath.Join(home, ".volta")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestVoltaHome_EmptyWhenUserHomeFails(t *testing.T) {
	// homeDir erroring should make voltaHome() return "" — same
	// safety net as NVM (avoiding a panic or surprising
	// filepath.Join result on a stripped-down CI runner).
	t.Setenv("VOLTA_HOME", "")
	withStubHomeDir(t, "", errSentinelForTest)

	got := voltaHome()
	if got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

func TestVoltaBinaryPath_UsesVoltaHome(t *testing.T) {
	t.Setenv("VOLTA_HOME", "/custom/root")
	got := voltaBinaryPath()
	want := filepath.Join("/custom/root", "bin", "volta")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestVoltaBinaryPath_EmptyWhenHomeUnresolved(t *testing.T) {
	t.Setenv("VOLTA_HOME", "")
	withStubHomeDir(t, "", errSentinelForTest)

	got := voltaBinaryPath()
	if got != "" {
		t.Errorf("got %q, want empty string when neither VOLTA_HOME nor homeDir resolves", got)
	}
}
