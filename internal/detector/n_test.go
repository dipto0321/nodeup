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

// --- parseNVersion ------------------------------------------------------

func TestParseNVersion_StandardOutput(t *testing.T) {
	// Real observed output of `n --version` (n 10.2.0):
	//   $ n --version
	//   10.2.0
	//
	// The upstream script does `echo "$VERSION" && exit 0` — no
	// "v" prefix, single line, trailing newline.
	got, err := parseNVersion("10.2.0\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "10.2.0" {
		t.Errorf("got %q, want %q", got, "10.2.0")
	}
}

func TestParseNVersion_VPrefixed(t *testing.T) {
	// Defensive: some forks / pre-release builds may prepend "v".
	got, err := parseNVersion("v10.2.0\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "10.2.0" {
		t.Errorf("got %q, want %q", got, "10.2.0")
	}
}

func TestParseNVersion_LeadingTrailingWhitespace(t *testing.T) {
	// Whitespace-only differences must not affect parsing.
	got, err := parseNVersion("   10.2.0   \n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "10.2.0" {
		t.Errorf("got %q, want %q", got, "10.2.0")
	}
}

func TestParseNVersion_Empty(t *testing.T) {
	_, err := parseNVersion("")
	if err == nil {
		t.Error("expected error for empty input, got nil")
	}
}

func TestParseNVersion_WhitespaceOnly(t *testing.T) {
	// Whitespace-only output: TrimSpace produces "", we must error.
	_, err := parseNVersion("\n   \n")
	if err == nil {
		t.Error("expected error for whitespace-only input, got nil")
	}
}

// --- parseNInstalled ----------------------------------------------------

