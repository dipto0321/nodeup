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

// --- parseMiseVersion ---------------------------------------------------

func TestParseMiseVersion_StandardCalVerOutput(t *testing.T) {
	// Observed on mise 2026.6.15:
	//   $ mise --version
	//   2026.6.15 macos-arm64 (2026-06-26)
	//
	// We take the first whitespace-separated token and strip the
	// optional "v" prefix (defensive against future builds that may
	// emit "v2026.6.15").
	got, err := parseMiseVersion("2026.6.15 macos-arm64 (2026-06-26)\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "2026.6.15" {
		t.Errorf("got %q, want %q", got, "2026.6.15")
	}
}

func TestParseMiseVersion_VPrefixed(t *testing.T) {
	// Defensive: a future mise build (or a fork) might emit
	// "v2026.6.15 ...". Strip the prefix.
	got, err := parseMiseVersion("v2026.6.15 linux-x64 (2026-06-26)\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "2026.6.15" {
		t.Errorf("got %q, want %q", got, "2026.6.15")
	}
}

func TestParseMiseVersion_LeadingTrailingWhitespace(t *testing.T) {
	// Whitespace-only differences must not affect parsing.
	got, err := parseMiseVersion("   2026.6.15 macos-arm64 (2026-06-26)   \n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "2026.6.15" {
		t.Errorf("got %q, want %q", got, "2026.6.15")
	}
}

func TestParseMiseVersion_Empty(t *testing.T) {
	_, err := parseMiseVersion("")
	if err == nil {
		t.Error("expected error for empty input, got nil")
	}
}

func TestParseMiseVersion_WhitespaceOnly(t *testing.T) {
	// Whitespace-only output: TrimSpace produces "", we must error.
	_, err := parseMiseVersion("\n   \n")
	if err == nil {
		t.Error("expected error for whitespace-only input, got nil")
	}
}

// --- parseMiseInstalled -------------------------------------------------

