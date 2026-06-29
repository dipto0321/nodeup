package detector

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Masterminds/semver/v3"

	"github.com/dipto0321/nodeup/internal/platform"
)

// --- parseASDFVersion ---------------------------------------------------

func TestParseASDFVersion_StandardOutput(t *testing.T) {
	// Observed on asdf 0.18.0:
	//   $ asdf version
	//   v0.18.0
	got, err := parseASDFVersion("v0.18.0\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "0.18.0" {
		t.Errorf("got %q, want %q", got, "0.18.0")
	}
}

func TestParseASDFVersion_BareVersion(t *testing.T) {
	// Defensive: some ASDF builds / forks drop the "v" prefix.
	got, err := parseASDFVersion("0.18.0\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "0.18.0" {
		t.Errorf("got %q, want %q", got, "0.18.0")
	}
}

func TestParseASDFVersion_TrailingWhitespace(t *testing.T) {
	got, err := parseASDFVersion("   v0.18.0   \n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "0.18.0" {
		t.Errorf("got %q, want %q", got, "0.18.0")
	}
}

func TestParseASDFVersion_Empty(t *testing.T) {
	_, err := parseASDFVersion("")
	if err == nil {
		t.Error("expected error for empty input")
	}
}

func TestParseASDFVersion_WhitespaceOnly(t *testing.T) {
	// Whitespace-only output: TrimSpace produces "", we must error.
	_, err := parseASDFVersion("\n   \n")
	if err == nil {
		t.Error("expected error for whitespace-only input")
	}
}

// --- parseASDFInstalled -------------------------------------------------