func TestParseNInstalled_RealOutput(t *testing.T) {
	// Real observed output of `n ls`:
	//   $ n ls
	//   node/18.20.4
	//   node/20.11.1
	//   node/22.5.0
	//
	// Per upstream `display_versions_paths` (bin/n):
	//   find "$CACHE_DIR" -maxdepth 2 -type d
	//     | sed 's|...CACHE_DIR.../||g'
	//     | grep -E "/[0-9]+\.[0-9]+\.[0-9]+"
	//     | sort ...
	// The grep regex filters out anything that doesn't have a
	// MAJOR.MINOR.PATCH semver.
	stdout := "node/18.20.4\nnode/20.11.1\nnode/22.5.0\n"
	got, err := parseNInstalled(stdout)
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

func TestParseNInstalled_UnsortedInput(t *testing.T) {
	// n's own sort is numeric by semver components, but a future
	// upstream change could break that. Re-sort defensively.
	stdout := "node/22.5.0\nnode/18.20.4\nnode/20.11.1\n"
	got, err := parseNInstalled(stdout)
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

func TestParseNInstalled_Empty(t *testing.T) {
	// Empty stdout: n installed but no node versions yet. n ls
	// exits 0 with no output, which we map to an empty (non-nil)
	// slice.
	got, err := parseNInstalled("")
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

func TestParseNInstalled_BlankLinesIgnored(t *testing.T) {
	// Real n output has trailing newlines, plus possibly blank
	// lines between entries on some shells.
	stdout := "node/20.11.1\n\nnode/22.5.0\n\n"
	got, err := parseNInstalled(stdout)
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

func TestParseNInstalled_SkipsMalformedLines(t *testing.T) {
	// Lines without a slash, or with a trailing slash but no
	// version, must not abort the parse.
	stdout := "node/20.11.1\nnode\nnode/\nnode/22.5.0\n"
	got, err := parseNInstalled(stdout)
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

func TestParseNInstalled_SkipsUnparseableVersions(t *testing.T) {
	// Non-semver version strings must be skipped, not abort.
	stdout := "node/20.11.1\nnode/latest\nnode/22.5.0\n"
	got, err := parseNInstalled(stdout)
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

func TestParseNInstalled_HandlesDeeperPaths(t *testing.T) {
	// A future n version might add an extra path component
	// (e.g., "node/v20/20.11.1"). We use LastIndex so the parser
	// is robust to that.
	stdout := "node/v20/20.11.1\nnode/v22/22.5.0\n"
	got, err := parseNInstalled(stdout)
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

// --- N method tests -----------------------------------------------------

func TestN_Name(t *testing.T) {
	if got := NewN().Name(); got != "n" {
		t.Errorf("Name() = %q, want %q", got, "n")
	}
}

func TestN_Version_Success(t *testing.T) {
	// Capture the exact command so we know N uses `--version`
	// (matching most CLIs; not `version` like ASDF).
	var captured []string
	withStubShell(t,
		func(name string, a []string) {
			captured = append(captured, name)
			captured = append(captured, a...)
		},
		func(req string) (*platform.RunResult, error) {
			if req != "n --version" {
				t.Errorf("unexpected runShell call: %q (want %q)", req, "n --version")
			}
			return &platform.RunResult{Stdout: "10.2.0\n"}, nil
		},
	)

	got, err := NewN().Version()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "10.2.0" {
		t.Errorf("got %q, want %q", got, "10.2.0")
	}
	wantCaptured := []string{"n", "--version"}
	if len(captured) != len(wantCaptured) {
		t.Fatalf("captured %v, want %v", captured, wantCaptured)
	}
	for i, w := range wantCaptured {
		if captured[i] != w {
			t.Errorf("captured[%d] = %q, want %q", i, captured[i], w)
		}
	}
}

func TestN_Version_VPrefixed(t *testing.T) {
	// Defensive parser coverage: v-prefixed output (some forks).
	withStubShell(t, nil, func(req string) (*platform.RunResult, error) {
		return &platform.RunResult{Stdout: "v10.2.0\n"}, nil
	})

	got, err := NewN().Version()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "10.2.0" {
		t.Errorf("got %q, want %q", got, "10.2.0")
	}
}

func TestN_Version_RunShellError(t *testing.T) {
	wantErr := errors.New("simulated subprocess failure")
	withStubShell(t, nil, func(req string) (*platform.RunResult, error) {
		return nil, wantErr
	})

	_, err := NewN().Version()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("error %v should wrap %v", err, wantErr)
	}
}

func TestN_Version_ParsingError(t *testing.T) {
	// runShell succeeded but the body is unparseable.
	withStubShell(t, nil, func(req string) (*platform.RunResult, error) {
		return &platform.RunResult{Stdout: ""}, nil
	})

	_, err := NewN().Version()
	if err == nil {
		t.Error("expected parsing error from blank output, got nil")
	}
}

func TestN_ListInstalled_Success(t *testing.T) {
	// Verify ListInstalled uses the right subcommand: `n ls`.
	var captured []string
	withStubShell(t,
		func(name string, a []string) {
			captured = append(captured, name)
			captured = append(captured, a...)
		},
		func(req string) (*platform.RunResult, error) {
			return &platform.RunResult{Stdout: "node/18.20.4\nnode/20.11.1\nnode/22.5.0\n"}, nil
		},
	)

	got, err := NewN().ListInstalled()
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
	wantCaptured := []string{"n", "ls"}
	if len(captured) != len(wantCaptured) {
		t.Fatalf("captured %v, want %v", captured, wantCaptured)
	}
	for i, w := range wantCaptured {
		if captured[i] != w {
			t.Errorf("captured[%d] = %q, want %q", i, captured[i], w)
		}
	}
}

func TestN_ListInstalled_EmptyStdout(t *testing.T) {
	// n installed but no node versions yet. `n ls` prints nothing
	// and exits 0; we must return an empty (non-nil) slice.
	withStubShell(t, nil, func(req string) (*platform.RunResult, error) {
		return &platform.RunResult{Stdout: ""}, nil
	})

	got, err := NewN().ListInstalled()
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

func TestN_ListInstalled_RunShellError(t *testing.T) {
	wantErr := errors.New("simulated subprocess failure")
	withStubShell(t, nil, func(req string) (*platform.RunResult, error) {
		return nil, wantErr
	})

	_, err := NewN().ListInstalled()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("error %v should wrap %v", err, wantErr)
	}
}

func TestN_ListInstalled_SkipsUnparseableLines(t *testing.T) {
	// runShell succeeded but the body has malformed lines. We
	// must not abort — just skip and return what parsed.
	withStubShell(t, nil, func(req string) (*platform.RunResult, error) {
		return &platform.RunResult{Stdout: "node/20.11.1\nnode/latest\nnode/22.5.0\n"}, nil
	})

	got, err := NewN().ListInstalled()
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

// --- Detect tests -------------------------------------------------------

func TestN_Detect_NoBinaryOnPath(t *testing.T) {
	// No `n` binary on PATH. Detect() must return false.
	//
	// We REPLACE PATH with an empty temp dir so no stray n
	// binary on the runner shadows our negative test. On
	// Windows, exec.LookPath requires a ".exe" suffix, so an
	// empty dir is a clean negative regardless of platform.
	t.Setenv("PATH", t.TempDir())

	// Force runShell to fail loudly if Detect() touches it.
	orig := runShell
	runShell = func(ctx context.Context, name string, a ...string) (*platform.RunResult, error) {
		t.Fatalf("Detect() must not invoke runShell (was called with %s %v)", name, a)
		return nil, nil
	}
	t.Cleanup(func() { runShell = orig })

	if NewN().Detect() {
		t.Error("Detect() = true with no n on PATH, want false")
	}
}

func TestN_Detect_FindsBinaryOnPath(t *testing.T) {
	// n on PATH (as a stub binary). Detect() must return true.
	//
	// On Windows, exec.LookPath requires a ".exe" suffix to find
	// an executable; the unadorned "n" works on unix. Use the
	// platform-correct filename so the test runs identically on
	// linux, macOS, and Windows.
	binDir := t.TempDir()
	binName := "n"
	if runtime.GOOS == "windows" {
		binName = "n.exe"
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

	if !NewN().Detect() {
		t.Error("Detect() = false with n on PATH, want true")
	}
	if called {
		t.Error("Detect() invoked runShell — must be a pure-PATH check")
	}
}

// --- Mutation methods -------------------------------------------------

func TestN_MutationMethodsInvokeShell(t *testing.T) {
	// n's mutation commands all take a bare `<v>` (no tool prefix).
	m := NewN()
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
		{"Install", func() error { return m.Install(*ver) }, "install 22.5.0"},
		{"Uninstall", func() error { return m.Uninstall(*ver) }, "uninstall 22.5.0"},
		{"Use", func() error { return m.Use(*ver) }, "22.5.0"},
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
			want := "n " + c.wantArg
			if captured != want {
				t.Errorf("%s invoked %q, want %q", c.name, captured, want)
			}
		})
	}
}

func TestN_SetDefaultIsNoOp(t *testing.T) {
	// n auto-uses the latest installed version, so SetDefault is a
	// no-op. We verify it returns nil without invoking the shell.
	m := NewN()
	ver, err := semver.NewVersion("22.5.0")
	if err != nil {
		t.Fatal(err)
	}

	orig := runShell
	runShell = func(ctx context.Context, name string, a ...string) (*platform.RunResult, error) {
		t.Fatalf("SetDefault must not invoke runShell for n (called %s %v)", name, a)
		return nil, nil
	}
	t.Cleanup(func() { runShell = orig })

	if err := m.SetDefault(*ver); err != nil {
		t.Errorf("SetDefault = %v, want nil", err)
	}
}

func TestN_Uninstall_PropagatesError(t *testing.T) {
	wantErr := errors.New("simulated n failure")
	withStubShell(t, nil, func(req string) (*platform.RunResult, error) {
		return nil, wantErr
	})

	v := semver.MustParse("20.0.0")
	err := NewN().Uninstall(*v)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("error %v should wrap %v", err, wantErr)
	}
}

// --- parseNCurrent ----------------------------------------------------

func TestParseNCurrent_Bare(t *testing.T) {
	v, err := parseNCurrent("22.11.0\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.String() != "22.11.0" {
		t.Errorf("got %q, want %q", v.String(), "22.11.0")
	}
}

func TestParseNCurrent_WithPrefix(t *testing.T) {
	v, err := parseNCurrent("v22.11.0\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.String() != "22.11.0" {
		t.Errorf("got %q, want %q", v.String(), "22.11.0")
	}
}

func TestParseNCurrent_Empty(t *testing.T) {
	_, err := parseNCurrent("")
	if err == nil {
		t.Error("expected error on empty input")
	}
}

func TestNCurrent_InvokesShell(t *testing.T) {
	var captured string
	withStubShell(t,
		nil,
		func(req string) (*platform.RunResult, error) {
			captured = req
			if req != "n current" {
				t.Errorf("unexpected runShell call: %q", req)
			}
			return &platform.RunResult{Stdout: "22.11.0\n"}, nil
		},
	)

	got, err := NewN().Current()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.String() != "22.11.0" {
		t.Errorf("got %q, want %q", got.String(), "22.11.0")
	}
	if captured == "" {
		t.Error("expected n current to be invoked")
	}
}
