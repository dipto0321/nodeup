//go:build windows

package detector

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Masterminds/semver/v3"

	"github.com/dipto0321/nodeup/internal/platform"
)

// --- parseNVMWindowsVersion ---------------------------------------------

func TestParseNVMWindowsVersion_StandardOutput(t *testing.T) {
	// Observed on nvm-windows 1.1.12:
	//   $ nvm version
	//   1.1.12
	//
	// The upstream dispatcher calls fmt.Println(NvmVersion), which
	// prints the bare semver followed by a single newline. There is
	// no "nvm " branding prefix (unlike fnm/nodenv/etc.) and no "v"
	// prefix.
	got, err := parseNVMWindowsVersion("1.1.12\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "1.1.12" {
		t.Errorf("got %q, want %q", got, "1.1.12")
	}
}

func TestParseNVMWindowsVersion_VPrefixed(t *testing.T) {
	// Defensive: a fork that re-adds the "v" prefix should still
	// yield the bare semver.
	got, err := parseNVMWindowsVersion("v1.1.12\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "1.1.12" {
		t.Errorf("got %q, want %q", got, "1.1.12")
	}
}

func TestParseNVMWindowsVersion_GitDescribeSuffix(t *testing.T) {
	// A git-checkout build of nvm-windows could emit a
	// git-describe-style version (e.g., "1.1.12-4-gabc1234"). We
	// don't validate semver here — the caller decides. The parser
	// just needs to return the first token.
	got, err := parseNVMWindowsVersion("1.1.12-4-gabc1234\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "1.1.12-4-gabc1234" {
		t.Errorf("got %q, want %q", got, "1.1.12-4-gabc1234")
	}
}

func TestParseNVMWindowsVersion_TrailingWhitespace(t *testing.T) {
	// Defensive: handle trailing whitespace and a CR (in case the
	// upstream switches to fmt.Print with "\r\n" later).
	got, err := parseNVMWindowsVersion("   1.1.12   \r\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "1.1.12" {
		t.Errorf("got %q, want %q", got, "1.1.12")
	}
}

func TestParseNVMWindowsVersion_Empty(t *testing.T) {
	_, err := parseNVMWindowsVersion("")
	if err == nil {
		t.Error("expected error for empty input")
	}
}

func TestParseNVMWindowsVersion_WhitespaceOnly(t *testing.T) {
	_, err := parseNVMWindowsVersion("\n   \n")
	if err == nil {
		t.Error("expected error for whitespace-only input")
	}
}

func TestParseNVMWindowsVersion_MultiToken(t *testing.T) {
	// Defensive: a fork that prints extra metadata after the
	// version (e.g., "1.1.12 (build 1234)") should still yield the
	// first token. Upstream never does this — NvmVersion is set via
	// ldflags to a single bare semver — but we accept the shape so
	// we don't break on a future fork.
	got, err := parseNVMWindowsVersion("1.1.12 (build 1234)\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "1.1.12" {
		t.Errorf("got %q, want %q", got, "1.1.12")
	}
}

// --- parseNVMWindowsInstalled -------------------------------------------

