package ui

import (
	"bytes"
	"strings"
	"testing"
)

// TestPlainWriter_RoutesByLevel pins the basic invariant: Success /
// Info / Println go to stdout; Warn / Error go to stderr. The
// plain writer never decorates, so the bytes the caller passes are
// the bytes the caller gets back (with a trailing newline).
func TestPlainWriter_RoutesByLevel(t *testing.T) {
	var out, errOut bytes.Buffer
	w := NewWriter(PlainMode, &out, &errOut)

	w.Success("upgraded")
	w.Info("using fnm")
	w.Println("plain row")
	w.Warn("skipped")
	w.Error("snapshot failed")

	if got := out.String(); !strings.Contains(got, "upgraded") ||
		!strings.Contains(got, "using fnm") ||
		!strings.Contains(got, "plain row") {
		t.Errorf("stdout missing expected lines, got: %q", got)
	}
	if got := errOut.String(); !strings.Contains(got, "skipped") ||
		!strings.Contains(got, "snapshot failed") {
		t.Errorf("stderr missing expected lines, got: %q", got)
	}
	// Cross-contamination check: stderr must not have success/info
	// lines, stdout must not have warn/error lines.
	if strings.Contains(out.String(), "skipped") {
		t.Errorf("stdout received a Warn line: %q", out.String())
	}
	if strings.Contains(errOut.String(), "upgraded") {
		t.Errorf("stderr received a Success line: %q", errOut.String())
	}
}

// TestPlainWriter_AddsNewlines pins the trailing-newline contract.
// Plain output goes line-by-line so a downstream `nodeup ... | jq`
// (or just a `nodeup version | head`) sees one record per line.
func TestPlainWriter_AddsNewlines(t *testing.T) {
	var out bytes.Buffer
	w := NewWriter(PlainMode, &out, nil)
	w.Info("hello")
	if got := out.String(); got != "hello\n" {
		t.Errorf("got %q, want %q", got, "hello\n")
	}
}

// TestFancyWriter_EmitsANSI pins that FancyMode writes ANSI escape
// sequences for the colored prefixes (✓ / • / ⚠ / ✗). This is the
// minimum-viable proof that the lipgloss styles are wired in.
func TestFancyWriter_EmitsANSI(t *testing.T) {
	var out bytes.Buffer
	w := NewWriter(FancyMode, &out, &out)
	w.Success("upgraded")
	w.Info("using fnm")
	w.Warn("skipped")
	w.Error("failed")

	got := out.String()
	if !strings.Contains(got, "\x1b[") {
		t.Errorf("fancy output missing ANSI escape sequences: %q", got)
	}
	for _, prefix := range []string{"✓", "•", "⚠", "✗"} {
		if !strings.Contains(got, prefix) {
			t.Errorf("fancy output missing glyph %q: %q", prefix, got)
		}
	}
}

// TestDecideMode_PinTTYDetection pins the rules documented on
// DecideMode: NO_COLOR → PlainMode, otherwise the test runner's
// stdio (which in `go test` is a pipe) wins. We can't easily fake
// a TTY from inside `go test`, so this test just checks the env-var
// branch (the dominant case in CI / piped scripts).
func TestDecideMode_PinTTYDetection(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	if got := DecideMode(false); got != PlainMode {
		t.Errorf("NO_COLOR=1 should force PlainMode, got %v", got)
	}

	// Without NO_COLOR, the answer depends on whether stdout is a
	// TTY — under `go test` it isn't, so PlainMode is the expected
	// outcome. The point of the test is that noColor=true always
	// wins (user override beats env, beats detection).
	if got := DecideMode(true); got != PlainMode {
		t.Errorf("noColor=true should force PlainMode, got %v", got)
	}
}

// TestMode_String pins the debug-string rendering. Cosmetic, but
// used in --verbose logs.
func TestMode_String(t *testing.T) {
	if PlainMode.String() != "plain" {
		t.Errorf("PlainMode.String() = %q, want %q", PlainMode.String(), "plain")
	}
	if FancyMode.String() != "fancy" {
		t.Errorf("FancyMode.String() = %q, want %q", FancyMode.String(), "fancy")
	}
}