func TestParseASDFInstalled_RealOutput(t *testing.T) {
	// Real observed output of `asdf list nodejs`:
	//   $ asdf list nodejs
	//    *18.20.4
	//     20.11.1
	//     22.5.0
	//
	// Note: lines are indented by exactly two spaces, and the
	// current version is preceded by " *".
	stdout := " *18.20.4\n  20.11.1\n  22.5.0\n"
	got, err := parseASDFInstalled(stdout)
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

func TestParseASDFInstalled_UnsortedInput(t *testing.T) {
	// ASDF may emit versions in install order. We must sort them
	// ascending by semver before returning.
	stdout := "  22.5.0\n *18.20.4\n  20.11.1\n"
	got, err := parseASDFInstalled(stdout)
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

func TestParseASDFInstalled_NoCompatibleVersionsMessage(t *testing.T) {
	// When no Node versions are installed, ASDF prints a human-readable
	// message on stdout. We must NOT error out — return an empty list.
	stdout := "No compatible versions installed (nodejs)\n"
	got, err := parseASDFInstalled(stdout)
	if err != nil {
		t.Fatalf("expected nil error for 'no versions' message, got %v", err)
	}
	if got == nil {
		t.Error("expected non-nil empty slice, got nil")
	}
	if len(got) != 0 {
		t.Errorf("expected empty slice, got %v", got)
	}
}

func TestParseASDFInstalled_SkipsUnparseable(t *testing.T) {
	// Stray non-version lines must not abort the parse.
	stdout := "  20.11.1\nwarning: deprecated listing\n  22.5.0\nlts/jod\n"
	got, err := parseASDFInstalled(stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"20.11.1", "22.5.0"}
	if len(got) != len(want) {
		t.Fatalf("got %d versions, want %d (%v)", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].String() != w {
			t.Errorf("got[%d] = %s, want %s", i, got[i], w)
		}
	}
}

func TestParseASDFInstalled_Empty(t *testing.T) {
	got, err := parseASDFInstalled("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Error("expected non-nil empty slice, got nil")
	}
	if len(got) != 0 {
		t.Errorf("expected empty slice, got %v", got)
	}
}

func TestParseASDFInstalled_BlankLinesIgnored(t *testing.T) {
	// Real asdf output has trailing newlines, plus some versions
	// may be followed by blank lines.
	stdout := "  20.11.1\n\n  22.5.0\n\n"
	got, err := parseASDFInstalled(stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"20.11.1", "22.5.0"}
	if len(got) != len(want) {
		t.Fatalf("got %d versions, want %d (%v)", len(got), len(want), got)
	}
}

// --- ASDF method tests --------------------------------------------------

func TestASDF_Name(t *testing.T) {
	if got := NewASDF().Name(); got != "asdf" {
		t.Errorf("Name() = %q, want %q", got, "asdf")
	}
}

func TestASDF_Version_Success(t *testing.T) {
	// Capture the exact command so we know ASDF uses `version` (not
	// `--version`, which is the urfave/cli convention).
	var captured []string
	withStubShell(t,
		func(name string, a []string) {
			captured = append(captured, name)
			captured = append(captured, a...)
		},
		func(req string) (*platform.RunResult, error) {
			if req != "asdf version" {
				t.Errorf("unexpected runShell call: %q (want %q)", req, "asdf version")
			}
			return &platform.RunResult{Stdout: "v0.18.0\n"}, nil
		},
	)

	got, err := NewASDF().Version()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "0.18.0" {
		t.Errorf("got %q, want %q", got, "0.18.0")
	}
	if len(captured) < 2 || captured[0] != "asdf" || captured[1] != "version" {
		t.Errorf("expected `asdf version` invocation, got %v", captured)
	}
}

func TestASDF_Version_BareVersion(t *testing.T) {
	// Defensive parser coverage: bare version output (no "v" prefix).
	withStubShell(t, nil, func(req string) (*platform.RunResult, error) {
		return &platform.RunResult{Stdout: "0.18.0\n"}, nil
	})

	got, err := NewASDF().Version()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "0.18.0" {
		t.Errorf("got %q, want %q", got, "0.18.0")
	}
}

func TestASDF_Version_RunShellError(t *testing.T) {
	wantErr := errors.New("simulated subprocess failure")
	withStubShell(t, nil, func(req string) (*platform.RunResult, error) {
		return nil, wantErr
	})

	_, err := NewASDF().Version()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("error %v should wrap %v", err, wantErr)
	}
}

func TestASDF_Version_ParsingError(t *testing.T) {
	// runShell succeeded but the body is unparseable.
	withStubShell(t, nil, func(req string) (*platform.RunResult, error) {
		return &platform.RunResult{Stdout: ""}, nil
	})

	_, err := NewASDF().Version()
	if err == nil {
		t.Error("expected parsing error from blank output, got nil")
	}
}

func TestASDF_ListInstalled_Success(t *testing.T) {
	// Verify ListInstalled uses the right subcommand: `asdf list nodejs`.
	var captured []string
	withStubShell(t,
		func(name string, a []string) {
			captured = append(captured, name)
			captured = append(captured, a...)
		},
		func(req string) (*platform.RunResult, error) {
			return &platform.RunResult{Stdout: " *18.20.4\n  20.11.1\n  22.5.0\n"}, nil
		},
	)

	got, err := NewASDF().ListInstalled()
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
	wantCaptured := []string{"asdf", "list", "nodejs"}
	if len(captured) < len(wantCaptured) {
		t.Fatalf("captured %v, want at least %v", captured, wantCaptured)
	}
	for i, w := range wantCaptured {
		if captured[i] != w {
			t.Errorf("captured[%d] = %q, want %q", i, captured[i], w)
		}
	}
}

func TestASDF_ListInstalled_EmptyStdout(t *testing.T) {
	// ASDF installed but no node versions yet. We must return
	// an empty (non-nil) slice, not nil.
	withStubShell(t, nil, func(req string) (*platform.RunResult, error) {
		return &platform.RunResult{Stdout: ""}, nil
	})

	got, err := NewASDF().ListInstalled()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Error("expected non-nil empty slice, got nil")
	}
	if len(got) != 0 {
		t.Errorf("expected empty slice, got %v", got)
	}
}

func TestASDF_ListInstalled_RunShellError(t *testing.T) {
	wantErr := errors.New("simulated subprocess failure")
	withStubShell(t, nil, func(req string) (*platform.RunResult, error) {
		return nil, wantErr
	})

	_, err := NewASDF().ListInstalled()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("error %v should wrap %v", err, wantErr)
	}
}

// --- Detect tests -------------------------------------------------------

func TestASDF_Detect_NeitherPathNorDir(t *testing.T) {
	// All three branches false: no binary on PATH, no $ASDF_DATA_DIR,
	// no ~/.asdf on disk. Detect() must return false without
	// spawning runShell.
	t.Setenv("ASDF_DATA_DIR", "")
	withStubHomeDir(t, "", errSentinelForTest)

	// Force runShell to fail loudly if Detect() touches it.
	orig := runShell
	runShell = func(ctx context.Context, name string, a ...string) (*platform.RunResult, error) {
		t.Fatalf("Detect() must not invoke runShell (was called with %s %v)", name, a)
		return nil, nil
	}
	t.Cleanup(func() { runShell = orig })

	if NewASDF().Detect() {
		t.Error("Detect() = true with no PATH, no $ASDF_DATA_DIR, and no ~/.asdf; want false")
	}
}

