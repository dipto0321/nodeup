package ui

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

// TestPlainPrompt_Confirm_Y answers "y" → true. Pins the
// happy-path affirmative parsing.
func TestPlainPrompt_Confirm_Y(t *testing.T) {
	p := NewPrompt(PlainMode, strings.NewReader("y\n"), &bytes.Buffer{})
	got, err := p.Confirm("Delete?", false)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !got {
		t.Errorf("Confirm(\"y\") = false, want true")
	}
}

// TestPlainPrompt_Confirm_N answers "n" → false.
func TestPlainPrompt_Confirm_N(t *testing.T) {
	p := NewPrompt(PlainMode, strings.NewReader("n\n"), &bytes.Buffer{})
	got, err := p.Confirm("Delete?", true)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got {
		t.Errorf("Confirm(\"n\") = true, want false")
	}
}

// TestPlainPrompt_Confirm_EmptyUsesDefault pins that empty input
// falls back to defaultYes. This is the EOF-equivalent path that
// piped `echo "" | nodeup upgrade` exercises.
func TestPlainPrompt_Confirm_EmptyUsesDefault(t *testing.T) {
	t.Run("default true", func(t *testing.T) {
		p := NewPrompt(PlainMode, strings.NewReader("\n"), &bytes.Buffer{})
		got, err := p.Confirm("Delete?", true)
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if !got {
			t.Errorf("empty input + defaultYes=true → false, want true")
		}
	})
	t.Run("default false", func(t *testing.T) {
		p := NewPrompt(PlainMode, strings.NewReader("\n"), &bytes.Buffer{})
		got, err := p.Confirm("Delete?", false)
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if got {
			t.Errorf("empty input + defaultYes=false → true, want false")
		}
	})
}

// TestPlainPrompt_Confirm_EOF pins that a closed stdin (no input
// at all) returns the default answer with no error. This is the
// critical "piped script that ran out of input" path: nodeup
// must not block on the prompt and must not return an error that
// would crash the upgrade flow.
func TestPlainPrompt_Confirm_EOF(t *testing.T) {
	p := NewPrompt(PlainMode, strings.NewReader(""), &bytes.Buffer{})
	got, err := p.Confirm("Delete?", false)
	if err != nil {
		t.Errorf("EOF Confirm err = %v, want nil", err)
	}
	if got {
		t.Errorf("EOF Confirm → true, want default (false)")
	}
}

// TestPlainPrompt_Confirm_YesVariants pins case-insensitive
// parsing: "Y", "YES", "yEs" should all be affirmative. Critical
// for muscle-memory users.
func TestPlainPrompt_Confirm_YesVariants(t *testing.T) {
	for _, in := range []string{"Y\n", "yes\n", "YES\n", "yEs\n"} {
		t.Run(in, func(t *testing.T) {
			p := NewPrompt(PlainMode, strings.NewReader(in), &bytes.Buffer{})
			got, err := p.Confirm("?", false)
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if !got {
				t.Errorf("Confirm(%q) = false, want true", in)
			}
		})
	}
}

// TestPlainPrompt_Confirm_RendersHint pins that the prompt prints
// a "[y/N]"-style hint so users know which default applies. The
// exact wording varies by default; we just check both branches
// have the right marker.
func TestPlainPrompt_Confirm_RendersHint(t *testing.T) {
	t.Run("default no", func(t *testing.T) {
		var out bytes.Buffer
		p := NewPrompt(PlainMode, strings.NewReader("\n"), &out)
		_, _ = p.Confirm("Delete?", false)
		if !strings.Contains(out.String(), "[y/N]") {
			t.Errorf("defaultNo hint missing, got %q", out.String())
		}
	})
	t.Run("default yes", func(t *testing.T) {
		var out bytes.Buffer
		p := NewPrompt(PlainMode, strings.NewReader("\n"), &out)
		_, _ = p.Confirm("Delete?", true)
		if !strings.Contains(out.String(), "[Y/n]") {
			t.Errorf("defaultYes hint missing, got %q", out.String())
		}
	})
}

// TestPlainPrompt_Select_Numeric covers the dominant path: the
// user types a number from the printed list.
func TestPlainPrompt_Select_Numeric(t *testing.T) {
	p := NewPrompt(PlainMode, strings.NewReader("2\n"), &bytes.Buffer{})
	got, err := p.Select("Pick one:", []string{"fnm", "nvm", "volta"}, "fnm")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got != "nvm" {
		t.Errorf("Select(\"2\") = %q, want \"nvm\"", got)
	}
}

