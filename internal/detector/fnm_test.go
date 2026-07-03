package detector

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Masterminds/semver/v3"

	"github.com/dipto0321/nodeup/internal/platform"
)

// withStubShell temporarily swaps the package-level runShell var so a
// test can intercept fnm invocations without spawning a real subprocess.
// All detector tests use this helper so the swap/restore ceremony is
// in one place.
//
// The args callback receives the (name, args...) tuple fnm.go passes to
// runShell, letting the test assert what command was issued. The reply
// callback returns the canned RunResult.
func withStubShell(t *testing.T, args func(name string, a []string), reply func(req string) (*platform.RunResult, error)) {
	t.Helper()
	orig := runShell
	runShell = func(ctx context.Context, name string, a ...string) (*platform.RunResult, error) {
		if args != nil {
			args(name, a)
		}
		return reply(strings.Join(append([]string{name}, a...), " "))
	}
	t.Cleanup(func() { runShell = orig })
}

// --- parseFNMVersion ----------------------------------------------------

func TestParseFNMVersion_StandardOutput(t *testing.T) {
	// Observed on fnm 1.39.0:
	//   $ fnm --version
	//   fnm 1.39.0
	got, err := parseFNMVersion("fnm 1.39.0\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "1.39.0" {
		t.Errorf("got %q, want %q", got, "1.39.0")
	}
}

func TestParseFNMVersion_BareVersion(t *testing.T) {
	// Some forks (or very old fnm) drop the "fnm " prefix.
	got, err := parseFNMVersion("1.39.0\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "1.39.0" {
		t.Errorf("got %q, want %q", got, "1.39.0")
	}
}

func TestParseFNMVersion_TrailingWhitespace(t *testing.T) {
	got, err := parseFNMVersion("   fnm 1.40.2   \n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "1.40.2" {
		t.Errorf("got %q, want %q", got, "1.40.2")
	}
}

func TestParseFNMVersion_Empty(t *testing.T) {
	_, err := parseFNMVersion("")
	if err == nil {
		t.Error("expected error for empty input")
	}
}

// --- parseFNMInstalled --------------------------------------------------

func TestParseFNMInstalled_HappyPath(t *testing.T) {
	// Observed from a real `fnm list` (note: no header row, * marks
	// default, "system" represents the system Node).
	input := "* v22.11.0 default\n* v24.15.0\n* system\n"

	got, err := parseFNMInstalled(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{"22.11.0", "24.15.0"} // sorted asc, "system" excluded
	if len(got) != len(want) {
		t.Fatalf("got %d versions, want %d (%v)", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].String() != w {
			t.Errorf("got[%d] = %s, want %s", i, got[i], w)
		}
	}
}

func TestParseFNMInstalled_HandlesUnparseableLines(t *testing.T) {
	// fnm occasionally emits banner/empty/garbage lines. We must skip
	// them silently rather than aborting the entire list.
	input := "* v20.0.0\n\nbogus-not-a-version\n* v18.19.0\n   \n* system\n"

	got, err := parseFNMInstalled(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"18.19.0", "20.0.0"}
	if len(got) != len(want) {
		t.Fatalf("got %d versions, want %d (%v)", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].String() != w {
			t.Errorf("got[%d] = %s, want %s", i, got[i], w)
		}
	}
}

func TestParseFNMInstalled_Empty(t *testing.T) {
	got, err := parseFNMInstalled("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty slice, got %v", got)
	}
}

func TestParseFNMInstalled_OnlySystem(t *testing.T) {
	// If fnm is installed but no versions are managed, the only line is
	// "* system". Result should be an empty (not nil) slice.
	got, err := parseFNMInstalled("* system\n")
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

// --- FNM method tests ---------------------------------------------------

func TestFNM_Name(t *testing.T) {
	if got := NewFNM().Name(); got != "fnm" {
		t.Errorf("Name() = %q, want %q", got, "fnm")
	}
}

func TestFNM_Version_Success(t *testing.T) {
	var captured []string
	withStubShell(t,
		func(name string, a []string) { captured = append(captured, name); captured = append(captured, a...) },
		func(req string) (*platform.RunResult, error) {
			if req != "fnm --version" {
				t.Errorf("unexpected runShell call: %q", req)
			}
			return &platform.RunResult{Stdout: "fnm 1.39.0\n"}, nil
		},
	)

	got, err := NewFNM().Version()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "1.39.0" {
		t.Errorf("got %q, want %q", got, "1.39.0")
	}
	if len(captured) == 0 || captured[0] != "fnm" {
		t.Errorf("expected fnm to be invoked, got %v", captured)
	}
}

func TestFNM_Version_RunShellError(t *testing.T) {
	wantErr := errors.New("simulated subprocess failure")
	withStubShell(t, nil, func(req string) (*platform.RunResult, error) {
		return nil, wantErr
	})

	_, err := NewFNM().Version()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// Must wrap the underlying error (not replace it) so callers can
	// use errors.Is(err, platform.ErrNotFound).
	if !errors.Is(err, wantErr) {
		t.Errorf("error %v should wrap %v", err, wantErr)
	}
}

func TestFNM_Version_ParsingError(t *testing.T) {
	// runShell succeeded but the body is unparseable.
	withStubShell(t, nil, func(req string) (*platform.RunResult, error) {
		return &platform.RunResult{Stdout: "\n   \n"}, nil
	})

	_, err := NewFNM().Version()
	if err == nil {
		t.Error("expected parsing error from blank output, got nil")
	}
}

func TestFNM_ListInstalled_Success(t *testing.T) {
	input := "* v20.18.0\n* v22.11.0\n* v24.15.0\n* system\n"

	withStubShell(t,
		func(name string, a []string) {
			if name != "fnm" || len(a) != 1 || a[0] != "list" {
				t.Errorf("expected fnm list, got %s %v", name, a)
			}
		},
		func(req string) (*platform.RunResult, error) {
			return &platform.RunResult{Stdout: input}, nil
		},
	)

	got, err := NewFNM().ListInstalled(t.Context())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"20.18.0", "22.11.0", "24.15.0"}
	if len(got) != len(want) {
		t.Fatalf("got %d versions, want %d (%v)", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].String() != w {
			t.Errorf("got[%d] = %s, want %s", i, got[i], w)
		}
	}
}

