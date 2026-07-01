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

// --- parseNodenvVersion -------------------------------------------------

func TestParseNodenvVersion_StandardOutput(t *testing.T) {
	// Observed on nodenv 1.6.2 (packaged install):
	//   $ nodenv --version
	//   nodenv 1.6.2
	got, err := parseNodenvVersion("nodenv 1.6.2\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "1.6.2" {
		t.Errorf("got %q, want %q", got, "1.6.2")
	}
}

func TestParseNodenvVersion_GitRevisionSuffix(t *testing.T) {
	// Observed on a git-checkout install:
	//   $ nodenv --version
	//   nodenv 1.6.2-12-gabc1234
	//
	// Upstream's sed rewrites "-N-gSHA" to "+N.SHA" before
	// printing, so the actual stdout is "nodenv 1.6.2+12.abc1234".
	// Either form should parse — we just take the first token.
	got, err := parseNodenvVersion("nodenv 1.6.2-12-gabc1234\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "1.6.2-12-gabc1234" {
		t.Errorf("got %q, want %q", got, "1.6.2-12-gabc1234")
	}
}

func TestParseNodenvVersion_VPrefixed(t *testing.T) {
	// Defensive: some forks emit "nodenv v1.6.2".
	got, err := parseNodenvVersion("nodenv v1.6.2\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "1.6.2" {
		t.Errorf("got %q, want %q", got, "1.6.2")
	}
}

func TestParseNodenvVersion_TrailingWhitespace(t *testing.T) {
	// Defensive: handle trailing whitespace.
	got, err := parseNodenvVersion("   nodenv 1.6.2   \n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "1.6.2" {
		t.Errorf("got %q, want %q", got, "1.6.2")
	}
}

func TestParseNodenvVersion_Empty(t *testing.T) {
	_, err := parseNodenvVersion("")
	if err == nil {
		t.Error("expected error for empty input")
	}
}

func TestParseNodenvVersion_WhitespaceOnly(t *testing.T) {
	_, err := parseNodenvVersion("\n   \n")
	if err == nil {
		t.Error("expected error for whitespace-only input")
	}
}

func TestParseNodenvVersion_NoVersionAfterPrefix(t *testing.T) {
	// "nodenv " with nothing after it should error rather than
	// silently returning "nodenv". Upstream never emits this
	// shape (it's always "nodenv <version>"), but a malformed
	// wrapper or fork could. TrimSpace strips the trailing
	// space, leaving the bare word "nodenv" with no version
	// token — we must not return that as a version.
	_, err := parseNodenvVersion("nodenv \n")
	if err == nil {
		t.Error("expected error for empty version after 'nodenv' prefix")
	}
}

// --- parseNodenvInstalled -----------------------------------------------

func TestParseNodenvInstalled_RealOutput(t *testing.T) {
	// Real observed output of `nodenv versions`:
	//   $ nodenv versions
	//     *18.20.4 (set by /home/user/.nodenv/version)
	//      20.11.1
	//      22.5.0
	//
	// Note: `nodenv versions` (NOT `nodenv version`) emits the
	// marker+version. The marker is " * " for the current version
	// and "   " (two spaces, no star) for the others. We strip
	// the marker and keep just the version token.
	stdout := "  *18.20.4 (set by /home/user/.nodenv/version)\n  20.11.1\n  22.5.0\n"
	got, err := parseNodenvInstalled(stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"18.20.4", "20.11.1", "22.5.0"}
	if len(got) != len(want) {
		t.Fatalf("got %d versions, want %d (%v)", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].String() != w {
			t.Errorf("got[%d] = %s, want %s", i, got[i], w)
		}
	}
}

func TestParseNodenvInstalled_FiltersSystemSentinel(t *testing.T) {
	// When `nodenv-which node` succeeds (system node on PATH),
	// upstream emits a "system" line before the managed
	// versions. We must filter it out — it isn't a managed Node
	// version and isn't semver-parseable.
	stdout := "  system\n  *18.20.4 (set by /home/user/.nodenv/version)\n  20.11.1\n"
	got, err := parseNodenvInstalled(stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"18.20.4", "20.11.1"}
	if len(got) != len(want) {
		t.Fatalf("got %d versions, want %d (%v)", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].String() != w {
			t.Errorf("got[%d] = %s, want %s", i, got[i], w)
		}
	}
}