func TestASDF_Detect_FindsDirOnDisk(t *testing.T) {
	// Create a real ~/.asdf directory. We use the homeDir seam
	// (rather than t.Setenv("HOME", ...)) for Windows portability
	// — os.UserHomeDir reads %USERPROFILE% on Windows and ignores
	// $HOME, so the env-var approach wouldn't redirect on Windows.
	tmp := t.TempDir()
	withStubHomeDir(t, tmp, nil)
	t.Setenv("ASDF_DATA_DIR", "")

	// Create ~/.asdf.
	if err := os.MkdirAll(filepath.Join(tmp, ".asdf"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Force runShell to fail loudly if Detect() touches it.
	orig := runShell
	runShell = func(ctx context.Context, name string, a ...string) (*platform.RunResult, error) {
		t.Fatalf("Detect() must not invoke runShell (was called with %s %v)", name, a)
		return nil, nil
	}
	t.Cleanup(func() { runShell = orig })

	if !NewASDF().Detect() {
		t.Error("Detect() = false with ~/.asdf present, want true")
	}
}

func TestASDF_Detect_HonorsASDF_DATA_DIR(t *testing.T) {
	// $ASDF_DATA_DIR set to a real directory should make Detect()
	// return true even when ~/.asdf is missing and PATH has no
	// asdf binary. This is the override path for users with a
	// custom data dir.
	tmp := t.TempDir()
	t.Setenv("ASDF_DATA_DIR", tmp)
	// homeDir returns a path with no .asdf subdir — the override
	// must take precedence over the default lookup.
	withStubHomeDir(t, "/nonexistent/home", nil)

	// Force runShell to fail loudly if Detect() touches it.
	orig := runShell
	runShell = func(ctx context.Context, name string, a ...string) (*platform.RunResult, error) {
		t.Fatalf("Detect() must not invoke runShell (was called with %s %v)", name, a)
		return nil, nil
	}
	t.Cleanup(func() { runShell = orig })

	if !NewASDF().Detect() {
		t.Error("Detect() = false with $ASDF_DATA_DIR set, want true")
	}
}

func TestASDF_Detect_EmptyASDF_DATA_DIR_FallsThrough(t *testing.T) {
	// $ASDF_DATA_DIR explicitly empty string must be treated as
	// "not set" — fall through to the home-dir lookup.
	home := t.TempDir()
	withStubHomeDir(t, home, nil)
	t.Setenv("ASDF_DATA_DIR", "")
	// No ~/.asdf under the home dir.

	// Force runShell to fail loudly if Detect() touches it.
	orig := runShell
	runShell = func(ctx context.Context, name string, a ...string) (*platform.RunResult, error) {
		t.Fatalf("Detect() must not invoke runShell (was called with %s %v)", name, a)
		return nil, nil
	}
	t.Cleanup(func() { runShell = orig })

	if NewASDF().Detect() {
		t.Error("Detect() = true with empty $ASDF_DATA_DIR and no ~/.asdf, want false")
	}
}

// --- Mutation stubs -----------------------------------------------------

func TestASDF_MutationMethods_NotImplemented(t *testing.T) {
	// Phase 1: Install / Uninstall / Use / SetDefault / GlobalNpmPrefix
	// return ErrASDFNotImplemented. This sentinel lets callers
	// distinguish "not implemented" from other errors via errors.Is.
	a := NewASDF()
	ver, _ := semver.NewVersion("20.0.0")

	if err := a.Install(*ver); !errors.Is(err, ErrASDFNotImplemented) {
		t.Errorf("Install() = %v, want ErrASDFNotImplemented", err)
	}
	if err := a.Uninstall(*ver); !errors.Is(err, ErrASDFNotImplemented) {
		t.Errorf("Uninstall() = %v, want ErrASDFNotImplemented", err)
	}
	if err := a.Use(*ver); !errors.Is(err, ErrASDFNotImplemented) {
		t.Errorf("Use() = %v, want ErrASDFNotImplemented", err)
	}
	if err := a.SetDefault(*ver); !errors.Is(err, ErrASDFNotImplemented) {
		t.Errorf("SetDefault() = %v, want ErrASDFNotImplemented", err)
	}
	if p, err := a.GlobalNpmPrefix(*ver); !errors.Is(err, ErrASDFNotImplemented) {
		t.Errorf("GlobalNpmPrefix() err = %v, want ErrASDFNotImplemented", err)
	} else if p != "" {
		t.Errorf("GlobalNpmPrefix() prefix = %q, want \"\"", p)
	}
}

func TestASDF_DoesNotInvokeRunShellOnDetect(t *testing.T) {
	// Defense-in-depth: a follow-up to the per-Detect test above.
	// Even on the "everything works" path (binary on PATH), Detect
	// must not call runShell. We exercise both the "PATH hit" and
	// "dir hit" branches by setting PATH to a temp dir that
	// contains a real "asdf" file, and verify no shell call.
	//
	// We REPLACE PATH with a single entry (the temp dir) rather
	// than prepending — on Windows some GitHub Actions images ship
	// with an asdf / asdf-vm-related binary already on PATH, which
	// would make this test pre-empt our stub binary. A clean PATH
	// guarantees the only "asdf" on the search path is our stub.
	binDir := t.TempDir()
	// Windows exec.LookPath requires a ".exe" suffix to find an
	// executable; the unadorned "asdf" works on unix. Use the
	// platform-correct filename so the test runs identically on
	// linux, macOS, and Windows.
	binName := "asdf"
	if runtime.GOOS == "windows" {
		binName = "asdf.exe"
	}
	bin := filepath.Join(binDir, binName)
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Set PATH to ONLY our bin dir. exec.LookPath walks PATH
	// itself, so a single-entry PATH is sufficient and removes
	// any chance of a stray asdf binary elsewhere on the runner
	// shadowing our stub.
	t.Setenv("PATH", binDir)

	orig := runShell
	called := false
	runShell = func(ctx context.Context, name string, a ...string) (*platform.RunResult, error) {
		called = true
		return nil, nil
	}
	t.Cleanup(func() { runShell = orig })

	if !NewASDF().Detect() {
		t.Error("Detect() = false with asdf on PATH, want true")
	}
	if called {
		t.Error("Detect() invoked runShell — must be a pure-PATH/on-disk check")
	}
}

func TestASDF_AsdfDataDirHelper_PrefersEnv(t *testing.T) {
	// $ASDF_DATA_DIR takes precedence over $HOME/.asdf.
	t.Setenv("ASDF_DATA_DIR", "/custom/asdf")
	got := asdfDataDir()
	if got != "/custom/asdf" {
		t.Errorf("asdfDataDir() = %q, want %q", got, "/custom/asdf")
	}
}

func TestASDF_AsdfDataDirHelper_FallsBackToHome(t *testing.T) {
	// $ASDF_DATA_DIR unset → use $HOME/.asdf.
	t.Setenv("ASDF_DATA_DIR", "")
	withStubHomeDir(t, "/home/user", nil)
	got := asdfDataDir()
	want := filepath.Join("/home/user", ".asdf")
	if got != want {
		t.Errorf("asdfDataDir() = %q, want %q", got, want)
	}
}

func TestASDF_AsdfDataDirHelper_ReturnsEmptyWhenNoHome(t *testing.T) {
	// $ASDF_DATA_DIR unset and homeDir() errored → "".
	t.Setenv("ASDF_DATA_DIR", "")
	withStubHomeDir(t, "", errSentinelForTest)
	got := asdfDataDir()
	if got != "" {
		t.Errorf("asdfDataDir() = %q, want empty string", got)
	}
}

func TestASDF_AsdfDataDirHelper_TrimsWhitespace(t *testing.T) {
	// $ASDF_DATA_DIR set to whitespace-only should be treated as
	// "not set" so we fall through to the home-dir lookup.
	t.Setenv("ASDF_DATA_DIR", "   ")
	withStubHomeDir(t, "/home/user", nil)
	got := asdfDataDir()
	want := filepath.Join("/home/user", ".asdf")
	if got != want {
		t.Errorf("asdfDataDir() = %q, want %q (whitespace env should not win)", got, want)
	}
}

// --- asdfDataDir — bonus defensive case ---------------------------------

func TestASDF_AsdfDataDirHelper_TrimsAndCleans(t *testing.T) {
	// Verify that a value with leading/trailing whitespace is
	// trimmed before being returned (so paths work even when
	// users accidentally export "  /custom/asdf  ").
	t.Setenv("ASDF_DATA_DIR", "  /custom/asdf  ")
	got := asdfDataDir()
	if got != "/custom/asdf" {
		t.Errorf("asdfDataDir() = %q, want %q (must be trimmed)", got, "/custom/asdf")
	}
	if strings.TrimSpace(got) != got {
		t.Errorf("asdfDataDir() = %q, must not have leading/trailing whitespace", got)
	}
}