func TestFNM_ListInstalled_RunShellError(t *testing.T) {
	wantErr := errors.New("simulated fnm failure")
	withStubShell(t, nil, func(req string) (*platform.RunResult, error) {
		return nil, wantErr
	})

	_, err := NewFNM().ListInstalled(t.Context())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("error %v should wrap %v", err, wantErr)
	}
}

func TestFNM_MutationMethodsInvokeShell(t *testing.T) {
	// Phase 4 (the upgrade command) needs the mutation methods to
	// actually shell out to fnm — not return a stub. Each call
	// should produce a single runShell invocation with a known argv.
	f := NewFNM()
	v, err := semver.NewVersion("22.11.0")
	if err != nil {
		t.Fatal(err)
	}

	type tc struct {
		name    string
		call    func() error
		wantArg string
	}
	cases := []tc{
		{"Install", func() error { return f.Install(*v) }, "install 22.11.0"},
		{"Uninstall", func() error { return f.Uninstall(*v) }, "uninstall 22.11.0"},
		{"Use", func() error { return f.Use(*v) }, "use 22.11.0"},
		{"SetDefault", func() error { return f.SetDefault(*v) }, "default 22.11.0"},
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
			want := "fnm " + c.wantArg
			if captured != want {
				t.Errorf("%s invoked %q, want %q", c.name, captured, want)
			}
		})
	}
}

func TestFNM_Uninstall_PropagatesError(t *testing.T) {
	// fnm refuses to uninstall the default; that error must surface
	// to the caller wrapped, so the CLI can show a useful message
	// instead of silently leaving the version on disk.
	wantErr := errors.New("cannot uninstall current version")
	withStubShell(t, nil, func(req string) (*platform.RunResult, error) {
		return nil, wantErr
	})

	v, _ := semver.NewVersion("20.0.0")
	err := NewFNM().Uninstall(*v)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("error %v should wrap %v", err, wantErr)
	}
}

// --- parseFNMCurrent ----------------------------------------------------

func TestParseFNMCurrent_StandardOutput(t *testing.T) {
	// Observed on fnm 1.39.0:
	//   $ fnm current
	//   v22.11.0
	v, err := parseFNMCurrent("v22.11.0\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.String() != "22.11.0" {
		t.Errorf("got %q, want %q", v.String(), "22.11.0")
	}
}

func TestParseFNMCurrent_BareVersion(t *testing.T) {
	// Older fnm versions drop the "v" prefix.
	v, err := parseFNMCurrent("20.18.0\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.String() != "20.18.0" {
		t.Errorf("got %q, want %q", v.String(), "20.18.0")
	}
}

func TestParseFNMCurrent_Empty(t *testing.T) {
	_, err := parseFNMCurrent("")
	if err == nil {
		t.Error("expected error on empty input")
	}
}

func TestFNMCurrent_InvokesShell(t *testing.T) {
	var captured string
	withStubShell(t,
		nil,
		func(req string) (*platform.RunResult, error) {
			captured = req
			if req != "fnm current" {
				t.Errorf("unexpected runShell call: %q", req)
			}
			return &platform.RunResult{Stdout: "v22.11.0\n"}, nil
		},
	)

	got, err := NewFNM().Current(t.Context())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.String() != "22.11.0" {
		t.Errorf("got %q, want %q", got.String(), "22.11.0")
	}
	if captured == "" {
		t.Error("expected fnm current to be invoked")
	}
}

func TestFNM_DetectUsesSoftLookup(t *testing.T) {
	// Detect() must NOT call runShell — it's the cheap probe documented
	// in the Manager interface contract. We verify this by leaving
	// runShell in its real (subprocess-invoking) state, replacing it
	// only with a panic. If Detect() never touches runShell, the panic
	// never fires.
	//
	// We can't replace it with a panic-storing helper because the test
	// runs in CI where fnm may or may not be installed — so we just
	// sanity-check that calling Detect doesn't blow up. The "no
	// subprocess" guarantee is verified by the absence of any
	// runShell interaction in the implementation.
	t.Run("does not panic", func(t *testing.T) {
		// Save and force runShell to panic if invoked.
		orig := runShell
		runShell = func(ctx context.Context, name string, a ...string) (*platform.RunResult, error) {
			t.Fatalf("Detect() must not invoke runShell (was called with %s %v)", name, a)
			return nil, nil
		}
		defer func() { runShell = orig }()

		// Should return without calling runShell.
		_ = NewFNM().Detect()
	})
}