func TestParseNodenvInstalled_UnsortedInput(t *testing.T) {
	// Nodenv may emit versions in install order. We must sort
	// them ascending by semver before returning.
	stdout := "  22.5.0\n  *18.20.4 (set by /home/user/.nodenv/version)\n  20.11.1\n"
	got, err := parseNodenvInstalled(stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"18.20.4", "20.11.1", "22.5.0"}
	if len(got) != len(want) {
		t.Fatalf("got %d versions, want %d (%v)", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].String() != w {
			t.Errorf("got[%d] = %s, want %s", i, got[i], w)
		}
	}
}

func TestParseNodenvInstalled_EmptyStdout(t *testing.T) {
	// No installed versions. Should return an empty (non-nil)
	// slice, not nil and not an error.
	got, err := parseNodenvInstalled("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Error("got nil slice, want empty non-nil slice")
	}
	if len(got) != 0 {
		t.Errorf("got %d versions, want 0", len(got))
	}
}

func TestParseNodenvInstalled_OnlyBlankLines(t *testing.T) {
	got, err := parseNodenvInstalled("\n\n\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Error("got nil slice, want empty non-nil slice")
	}
	if len(got) != 0 {
		t.Errorf("got %d versions, want 0", len(got))
	}
}

func TestParseNodenvInstalled_SkipsUnparseableLines(t *testing.T) {
	// Lines that look like version lines but don't parse as
	// semver should be skipped silently rather than aborting
	// the whole list. Forward-compat for future metadata.
	stdout := "  garbage-no-version\n  20.11.1\n  some-other-text\n"
	got, err := parseNodenvInstalled(stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].String() != "20.11.1" {
		t.Errorf("got %v, want [20.11.1]", got)
	}
}

// --- Method tests ------------------------------------------------------

func TestNodenv_Name(t *testing.T) {
	if got := NewNodenv().Name(); got != "nodenv" {
		t.Errorf("Name() = %q, want %q", got, "nodenv")
	}
}

func TestNodenv_Version_Success(t *testing.T) {
	// Capture the exact command and args passed to runShell.
	var gotName string
	var gotArgs []string
	withStubShell(t,
		func(name string, a []string) {
			gotName = name
			gotArgs = a
		},
		func(req string) (*platform.RunResult, error) {
			return &platform.RunResult{Stdout: "nodenv 1.6.2\n"}, nil
		},
	)

	got, err := NewNodenv().Version()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "1.6.2" {
		t.Errorf("Version() = %q, want %q", got, "1.6.2")
	}
	if gotName != "nodenv" {
		t.Errorf("runShell name = %q, want %q", gotName, "nodenv")
	}
	if len(gotArgs) != 1 || gotArgs[0] != "--version" {
		t.Errorf("runShell args = %v, want [--version]", gotArgs)
	}
}

func TestNodenv_Version_VPrefixed(t *testing.T) {
	// Defensive: forks that emit "nodenv v1.6.2" should still
	// yield "1.6.2".
	withStubShell(t, nil,
		func(req string) (*platform.RunResult, error) {
			return &platform.RunResult{Stdout: "nodenv v1.6.2\n"}, nil
		},
	)
	got, err := NewNodenv().Version()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "1.6.2" {
		t.Errorf("Version() = %q, want %q", got, "1.6.2")
	}
}

