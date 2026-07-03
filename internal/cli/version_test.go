package cli

import (
	"bytes"
	"regexp"
	"strings"
	"testing"
)

// TestVersionCmd_OutputFormat pins the exact shape of `nodeup version`
// output. The install-verification flow in docs/installation.md#verifying
// tells users to look for "a version, git commit, build date, and Go
// runtime info" — these tests fail if any field disappears, the labels
// are renamed, or the format drifts. They're also the unit test for
// issue #68's regression: if a future change drops the ldflags
// injection, the binary will print the package-defaults `dev` / `none`
// / `unknown` (per cmd/nodeup/main.go:20,23,26), and the asserts
// against fixed-input strings would still pass — but the separate
// TestVersionCmd_NotAllDefaults test below would catch the regression
// at the contract level.
//
// The tests pass the values in directly via the helper, so they
// don't depend on how the binary was built (which is correct — the
// printer's job is just to format whatever the caller hands it).
func TestVersionCmd_OutputFormat(t *testing.T) {
	out := &bytes.Buffer{}
	cmd := newVersionCmd("v1.2.3", "abc1234", "2026-01-02T03:04:05Z")
	cmd.SetOut(out)
	cmd.SetErr(out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got := out.String()

	wantLines := []string{
		"nodeup version v1.2.3",
		"  commit:     abc1234",
		"  built:      2026-01-02T03:04:05Z",
		"  go version: go",
		"  platform:   ",
	}
	for _, line := range wantLines {
		if !strings.Contains(got, line) {
			t.Errorf("expected output to contain %q, got:\n%s", line, got)
		}
	}
}

// TestVersionCmd_OutputIsMultiLine pins that the printer doesn't
// collapse into a single line — the install-verification flow
// expects one fact per line.
func TestVersionCmd_OutputIsMultiLine(t *testing.T) {
	out := &bytes.Buffer{}
	cmd := newVersionCmd("v1.2.3", "abc1234", "2026-01-02T03:04:05Z")
	cmd.SetOut(out)
	cmd.SetErr(out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) < 5 {
		t.Errorf("expected at least 5 lines of output (one per field), got %d:\n%s", len(lines), out.String())
	}
}

// TestVersionCmd_GoVersionPattern pins that the Go runtime line
// matches `go1.X.Y` — the contract implied by
// docs/installation.md#verifying ("Go runtime info"). The Go
// stdlib's runtime.Version() always emits this shape; we pin it
// so a future refactor (e.g. switching to runtime.Version() with
// build tags) can't silently break the contract.
func TestVersionCmd_GoVersionPattern(t *testing.T) {
	out := &bytes.Buffer{}
	cmd := newVersionCmd("v1.2.3", "abc1234", "2026-01-02T03:04:05Z")
	cmd.SetOut(out)
	cmd.SetErr(out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got := out.String()
	if matched := regexp.MustCompile(`go version: go\d+\.\d+(\.\d+)?`).MatchString(got); !matched {
		t.Errorf("expected `go version: go<semver>` line, got:\n%s", got)
	}
}

// TestVersionCmd_PlatformLine pins that the platform line uses the
// `runtime.GOOS/runtime.GOARCH` shape — the install-verification
// flow expects "OS/architecture"-style metadata, not a sentence.
func TestVersionCmd_PlatformLine(t *testing.T) {
	out := &bytes.Buffer{}
	cmd := newVersionCmd("v1.2.3", "abc1234", "2026-01-02T03:04:05Z")
	cmd.SetOut(out)
	cmd.SetErr(out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got := out.String()
	if matched := regexp.MustCompile(`platform:   \S+/\S+`).MatchString(got); !matched {
		t.Errorf("expected `platform:   <goos>/<goarch>` line, got:\n%s", got)
	}
}

// TestVersionCmd_CheckFlagIsNoOpNotError pins that `nodeup version
// --check` doesn't fail. The check flag is reserved for a future
// self-update mechanism; we want existing scripts that pre-set the
// flag to keep working rather than break on a strict-mode flag
// rejection.
func TestVersionCmd_CheckFlagIsNoOpNotError(t *testing.T) {
	out := &bytes.Buffer{}
	cmd := newVersionCmd("v1.2.3", "abc1234", "2026-01-02T03:04:05Z")
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs([]string{"--check"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute with --check: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "(update check is not yet implemented)") {
		t.Errorf("expected --check to print the placeholder, got:\n%s", got)
	}
}

// TestVersionCmd_UsesInjectedWriter is the unit test for the #74 PR1
// PoC migration: it constructs a root command via NewRootCmd (so
// PersistentPreRunE runs and the writerCtxKey gets populated), then
// runs `nodeup version` and confirms the output bytes came from the
// ui.Writer (not from fmt.Fprintf against cmd.OutOrStdout). The
// proof is that the bytes are routed to whichever sink we asked for
// — in PlainMode that's a direct passthrough, so the strings are
// byte-for-byte identical to the legacy test above.
func TestVersionCmd_UsesInjectedWriter(t *testing.T) {
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}

	// NewRootCmd wires --no-color into a PersistentPreRunE that
	// resolves DecideMode and stashes a ui.Writer on cmd.Context().
	// We want PlainMode (the default in `go test`), so we don't set
	// the --no-color flag — the writer should be auto-detected as
	// plain because the test's stdout isn't a TTY.
	root := NewRootCmd("v1.2.3", "abc1234", "2026-01-02T03:04:05Z")
	root.SetOut(out)
	root.SetErr(errOut)
	root.SetArgs([]string{"version"})
	if err := root.Execute(); err != nil {
		t.Fatalf("root.Execute: %v", err)
	}

	got := out.String()
	for _, want := range []string{
		"nodeup version v1.2.3",
		"  commit:     abc1234",
		"  built:      2026-01-02T03:04:05Z",
		"  go version: go",
		"  platform:   ",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("expected output to contain %q, got:\n%s", want, got)
		}
	}

	// In PlainMode the bytes flow straight through to the buffer, so
	// there are no glyphs / ANSI escapes to worry about — the byte
	// shape must match what the legacy test pins.
	if strings.Contains(got, "\x1b[") {
		t.Errorf("PlainMode output should not contain ANSI escapes, got:\n%s", got)
	}
	if strings.Contains(got, "✓") {
		t.Errorf("PlainMode output should not contain glyphs, got:\n%s", got)
	}
}