func TestParseMiseInstalled_RealOutput(t *testing.T) {
	// Real observed output of `mise ls --installed --json node`:
	//
	//   [
	//     {
	//       "version": "20.11.1",
	//       "installed": true,
	//       "active": false
	//     },
	//     {
	//       "version": "22.5.0",
	//       "requested_version": "lts",
	//       "installed": true,
	//       "active": true
	//     }
	//   ]
	//
	// Fields are JSON-stable as of mise 2026.6.15.
	stdout := `[
		{"version": "20.11.1", "installed": true, "active": false},
		{"version": "22.5.0", "requested_version": "lts", "installed": true, "active": true}
	]`
	got, err := parseMiseInstalled(stdout)
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

func TestParseMiseInstalled_UnsortedInput(t *testing.T) {
	// Mise emits versions in install order. We must sort ascending
	// by semver before returning.
	stdout := `[
		{"version": "22.5.0", "installed": true, "active": false},
		{"version": "18.20.4", "installed": true, "active": false},
		{"version": "20.11.1", "installed": true, "active": false}
	]`
	got, err := parseMiseInstalled(stdout)
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

func TestParseMiseInstalled_EmptyArray(t *testing.T) {
	// Empty array: mise installed but no node versions yet.
	stdout := `[]`
	got, err := parseMiseInstalled(stdout)
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

func TestParseMiseInstalled_EmptyStdout(t *testing.T) {
	// Empty stdout: some mise builds print nothing at all when no
	// versions are installed. We must NOT call this a parse error.
	got, err := parseMiseInstalled("")
	if err != nil {
		t.Fatalf("unexpected error for empty stdout: %v", err)
	}
	if got == nil {
		t.Error("expected non-nil empty slice, got nil")
	}
	if len(got) != 0 {
		t.Errorf("expected empty slice, got %v", got)
	}
}

func TestParseMiseInstalled_SkipsNotInstalled(t *testing.T) {
	// Defensive: even though we pass --installed, upstream might
	// ignore it. Entries with installed=false must be filtered.
	stdout := `[
		{"version": "20.11.1", "installed": true, "active": false},
		{"version": "21.0.0", "installed": false, "active": false},
		{"version": "22.5.0", "installed": true, "active": true}
	]`
	got, err := parseMiseInstalled(stdout)
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

func TestParseMiseInstalled_SkipsEmptyVersion(t *testing.T) {
	// Malformed upstream: a row with version="" must not crash and
	// must not produce a zero-version entry.
	stdout := `[
		{"version": "20.11.1", "installed": true, "active": false},
		{"version": "", "installed": true, "active": false}
	]`
	got, err := parseMiseInstalled(stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"20.11.1"}
	if len(got) != len(want) {
		t.Fatalf("got %d versions, want %d (%v)", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].String() != w {
			t.Errorf("got[%d] = %s, want %s", i, got[i], w)
		}
	}
}

func TestParseMiseInstalled_SkipsUnparseableVersion(t *testing.T) {
	// Non-semver version strings (e.g., a future mise metadata
	// format) must be skipped, not abort the whole parse.
	stdout := `[
		{"version": "20.11.1", "installed": true, "active": false},
		{"version": "latest", "installed": true, "active": false},
		{"version": "22.5.0", "installed": true, "active": true}
	]`
	got, err := parseMiseInstalled(stdout)
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

func TestParseMiseInstalled_MalformedJSON(t *testing.T) {
	// Unparseable JSON is a hard error — we cannot recover from
	// garbled output and should bubble the failure up.
	_, err := parseMiseInstalled(`{"not": "an array"}`)
	if err == nil {
		t.Error("expected error for JSON object instead of array, got nil")
	}
	_, err = parseMiseInstalled(`[{"version": "20.11.1"`)
	if err == nil {
		t.Error("expected error for truncated JSON, got nil")
	}
}

func TestParseMiseInstalled_IgnoresExtraFields(t *testing.T) {
	// mise may add new fields to JSONToolVersion over time. Our
	// struct must silently ignore unknown fields rather than
	// rejecting the entire payload.
	stdout := `[
		{
			"version": "20.11.1",
			"installed": true,
			"active": false,
			"future_field": "ignored",
			"another_future_field": {"nested": "value"}
		}
	]`
	got, err := parseMiseInstalled(stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].String() != "20.11.1" {
		t.Errorf("got %v, want [20.11.1]", got)
	}
}

// --- Mise method tests --------------------------------------------------

func TestMise_Name(t *testing.T) {
	if got := NewMise().Name(); got != "mise" {
		t.Errorf("Name() = %q, want %q", got, "mise")
	}
}

func TestMise_Version_Success(t *testing.T) {
	// Capture the exact command so we know Mise uses `--version`
	// (not `version`, which is the ASDF convention).
	var captured []string
	withStubShell(t,
		func(name string, a []string) {
			captured = append(captured, name)
			captured = append(captured, a...)
		},
		func(req string) (*platform.RunResult, error) {
			if req != "mise --version" {
				t.Errorf("unexpected runShell call: %q (want %q)", req, "mise --version")
			}
			return &platform.RunResult{Stdout: "2026.6.15 macos-arm64 (2026-06-26)\n"}, nil
		},
	)

	got, err := NewMise().Version()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "2026.6.15" {
		t.Errorf("got %q, want %q", got, "2026.6.15")
	}
	wantCaptured := []string{"mise", "--version"}
	if len(captured) != len(wantCaptured) {
		t.Fatalf("captured %v, want %v", captured, wantCaptured)
	}
	for i, w := range wantCaptured {
		if captured[i] != w {
			t.Errorf("captured[%d] = %q, want %q", i, captured[i], w)
		}
	}
}

func TestMise_Version_VPrefixed(t *testing.T) {
	// Defensive parser coverage: v-prefixed output.
	withStubShell(t, nil, func(req string) (*platform.RunResult, error) {
		return &platform.RunResult{Stdout: "v2026.6.15 linux-x64 (2026-06-26)\n"}, nil
	})

	got, err := NewMise().Version()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "2026.6.15" {
		t.Errorf("got %q, want %q", got, "2026.6.15")
	}
}

func TestMise_Version_RunShellError(t *testing.T) {
	wantErr := errors.New("simulated subprocess failure")
	withStubShell(t, nil, func(req string) (*platform.RunResult, error) {
		return nil, wantErr
	})

	_, err := NewMise().Version()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("error %v should wrap %v", err, wantErr)
	}
}

func TestMise_Version_ParsingError(t *testing.T) {
	// runShell succeeded but the body is unparseable.
	withStubShell(t, nil, func(req string) (*platform.RunResult, error) {
		return &platform.RunResult{Stdout: ""}, nil
	})

	_, err := NewMise().Version()
	if err == nil {
		t.Error("expected parsing error from blank output, got nil")
	}
}

func TestMise_ListInstalled_Success(t *testing.T) {
	// Verify ListInstalled uses the right invocation:
	// `mise ls --installed --json node`.
	var captured []string
	withStubShell(t,
		func(name string, a []string) {
			captured = append(captured, name)
			captured = append(captured, a...)
		},
		func(req string) (*platform.RunResult, error) {
			return &platform.RunResult{Stdout: `[
				{"version": "18.20.4", "installed": true, "active": false},
				{"version": "20.11.1", "installed": true, "active": false},
				{"version": "22.5.0", "installed": true, "active": true}
			]`}, nil
		},
	)

	got, err := NewMise().ListInstalled()
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
	wantCaptured := []string{"mise", "ls", "--installed", "--json", "node"}
	if len(captured) != len(wantCaptured) {
		t.Fatalf("captured %v, want %v", captured, wantCaptured)
	}
	for i, w := range wantCaptured {
		if captured[i] != w {
			t.Errorf("captured[%d] = %q, want %q", i, captured[i], w)
		}
	}
}

func TestMise_ListInstalled_EmptyArray(t *testing.T) {
	// Mise installed but no node versions yet. We must return
	// an empty (non-nil) slice, not nil.
	withStubShell(t, nil, func(req string) (*platform.RunResult, error) {
		return &platform.RunResult{Stdout: "[]"}, nil
	})

	got, err := NewMise().ListInstalled()
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

func TestMise_ListInstalled_EmptyStdout(t *testing.T) {
	// Some mise builds print no stdout when no versions match.
	// Same expectation as empty array: empty slice, no error.
	withStubShell(t, nil, func(req string) (*platform.RunResult, error) {
		return &platform.RunResult{Stdout: ""}, nil
	})

	got, err := NewMise().ListInstalled()
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

func TestMise_ListInstalled_RunShellError(t *testing.T) {
	wantErr := errors.New("simulated subprocess failure")
	withStubShell(t, nil, func(req string) (*platform.RunResult, error) {
		return nil, wantErr
	})

	_, err := NewMise().ListInstalled()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("error %v should wrap %v", err, wantErr)
	}
}

func TestMise_ListInstalled_JSONParseError(t *testing.T) {
	// runShell succeeded but the body is malformed JSON.
	withStubShell(t, nil, func(req string) (*platform.RunResult, error) {
		return &platform.RunResult{Stdout: "{not json"}, nil
	})

	_, err := NewMise().ListInstalled()
	if err == nil {
		t.Error("expected JSON parse error, got nil")
	}
}

// --- Detect tests -------------------------------------------------------

func TestMise_Detect_NoBinaryOnPath(t *testing.T) {
	// No `mise` binary on PATH. Detect() must return false.
	//
	// We REPLACE PATH with an empty temp dir so no stray mise
	// binary on the runner shadows our negative test. On Windows,
	// exec.LookPath requires a ".exe" suffix, so an empty dir
	// is a clean negative regardless of platform.
	t.Setenv("PATH", t.TempDir())

	// Force runShell to fail loudly if Detect() touches it.
	orig := runShell
	runShell = func(ctx context.Context, name string, a ...string) (*platform.RunResult, error) {
		t.Fatalf("Detect() must not invoke runShell (was called with %s %v)", name, a)
		return nil, nil
	}
	t.Cleanup(func() { runShell = orig })

	if NewMise().Detect() {
		t.Error("Detect() = true with no mise on PATH, want false")
	}
}

func TestMise_Detect_FindsBinaryOnPath(t *testing.T) {
	// mise on PATH (as a stub binary). Detect() must return true.
	//
	// On Windows, exec.LookPath requires a ".exe" suffix to find
	// an executable; the unadorned "mise" works on unix. Use the
	// platform-correct filename so the test runs identically on
	// linux, macOS, and Windows.
	binDir := t.TempDir()
	binName := "mise"
	if runtime.GOOS == "windows" {
		binName = "mise.exe"
	}
	bin := filepath.Join(binDir, binName)
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Set PATH to ONLY our bin dir so the stub is the only
	// candidate. exec.LookPath walks PATH itself, so a single-
	// entry PATH is sufficient.
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

	if !NewMise().Detect() {
		t.Error("Detect() = false with mise on PATH, want true")
	}
	if called {
		t.Error("Detect() invoked runShell — must be a pure-PATH check")
	}
}

// --- Mutation methods -------------------------------------------------

func TestMise_MutationMethodsInvokeShell(t *testing.T) {
	// Mise mutation commands all take `node@<v>` (tool prefix +
	// version). We verify each call wraps the version with the tool
	// prefix so the CLI doesn't accidentally call `mise install 22.5.0`.
	m := NewMise()
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
		{"Install", func() error { return m.Install(*ver) }, "install node@22.5.0"},
		{"Uninstall", func() error { return m.Uninstall(*ver) }, "uninstall node@22.5.0"},
		{"Use", func() error { return m.Use(*ver) }, "use node@22.5.0"},
		{"SetDefault", func() error { return m.SetDefault(*ver) }, "use --global node@22.5.0"},
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
			want := "mise " + c.wantArg
			if captured != want {
				t.Errorf("%s invoked %q, want %q", c.name, captured, want)
			}
		})
	}
}

func TestMise_Uninstall_PropagatesError(t *testing.T) {
	wantErr := errors.New("simulated mise failure")
	withStubShell(t, nil, func(req string) (*platform.RunResult, error) {
		return nil, wantErr
	})

	v := semver.MustParse("20.0.0")
	err := NewMise().Uninstall(*v)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("error %v should wrap %v", err, wantErr)
	}
}

// --- parseMiseCurrent --------------------------------------------------

func TestParseMiseCurrent_Bare(t *testing.T) {
	v, err := parseMiseCurrent("22.11.0\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.String() != "22.11.0" {
		t.Errorf("got %q, want %q", v.String(), "22.11.0")
	}
}

func TestParseMiseCurrent_WithPrefix(t *testing.T) {
	v, err := parseMiseCurrent("v22.11.0\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.String() != "22.11.0" {
		t.Errorf("got %q, want %q", v.String(), "22.11.0")
	}
}

func TestParseMiseCurrent_SystemIsError(t *testing.T) {
	_, err := parseMiseCurrent("system\n")
	if err == nil {
		t.Error("expected error for 'system' (not a managed version)")
	}
}

func TestParseMiseCurrent_Empty(t *testing.T) {
	_, err := parseMiseCurrent("")
	if err == nil {
		t.Error("expected error on empty input")
	}
}

func TestMiseCurrent_InvokesShell(t *testing.T) {
	var captured string
	withStubShell(t,
		nil,
		func(req string) (*platform.RunResult, error) {
			captured = req
			if req != "mise current node" {
				t.Errorf("unexpected runShell call: %q", req)
			}
			return &platform.RunResult{Stdout: "22.11.0\n"}, nil
		},
	)

	got, err := NewMise().Current()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.String() != "22.11.0" {
		t.Errorf("got %q, want %q", got.String(), "22.11.0")
	}
	if captured == "" {
		t.Error("expected mise current node to be invoked")
	}
}