func TestNodenv_Version_RunShellError(t *testing.T) {
	// runShell error must propagate as a wrapped error so callers
	// can distinguish "binary missing" from "binary present but
	// version unparseable".
	wantErr := errSentinelForTest
	withStubShell(t, nil,
		func(req string) (*platform.RunResult, error) {
			return nil, wantErr
		},
	)

	_, err := NewNodenv().Version()
	if err == nil {
		t.Fatal("expected error from runShell failure, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("error %v should wrap %v", err, wantErr)
	}
}

func TestNodenv_Version_ParsingError(t *testing.T) {
	// runShell succeeds but the output is unparseable. The
	// returned error must NOT wrap errSentinelForTest (it's a
	// parser-level error, not a shell error).
	withStubShell(t, nil,
		func(req string) (*platform.RunResult, error) {
			return &platform.RunResult{Stdout: "nodenv \n"}, nil
		},
	)
	_, err := NewNodenv().Version()
	if err == nil {
		t.Fatal("expected parsing error, got nil")
	}
}

func TestNodenv_ListInstalled_Success(t *testing.T) {
	// Capture the exact command and args passed to runShell.
	var gotName string
	var gotArgs []string
	withStubShell(t,
		func(name string, a []string) {
			gotName = name
			gotArgs = a
		},
		func(req string) (*platform.RunResult, error) {
			return &platform.RunResult{Stdout: "  *18.20.4 (set by /home/user/.nodenv/version)\n  20.11.1\n  22.5.0\n"}, nil
		},
	)

	got, err := NewNodenv().ListInstalled()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"18.20.4", "20.11.1", "22.5.0"}
	if len(got) != len(want) {
		t.Fatalf("got %d versions, want %d (%v)", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].String() != w {
			t.Errorf("got[%d] = %s, want %s", i, got[i], w)
		}
	}
	if gotName != "nodenv" {
		t.Errorf("runShell name = %q, want %q", gotName, "nodenv")
	}
	if len(gotArgs) != 1 || gotArgs[0] != "versions" {
		t.Errorf("runShell args = %v, want [versions]", gotArgs)
	}
}

func TestNodenv_ListInstalled_EmptyStdout(t *testing.T) {
	// `nodenv versions` exited 0 with no stdout — only happens
	// if the upstream `versions` helper printed nothing (e.g.,
	// the "Warning: no Node detected" went to stderr and the
	// shell capture dropped it). We treat empty stdout as "no
	// versions, no error".
	withStubShell(t, nil,
		func(req string) (*platform.RunResult, error) {
			return &platform.RunResult{Stdout: ""}, nil
		},
	)
	got, err := NewNodenv().ListInstalled()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Error("got nil slice, want empty non-nil slice")
	}
	if len(got) != 0 {
		t.Errorf("got %d versions, want 0", len(got))
	}
}