func TestParseNVMWindowsInstalled_RealOutput(t *testing.T) {
	// Real observed output of `nvm list` (nvm-windows 1.1.12):
	//   $ nvm list
	//
	//     * 18.20.4 (Currently using 64-bit executable)
	//       20.11.1
	//       22.5.0
	//
	// Note: `nvm list` (NOT `nvm ls`) emits the marker+version. The
	// marker is "  * " (two spaces + star) for the current version
	// and "    " (four spaces, no star) for the others. We strip
	// the marker and keep just the version token. The
	// " (Currently using <arch>-bit executable)" suffix on the
	// current-version line is dropped before semver parsing.
	stdout := "\n  * 18.20.4 (Currently using 64-bit executable)\n    20.11.1\n    22.5.0\n"
	got, err := parseNVMWindowsInstalled(stdout)
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

func TestParseNVMWindowsInstalled_ThirtyTwoBitMarker(t *testing.T) {
	// Real observed output on a 32-bit nvm-windows install:
	// the current-version suffix uses "32-bit" instead of
	// "64-bit". We must accept either.
	stdout := "\n  * 18.20.4 (Currently using 32-bit executable)\n    20.11.1\n"
	got, err := parseNVMWindowsInstalled(stdout)
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

func TestParseNVMWindowsInstalled_FiltersEmptySentinel(t *testing.T) {
	// When no versions are installed, upstream emits a single line
	// "No installations recognized." We must filter it out — it
	// isn't a version line.
	stdout := "No installations recognized.\n"
	got, err := parseNVMWindowsInstalled(stdout)
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

func TestParseNVMWindowsInstalled_EmptySentinelWithLeadingBlank(t *testing.T) {
	// The empty-install branch starts with a blank line (the
	// upstream code unconditionally prints \n before any version
	// iteration). Confirm we still filter correctly when the
	// sentinel is preceded by blank lines.
	stdout := "\n\nNo installations recognized.\n"
	got, err := parseNVMWindowsInstalled(stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d versions, want 0", len(got))
	}
}

func TestParseNVMWindowsInstalled_UnsortedInput(t *testing.T) {
	// nvm-windows may emit versions in install order. We must
	// sort them ascending by semver before returning.
	stdout := "\n    22.5.0\n  * 18.20.4 (Currently using 64-bit executable)\n    20.11.1\n"
	got, err := parseNVMWindowsInstalled(stdout)
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

func TestParseNVMWindowsInstalled_EmptyStdout(t *testing.T) {
	// No installed versions, no sentinel line — only happens if
	// the upstream `list` helper printed nothing. We treat empty
	// stdout as "no versions, no error".
	got, err := parseNVMWindowsInstalled("")
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

func TestParseNVMWindowsInstalled_OnlyBlankLines(t *testing.T) {
	got, err := parseNVMWindowsInstalled("\n\n\n")
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

func TestParseNVMWindowsInstalled_DefensiveVPrefix(t *testing.T) {
	// A fork that forgets the upstream regex-substitution could
	// emit "v18.20.4" — we must strip the leading "v" before
	// handing to semver.
	stdout := "\n  * v18.20.4 (Currently using 64-bit executable)\n    v20.11.1\n"
	got, err := parseNVMWindowsInstalled(stdout)
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

func TestParseNVMWindowsInstalled_SkipsUnparseableLines(t *testing.T) {
	// Lines that look like version lines but don't parse as
	// semver should be skipped silently rather than aborting the
	// whole list. Forward-compat for future metadata nvm-windows
	// might add.
	stdout := "    garbage-no-version\n    20.11.1\n    some-other-text\n"
	got, err := parseNVMWindowsInstalled(stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].String() != "20.11.1" {
		t.Errorf("got %v, want [20.11.1]", got)
	}
}

func TestParseNVMWindowsInstalled_CRLineEndings(t *testing.T) {
	// Defensive: some Windows console captures emit "\r\n".
	// strings.Split on "\n" leaves a trailing "\r" on each line
	// after TrimSpace runs — TrimSpace strips that, so parsing
	// must work identically.
	stdout := "\r\n  * 18.20.4 (Currently using 64-bit executable)\r\n    20.11.1\r\n"
	got, err := parseNVMWindowsInstalled(stdout)
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

// --- Method tests ------------------------------------------------------

func TestNVMWindows_Name(t *testing.T) {
	if got := NewNVMWindows().Name(); got != "nvm-windows" {
		t.Errorf("Name() = %q, want %q", got, "nvm-windows")
	}
}

func TestNVMWindows_Version_Success(t *testing.T) {
	// Capture the exact command and args passed to runShell.
	var gotName string
	var gotArgs []string
	withStubShell(t,
		func(name string, a []string) {
			gotName = name
			gotArgs = a
		},
		func(req string) (*platform.RunResult, error) {
			return &platform.RunResult{Stdout: "1.1.12\n"}, nil
		},
	)

	got, err := NewNVMWindows().Version()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "1.1.12" {
		t.Errorf("Version() = %q, want %q", got, "1.1.12")
	}
	if gotName != "nvm" {
		t.Errorf("runShell name = %q, want %q", gotName, "nvm")
	}
	if len(gotArgs) != 1 || gotArgs[0] != "version" {
		t.Errorf("runShell args = %v, want [version]", gotArgs)
	}
}

func TestNVMWindows_Version_VPrefixed(t *testing.T) {
	// Defensive: forks that emit "v1.1.12" should still yield
	// "1.1.12".
	withStubShell(t, nil,
		func(req string) (*platform.RunResult, error) {
			return &platform.RunResult{Stdout: "v1.1.12\n"}, nil
		},
	)
	got, err := NewNVMWindows().Version()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "1.1.12" {
		t.Errorf("Version() = %q, want %q", got, "1.1.12")
	}
}

func TestNVMWindows_Version_RunShellError(t *testing.T) {
	// runShell error must propagate as a wrapped error so callers
	// can distinguish "binary missing" from "binary present but
	// version unparseable".
	wantErr := errSentinelForTest
	withStubShell(t, nil,
		func(req string) (*platform.RunResult, error) {
			return nil, wantErr
		},
	)

	_, err := NewNVMWindows().Version()
	if err == nil {
		t.Fatal("expected error from runShell failure, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("error %v should wrap %v", err, wantErr)
	}
}

func TestNVMWindows_Version_EmptyOutput(t *testing.T) {
	// runShell succeeds but the output is empty (rare — usually
	// a corrupted install). The returned error must NOT wrap
	// errSentinelForTest (it's a parser-level error, not a shell
	// error).
	withStubShell(t, nil,
		func(req string) (*platform.RunResult, error) {
			return &platform.RunResult{Stdout: ""}, nil
		},
	)
	_, err := NewNVMWindows().Version()
	if err == nil {
		t.Fatal("expected parsing error, got nil")
	}
}

func TestNVMWindows_ListInstalled_Success(t *testing.T) {
	// Capture the exact command and args passed to runShell.
	var gotName string
	var gotArgs []string
	withStubShell(t,
		func(name string, a []string) {
			gotName = name
			gotArgs = a
		},
		func(req string) (*platform.RunResult, error) {
			return &platform.RunResult{Stdout: "\n  * 18.20.4 (Currently using 64-bit executable)\n    20.11.1\n    22.5.0\n"}, nil
		},
	)

	got, err := NewNVMWindows().ListInstalled(t.Context())
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
	if gotName != "nvm" {
		t.Errorf("runShell name = %q, want %q", gotName, "nvm")
	}
	if len(gotArgs) != 1 || gotArgs[0] != "list" {
		t.Errorf("runShell args = %v, want [list]", gotArgs)
	}
}

func TestNVMWindows_ListInstalled_EmptyStdout(t *testing.T) {
	// `nvm list` exited 0 with no stdout — only happens if the
	// upstream `list` helper printed nothing (very rare). We
	// treat empty stdout as "no versions, no error".
	withStubShell(t, nil,
		func(req string) (*platform.RunResult, error) {
			return &platform.RunResult{Stdout: ""}, nil
		},
	)
	got, err := NewNVMWindows().ListInstalled(t.Context())
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

func TestNVMWindows_ListInstalled_RunShellError(t *testing.T) {
	// runShell failure (binary missing, permission denied, etc.)
	// must propagate. This is the typical failure mode when nvm
	// is on PATH but a malformed install blocks the listing.
	wantErr := errSentinelForTest
	withStubShell(t, nil,
		func(req string) (*platform.RunResult, error) {
			return nil, wantErr
		},
	)

	_, err := NewNVMWindows().ListInstalled(t.Context())
	if err == nil {
		t.Fatal("expected error from runShell failure, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("error %v should wrap %v", err, wantErr)
	}
}

// --- Detect tests -------------------------------------------------------

func TestNVMWindows_Detect_NoBinaryNoEnv(t *testing.T) {
	// All three branches false: no binary on PATH, no
	// $NVM_HOME, no $NVM_SYMLINK. Detect() must return false
	// without spawning runShell.
	t.Setenv("NVM_HOME", "")
	t.Setenv("NVM_SYMLINK", "")
	// Force runShell to fail loudly if Detect() touches it.
	orig := runShell
	called := false
	runShell = func(ctx context.Context, name string, a ...string) (*platform.RunResult, error) {
		called = true
		return nil, nil
	}
	t.Cleanup(func() { runShell = orig })

	if NewNVMWindows().Detect() {
		t.Error("Detect() = true with no PATH, no $NVM_HOME, no $NVM_SYMLINK; want false")
	}
	if called {
		t.Error("Detect() invoked runShell — must be a pure-PATH/env check")
	}
}

func TestNVMWindows_Detect_FindsBinaryOnPath(t *testing.T) {
	// nvm.exe on PATH. Detect() must return true. The file is
	// build-tagged `//go:build windows`, so the binary name
	// always carries the .exe suffix — no `runtime.GOOS`
	// conditional needed.
	binDir := t.TempDir()
	bin := filepath.Join(binDir, "nvm.exe")
	if err := os.WriteFile(bin, []byte("@echo off\r\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Set PATH to ONLY our bin dir. exec.LookPath walks PATH
	// itself, so a single-entry PATH is sufficient and removes
	// any chance of a stray nvm binary elsewhere on the runner
	// shadowing our stub.
	t.Setenv("PATH", binDir)
	t.Setenv("NVM_HOME", "")
	t.Setenv("NVM_SYMLINK", "")

	// Detect() must not invoke runShell — the on-disk check is
	// sufficient. Force a failure if it does.
	orig := runShell
	called := false
	runShell = func(ctx context.Context, name string, a ...string) (*platform.RunResult, error) {
		called = true
		return nil, nil
	}
	t.Cleanup(func() { runShell = orig })

	if !NewNVMWindows().Detect() {
		t.Error("Detect() = false with nvm.exe on PATH, want true")
	}
	if called {
		t.Error("Detect() invoked runShell — must be a pure-PATH check")
	}
}

func TestNVMWindows_Detect_HonorsNVMHOME(t *testing.T) {
	// $NVM_HOME set should make Detect() return true even when
	// no nvm.exe is on PATH and $NVM_SYMLINK is unset. This is
	// the override path for users with a custom install root
	// whose installer didn't put nvm.exe on PATH (rare but
	// observed when the installer is invoked manually).
	t.Setenv("NVM_HOME", `C:\custom\nvm`)
	t.Setenv("NVM_SYMLINK", "")
	t.Setenv("PATH", t.TempDir()) // empty PATH

	// Force runShell to fail loudly if Detect() touches it.
	orig := runShell
	called := false
	runShell = func(ctx context.Context, name string, a ...string) (*platform.RunResult, error) {
		called = true
		return nil, nil
	}
	t.Cleanup(func() { runShell = orig })

	if !NewNVMWindows().Detect() {
		t.Error("Detect() = false with $NVM_HOME set, want true")
	}
	if called {
		t.Error("Detect() invoked runShell — must be a pure-env check")
	}
}

func TestNVMWindows_Detect_HonorsNVMSYMLINK(t *testing.T) {
	// $NVM_SYMLINK set (without $NVM_HOME and without PATH) is
	// a strong install signal — the upstream installer sets
	// both, but either alone is enough for us.
	t.Setenv("NVM_HOME", "")
	t.Setenv("NVM_SYMLINK", `C:\nvm4w\nodejs`)
	t.Setenv("PATH", t.TempDir()) // empty PATH

	// Force runShell to fail loudly if Detect() touches it.
	orig := runShell
	called := false
	runShell = func(ctx context.Context, name string, a ...string) (*platform.RunResult, error) {
		called = true
		return nil, nil
	}
	t.Cleanup(func() { runShell = orig })

	if !NewNVMWindows().Detect() {
		t.Error("Detect() = false with $NVM_SYMLINK set, want true")
	}
	if called {
		t.Error("Detect() invoked runShell — must be a pure-env check")
	}
}

func TestNVMWindows_Detect_EmptyNVMHOME_FallsThrough(t *testing.T) {
	// $NVM_HOME explicitly empty string must be treated as
	// "not set" — fall through to the symlink check, then PATH.
	t.Setenv("NVM_HOME", "")
	t.Setenv("NVM_SYMLINK", "")
	t.Setenv("PATH", t.TempDir()) // empty PATH

	// Force runShell to fail loudly if Detect() touches it.
	orig := runShell
	called := false
	runShell = func(ctx context.Context, name string, a ...string) (*platform.RunResult, error) {
		called = true
		return nil, nil
	}
	t.Cleanup(func() { runShell = orig })

	if NewNVMWindows().Detect() {
		t.Error("Detect() = true with all three branches empty, want false")
	}
	if called {
		t.Error("Detect() invoked runShell — must be a pure-PATH/env check")
	}
}

func TestNVMWindows_Detect_WhitespaceNVMHOME_FallsThrough(t *testing.T) {
	// $NVM_HOME set to whitespace-only should be treated as
	// "not set" — defensive against copy-paste accidents.
	t.Setenv("NVM_HOME", "   ")
	t.Setenv("NVM_SYMLINK", "")
	t.Setenv("PATH", t.TempDir()) // empty PATH

	// Force runShell to fail loudly if Detect() touches it.
	orig := runShell
	called := false
	runShell = func(ctx context.Context, name string, a ...string) (*platform.RunResult, error) {
		called = true
		return nil, nil
	}
	t.Cleanup(func() { runShell = orig })

	if NewNVMWindows().Detect() {
		t.Error("Detect() = true with whitespace-only $NVM_HOME, want false")
	}
	if called {
		t.Error("Detect() invoked runShell — must be a pure-PATH/env check")
	}
}

// --- Mutation stubs -----------------------------------------------------

func TestNVMWindows_MutationMethods_NotImplemented(t *testing.T) {
	// Phase 1: Install / Uninstall / Use / SetDefault /
	// GlobalNpmPrefix return ErrNVMWindowsNotImplemented. This
	// sentinel lets callers distinguish "not implemented" from
	// other errors via errors.Is.
	n := NewNVMWindows()
	ver, _ := semver.NewVersion("20.0.0")

	if err := n.Install(*ver); !errors.Is(err, ErrNVMWindowsNotImplemented) {
		t.Errorf("Install() = %v, want ErrNVMWindowsNotImplemented", err)
	}
	if err := n.Uninstall(*ver); !errors.Is(err, ErrNVMWindowsNotImplemented) {
		t.Errorf("Uninstall() = %v, want ErrNVMWindowsNotImplemented", err)
	}
	if err := n.Use(*ver); !errors.Is(err, ErrNVMWindowsNotImplemented) {
		t.Errorf("Use() = %v, want ErrNVMWindowsNotImplemented", err)
	}
	if err := n.SetDefault(*ver); !errors.Is(err, ErrNVMWindowsNotImplemented) {
		t.Errorf("SetDefault() = %v, want ErrNVMWindowsNotImplemented", err)
	}
	if p, err := n.GlobalNpmPrefix(*ver); !errors.Is(err, ErrNVMWindowsNotImplemented) {
		t.Errorf("GlobalNpmPrefix() err = %v, want ErrNVMWindowsNotImplemented", err)
	} else if p != "" {
		t.Errorf("GlobalNpmPrefix() prefix = %q, want \"\"", p)
	}
}

func TestNVMWindows_CurrentReturnsSentinel(t *testing.T) {
	// Current() on nvm-windows is unimplemented (the upstream
	// `nvm current` subcommand is unreliable on newer builds). The
	// cleanup prompt treats the error as "active version unknown"
	// and proceeds without exclusion — so returning the sentinel
	// here is the right behavior.
	_, err := NewNVMWindows().Current(t.Context())
	if !errors.Is(err, ErrNVMWindowsNotImplemented) {
		t.Errorf("Current() err = %v, want ErrNVMWindowsNotImplemented", err)
	}
}

// --- nvmWindowsRoot / nvmWindowsSymlink helpers -------------------------

func TestNVMWindows_NvmWindowsRootHelper_PrefersEnv(t *testing.T) {
	// $NVM_HOME takes precedence — there's no home-dir fallback
	// because the default location varies between Windows
	// releases.
	t.Setenv("NVM_HOME", `C:\custom\nvm`)
	got := nvmWindowsRoot()
	if got != `C:\custom\nvm` {
		t.Errorf("nvmWindowsRoot() = %q, want %q", got, `C:\custom\nvm`)
	}
}

func TestNVMWindows_NvmWindowsRootHelper_EmptyWhenUnset(t *testing.T) {
	// $NVM_HOME unset → "" (no home-dir fallback).
	t.Setenv("NVM_HOME", "")
	got := nvmWindowsRoot()
	if got != "" {
		t.Errorf("nvmWindowsRoot() = %q, want empty string", got)
	}
}

func TestNVMWindows_NvmWindowsRootHelper_TrimsWhitespace(t *testing.T) {
	// $NVM_HOME set to whitespace-only should be treated as
	// "not set" — fall through to "".
	t.Setenv("NVM_HOME", "   ")
	got := nvmWindowsRoot()
	if got != "" {
		t.Errorf("nvmWindowsRoot() = %q, want empty string (whitespace env should not win)", got)
	}
}

func TestNVMWindows_NvmWindowsSymlinkHelper_ReturnsEnv(t *testing.T) {
	t.Setenv("NVM_SYMLINK", `C:\nvm4w\nodejs`)
	got := nvmWindowsSymlink()
	if got != `C:\nvm4w\nodejs` {
		t.Errorf("nvmWindowsSymlink() = %q, want %q", got, `C:\nvm4w\nodejs`)
	}
}

func TestNVMWindows_NvmWindowsSymlinkHelper_EmptyWhenUnset(t *testing.T) {
	t.Setenv("NVM_SYMLINK", "")
	got := nvmWindowsSymlink()
	if got != "" {
		t.Errorf("nvmWindowsSymlink() = %q, want empty string", got)
	}
}

func TestNVMWindows_NvmWindowsSymlinkHelper_TrimsWhitespace(t *testing.T) {
	t.Setenv("NVM_SYMLINK", "   ")
	got := nvmWindowsSymlink()
	if got != "" {
		t.Errorf("nvmWindowsSymlink() = %q, want empty string (whitespace env should not win)", got)
	}
}