// TestPlainPrompt_Select_EmptyUsesDefault pins that empty input
// falls back to defaultLabel. EOF equivalent.
func TestPlainPrompt_Select_EmptyUsesDefault(t *testing.T) {
	t.Run("with default", func(t *testing.T) {
		p := NewPrompt(PlainMode, strings.NewReader("\n"), &bytes.Buffer{})
		got, err := p.Select("?", []string{"a", "b"}, "b")
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if got != "b" {
			t.Errorf("empty input → %q, want default \"b\"", got)
		}
	})
	t.Run("no default → first option", func(t *testing.T) {
		p := NewPrompt(PlainMode, strings.NewReader("\n"), &bytes.Buffer{})
		got, err := p.Select("?", []string{"a", "b"}, "")
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if got != "a" {
			t.Errorf("empty input + no default → %q, want first option \"a\"", got)
		}
	})
}

// TestPlainPrompt_Select_LabelMatch covers typing the label text
// directly rather than the numeric index.
func TestPlainPrompt_Select_LabelMatch(t *testing.T) {
	p := NewPrompt(PlainMode, strings.NewReader("volta\n"), &bytes.Buffer{})
	got, err := p.Select("?", []string{"fnm", "nvm", "volta"}, "fnm")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got != "volta" {
		t.Errorf("Select(\"volta\") = %q, want \"volta\"", got)
	}
}

// TestPlainPrompt_Select_EOF pins that closed stdin returns the
// default (or first option) — same contract as Confirm.EOF.
func TestPlainPrompt_Select_EOF(t *testing.T) {
	t.Run("with default", func(t *testing.T) {
		p := NewPrompt(PlainMode, strings.NewReader(""), &bytes.Buffer{})
		got, err := p.Select("?", []string{"a", "b"}, "b")
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if got != "b" {
			t.Errorf("EOF Select → %q, want default \"b\"", got)
		}
	})
}

// TestPlainPrompt_Select_NoOptions pins that calling Select with
// no options returns an error rather than panicking. Defensive —
// callers should pre-validate but we don't want a panic in
// production if a future code path forgets.
func TestPlainPrompt_Select_NoOptions(t *testing.T) {
	p := NewPrompt(PlainMode, strings.NewReader("\n"), &bytes.Buffer{})
	_, err := p.Select("?", nil, "")
	if err == nil {
		t.Errorf("Select(nil options) → nil err, want error")
	}
}

// TestPlainPrompt_Confirm_BufioReader pins that a *bufio.Reader
// (the typical cmd.InOrStdin() wrapping) is accepted without
// double-buffering. This is the path upgrade.go's old
// `bufio.NewReader(cmd.InOrStdin())` takes.
func TestPlainPrompt_Confirm_BufioReader(t *testing.T) {
	in := bufio.NewReader(strings.NewReader("y\n"))
	p := NewPrompt(PlainMode, in, &bytes.Buffer{})
	got, err := p.Confirm("?", false)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !got {
		t.Errorf("bufio reader path: got %v, want true", got)
	}
}

// TestNewPrompt_FancyNilInDegradesToPlain pins that FancyMode +
// nil stream degrades to PlainMode. Same defensive branch as
// the spinner.
func TestNewPrompt_FancyNilInDegradesToPlain(t *testing.T) {
	p := NewPrompt(FancyMode, nil, nil)
	if p == nil {
		t.Fatalf("NewPrompt returned nil for (FancyMode, nil, nil)")
	}
	if p.Mode() != PlainMode {
		t.Errorf("nil streams under FancyMode → Mode() = %v, want PlainMode", p.Mode())
	}
}

// errReader is a reader that always returns an error. Used to
// pin that Confirm/Select surface I/O errors rather than silently
// returning the default.
type errReader struct{ err error }

func (e errReader) Read([]byte) (int, error) { return 0, e.err }

// TestPlainPrompt_Confirm_ReaderErrorSurfaces pins that I/O
// errors from the underlying reader are propagated to the caller.
// Without this, a broken pipe on stdin would silently default to
// "no", masking real errors.
func TestPlainPrompt_Confirm_ReaderErrorSurfaces(t *testing.T) {
	wantErr := errors.New("broken pipe")
	p := NewPrompt(PlainMode, errReader{err: wantErr}, &bytes.Buffer{})
	_, err := p.Confirm("?", false)
	if !errors.Is(err, wantErr) {
		t.Errorf("Confirm err = %v, want %v", err, wantErr)
	}
}

// io.Discard writer doesn't error; we just confirm Confirm works
// with a discarded-output sink (some tests might pass it).
func TestPlainPrompt_Confirm_NilOutSafe(t *testing.T) {
	p := NewPrompt(PlainMode, strings.NewReader("y\n"), io.Discard)
	got, err := p.Confirm("?", false)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !got {
		t.Errorf("Confirm with io.Discard: got false, want true")
	}
}