func TestNodenv_ListInstalled_RunShellError(t *testing.T) {
	// runShell failure (binary missing, permission denied, etc.)
	// must propagate. This is the typical failure mode when
	// `nodenv` is on PATH but no install is configured yet —
	// upstream exits 1 with stderr "Warning: no Node detected
	// on the system".
	wantErr := errSentinelForTest
	withStubShell(t, nil,
		func(req string) (*platform.RunResult, error) {
			return nil, wantErr
		},
	)

	_, err := NewNodenv().ListInstalled()
	if err == nil {
		t.Fatal("expected error from runShell failure, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("error %v should wrap %v", err, wantErr)
	}
}

// --- Detect tests -------------------------------------------------------

func TestNodenv_Detect_NoBinaryNoEnvNoDir(t *testing.T) {
	// All three branches false: no binary on PATH, no
	// $NODENV_ROOT, no ~/.nodenv on disk. Detect() must return
	// false without spawning runShell.
	t.Setenv("NODENV_ROOT", "")
	withStubHomeDir(t, "", errSentinelForTest)

	// Force runShell to fail loudly if Detect() touches it.
	orig := runShell
	runShell = func(ctx context.Context, name string, a ...string) (*platform.RunResult, error) {
		t.Fatalf("Detect() must not invoke runShell (was called with %s %v)", name, a)
		return nil, nil
	}
	t.Cleanup(func() { runShell = orig })

	if NewNodenv().Detect() {
		t.Error("Detect() = true with no PATH, no $NODENV_ROOT, and no ~/.nodenv; want false")
	}
}

func TestNodenv_Detect_FindsDirOnDisk(t *testing.T) {
	// Create a real ~/.nodenv directory. We use the homeDir
	// seam (rather than t.Setenv("HOME", ...)) for Windows
	// portability — os.UserHomeDir reads %USERPROFILE% on
	// Windows and ignores $HOME, so the env-var approach
	// wouldn't redirect on Windows.
	tmp := t.TempDir()
	withStubHomeDir(t, tmp, nil)
	t.Setenv("NODENV_ROOT", "")

	// Create ~/.nodenv.
	if err := os.MkdirAll(filepath.Join(tmp, ".nodenv"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Force runShell to fail loudly if Detect() touches it.
	orig := runShell
	runShell = func(ctx context.Context, name string, a ...string) (*platform.RunResult, error) {
		t.Fatalf("Detect() must not invoke runShell (was called with %s %v)", name, a)
		return nil, nil
	}
	t.Cleanup(func() { runShell = orig })

	if !NewNodenv().Detect() {
		t.Error("Detect() = false with ~/.nodenv present, want true")
	}
}

func TestNodenv_Detect_HonorsNODENV_ROOT(t *testing.T) {
	// $NODENV_ROOT set to a real directory should make Detect()
	// return true even when ~/.nodenv is missing and PATH has no
	// nodenv binary. This is the override path for users with a
	// custom install root.
	tmp := t.TempDir()
	t.Setenv("NODENV_ROOT", tmp)
	// homeDir returns a path with no .nodenv subdir — the
	// override must take precedence over the default lookup.
	withStubHomeDir(t, "/nonexistent/home", nil)

	// Force runShell to fail loudly if Detect() touches it.
	orig := runShell
	runShell = func(ctx context.Context, name string, a ...string) (*platform.RunResult, error) {
		t.Fatalf("Detect() must not invoke runShell (was called with %s %v)", name, a)
		return nil, nil
	}
	t.Cleanup(func() { runShell = orig })

	if !NewNodenv().Detect() {
		t.Error("Detect() = false with $NODENV_ROOT set, want true")
	}
}

func TestNodenv_Detect_EmptyNODENV_ROOT_FallsThrough(t *testing.T) {
	// $NODENV_ROOT explicitly empty string must be treated as
	// "not set" — fall through to the home-dir lookup.
	home := t.TempDir()
	withStubHomeDir(t, home, nil)
	t.Setenv("NODENV_ROOT", "")
	// No ~/.nodenv under the home dir.

	// Force runShell to fail loudly if Detect() touches it.
	orig := runShell
	runShell = func(ctx context.Context, name string, a ...string) (*platform.RunResult, error) {
		t.Fatalf("Detect() must not invoke runShell (was called with %s %v)", name, a)
		return nil, nil
	}
	t.Cleanup(func() { runShell = orig })

	if NewNodenv().Detect() {
		t.Error("Detect() = true with empty $NODENV_ROOT and no ~/.nodenv, want false")
	}
}

func TestNodenv_Detect_FindsBinaryOnPath(t *testing.T) {
	// nodenv on PATH (as a stub binary). Detect() must return
	// true. The on-disk branch is separate from this one — we
	// verify that the PATH hit alone is sufficient.
	//
	// On Windows, exec.LookPath requires a ".exe" suffix to
	// find an executable; the unadorned "nodenv" works on
	// unix. Use the platform-correct filename so the test runs
	// identically on linux, macOS, and Windows.
	binDir := t.TempDir()
	binName := "nodenv"
	if runtime.GOOS == "windows" {
		binName = "nodenv.exe"
	}
	bin := filepath.Join(binDir, binName)
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Set PATH to ONLY our bin dir. exec.LookPath walks PATH
	// itself, so a single-entry PATH is sufficient and removes
	// any chance of a stray nodenv binary elsewhere on the
	// runner shadowing our stub.
	t.Setenv("PATH", binDir)

	// Detect() must not invoke runShell — the on-disk check is
	// sufficient. Force a failure if it does.
	orig := runShell
	called := false
	runShell = func(ctx context.Context, name string, a ...string) (*platform.RunResult, error) {
		called = true
		return nil, nil
	}
	t.Cleanup(func() { runShell = orig })

	if !NewNodenv().Detect() {
		t.Error("Detect() = false with nodenv on PATH, want true")
	}
	if called {
		t.Error("Detect() invoked runShell — must be a pure-PATH/on-disk check")
	}
}

// --- Mutation methods -------------------------------------------------

func TestNodenv_MutationMethodsInvokeShell(t *testing.T) {
	// Nodenv mutation commands all take a bare `<v>` (no plugin
	// prefix — Nodenv is Node-only, unlike asdf).
	nd := NewNodenv()
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
		{"Install", func() error { return nd.Install(*ver) }, "install 22.5.0"},
		{"Uninstall", func() error { return nd.Uninstall(*ver) }, "uninstall 22.5.0"},
		{"Use", func() error { return nd.Use(*ver) }, "shell 22.5.0"},
		{"SetDefault", func() error { return nd.SetDefault(*ver) }, "global 22.5.0"},
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
			want := "nodenv " + c.wantArg
			if captured != want {
				t.Errorf("%s invoked %q, want %q", c.name, captured, want)
			}
		})
	}
}

func TestNodenv_Uninstall_PropagatesError(t *testing.T) {
	wantErr := errors.New("simulated nodenv failure")
	withStubShell(t, nil, func(req string) (*platform.RunResult, error) {
		return nil, wantErr
	})

	v := semver.MustParse("20.0.0")
	err := NewNodenv().Uninstall(*v)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("error %v should wrap %v", err, wantErr)
	}
}

// --- parseNodenvCurrent -------------------------------------------------

func TestParseNodenvCurrent_StandardOutput(t *testing.T) {
	// Real observed output of `nodenv version`:
	stdout := "22.11.0 (set by /home/user/.nodenv/version)\n"
	v, err := parseNodenvCurrent(stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.String() != "22.11.0" {
		t.Errorf("got %q, want %q", v.String(), "22.11.0")
	}
}

func TestParseNodenvCurrent_SystemIsError(t *testing.T) {
	_, err := parseNodenvCurrent("system\n")
	if err == nil {
		t.Error("expected error for 'system' (not a managed version)")
	}
}

func TestParseNodenvCurrent_Empty(t *testing.T) {
	_, err := parseNodenvCurrent("")
	if err == nil {
		t.Error("expected error on empty input")
	}
}

func TestNodenvCurrent_InvokesShell(t *testing.T) {
	var captured string
	withStubShell(t,
		nil,
		func(req string) (*platform.RunResult, error) {
			captured = req
			if req != "nodenv version" {
				t.Errorf("unexpected runShell call: %q", req)
			}
			return &platform.RunResult{Stdout: "22.11.0 (set by /home/user/.nodenv/version)\n"}, nil
		},
	)

	got, err := NewNodenv().Current()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.String() != "22.11.0" {
		t.Errorf("got %q, want %q", got.String(), "22.11.0")
	}
	if captured == "" {
		t.Error("expected nodenv version to be invoked")
	}
}

// --- nodenvRoot helper --------------------------------------------------

func TestNodenv_NodenvRootHelper_PrefersEnv(t *testing.T) {
	// $NODENV_ROOT takes precedence over $HOME/.nodenv.
	t.Setenv("NODENV_ROOT", "/custom/nodenv")
	got := nodenvRoot()
	if got != "/custom/nodenv" {
		t.Errorf("nodenvRoot() = %q, want %q", got, "/custom/nodenv")
	}
}

func TestNodenv_NodenvRootHelper_FallsBackToHome(t *testing.T) {
	// $NODENV_ROOT unset → use $HOME/.nodenv.
	t.Setenv("NODENV_ROOT", "")
	withStubHomeDir(t, "/home/user", nil)
	got := nodenvRoot()
	want := filepath.Join("/home/user", ".nodenv")
	if got != want {
		t.Errorf("nodenvRoot() = %q, want %q", got, want)
	}
}

func TestNodenv_NodenvRootHelper_ReturnsEmptyWhenNoHome(t *testing.T) {
	// $NODENV_ROOT unset and homeDir() errored → "".
	t.Setenv("NODENV_ROOT", "")
	withStubHomeDir(t, "", errSentinelForTest)
	got := nodenvRoot()
	if got != "" {
		t.Errorf("nodenvRoot() = %q, want empty string", got)
	}
}

func TestNodenv_NodenvRootHelper_TrimsWhitespace(t *testing.T) {
	// $NODENV_ROOT set to whitespace-only should be treated as
	// "not set" so we fall through to the home-dir lookup.
	t.Setenv("NODENV_ROOT", "   ")
	withStubHomeDir(t, "/home/user", nil)
	got := nodenvRoot()
	want := filepath.Join("/home/user", ".nodenv")
	if got != want {
		t.Errorf("nodenvRoot() = %q, want %q (whitespace env should not win)", got, want)
	}
}
