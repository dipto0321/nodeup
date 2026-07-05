package ui

import (
	"errors"
	"fmt"
	"io"

	"github.com/charmbracelet/huh"
)

// Prompt is the surface for interactive user prompts. The two
// implementations are PlainPrompt (no huh, no ANSI — backs onto
// bufio on the supplied reader) and FancyPrompt (huh.Form with
// lipgloss styling).
//
// Per CLAUDE.md / #105 constraints:
//   - Honor PlainMode: no huh.Form, no ANSI, no TTY probing.
//   - internal/cli never imports huh directly — only this package.
//   - Prompts MUST degrade to a sane default on EOF (closed stdin),
//     so a piped `echo y | nodeup upgrade` works.
//
// Construction:
//
//	ui.NewPrompt(ui.DecideMode(noColor), in, out)
//
// Callers request a prompt through the methods (Confirm / Select),
// not by constructing the underlying huh.Form. That keeps the
// call sites mode-agnostic: the same code path renders differently
// in PlainMode vs FancyMode.
type Prompt interface {
	// Confirm asks the user a yes/no question. Returns true on
	// affirmative ("y" / "yes" / "Y" / "YES"); false on empty input,
	// "n", or anything else. EOF (closed stdin) returns (false, nil)
	// so a piped script with no input defaults to "no" — the safe
	// direction for cleanup / overwrite-style prompts.
	Confirm(question string, defaultYes bool) (bool, error)

	// Select asks the user to pick one option from a list. Returns
	// the selected option's label. EOF returns an error so the caller
	// can decide (most call sites should treat EOF as "use the first
	// option" or "skip the operation").
	Select(prompt string, options []string, defaultLabel string) (string, error)

	// Mode returns the prompt's render mode.
	Mode() Mode
}

// NewPrompt constructs a Prompt for the requested mode. PlainMode
// returns a PlainPrompt backed by `in` (a *bufio.Reader-shaped
// source — accepts any io.Reader but stores the concrete interface)
// and `out`; FancyMode returns a FancyPrompt that builds a huh.Form
// per call.
//
// We deliberately don't try to detect "huh ran but bubbletea can't
// initialize a real renderer" here — DecideMode already gated that.
// If DecideMode returned FancyMode, both stdout and stdin are TTYs,
// so huh's renderer will produce real output.
func NewPrompt(mode Mode, in io.Reader, out io.Writer) Prompt {
	if mode == FancyMode && in != nil && out != nil {
		return &fancyPrompt{in: in, out: out}
	}
	// Plain mode (or any nil stream in fancy mode — degrade to plain).
	return &plainPrompt{in: in, out: out}
}

// --- Plain prompt ------------------------------------------------------

// plainPrompt implements Prompt using simple line reads against `in`
// and writes prompts to `out`. The question is rendered as
// "<question> [Y/n] " or "<question> [y/N] " depending on defaultYes,
// and the answer is matched case-insensitively against "y" / "yes"
// (affirmative) or "n" / "no" / "" (negative, when defaultYes is true).
//
// EOF (closed stdin) is treated as the default answer so piped
// `echo "" | nodeup upgrade` keeps working without an explicit
// fallback in every call site.
type plainPrompt struct {
	in  io.Reader
	out io.Writer
}

func (p *plainPrompt) Mode() Mode { return PlainMode }

// readLine is a small EOF-tolerant line reader. We use bufio.Scanner
// under the hood — the input is always one human-typed line so the
// 64KiB token-length cap is irrelevant.
func (p *plainPrompt) readLine() (line string, ok bool, err error) {
	// Wrap `in` lazily so callers that pass a *bufio.Reader
	// directly don't double-buffer. io.Reader is the public surface
	// so callers don't have to know.
	type lineReader interface {
		ReadString(delim byte) (string, error)
	}
	type scanner interface {
		Scan() bool
		Text() string
		Err() error
	}
	switch r := p.in.(type) {
	case lineReader:
		s, rerr := r.ReadString('\n')
		if rerr != nil && rerr != io.EOF {
			return "", false, rerr
		}
		return trimTrailingNewline(s), true, nil
	case scanner:
		if !r.Scan() {
			if r.Err() != nil {
				return "", false, r.Err()
			}
			return "", false, nil // EOF cleanly
		}
		return r.Text(), true, nil
	default:
		// Fallback: one byte at a time. Avoids pulling in bufio
		// for callers that already wrapped their own reader.
		var out []byte
		buf := make([]byte, 1)
		for {
			n, rerr := r.Read(buf)
			if n > 0 {
				if buf[0] == '\n' {
					return trimTrailingNewline(string(out)), true, nil
				}
				out = append(out, buf[0])
			}
			if rerr != nil {
				if rerr == io.EOF {
					return trimTrailingNewline(string(out)), true, nil
				}
				return "", false, rerr
			}
		}
	}
}

func trimTrailingNewline(s string) string {
	for s != "" && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}

func (p *plainPrompt) Confirm(question string, defaultYes bool) (bool, error) {
	if p.out != nil {
		if defaultYes {
			_, _ = fmt.Fprintf(p.out, "%s [Y/n] ", question)
		} else {
			_, _ = fmt.Fprintf(p.out, "%s [y/N] ", question)
		}
	}
	line, ok, err := p.readLine()
	if err != nil {
		return false, err
	}
	if !ok {
		// EOF — fall back to default. This is the safe direction
		// for confirm-style prompts: a piped `echo "" | nodeup`
		// without -y should NOT mass-delete.
		return defaultYes, nil
	}
	switch normalizeAffirmative(line) {
	case "y", "yes":
		return true, nil
	case "n", "no":
		return false, nil
	default:
		return defaultYes, nil
	}
}

func (p *plainPrompt) Select(prompt string, options []string, defaultLabel string) (string, error) {
	if len(options) == 0 {
		return "", errors.New("ui: Select called with no options")
	}
	if p.out != nil {
		_, _ = fmt.Fprintf(p.out, "%s\n", prompt)
		for i, o := range options {
			if o == defaultLabel {
				_, _ = fmt.Fprintf(p.out, "  %d) %s [default]\n", i+1, o)
			} else {
				_, _ = fmt.Fprintf(p.out, "  %d) %s\n", i+1, o)
			}
		}
	}
	line, ok, err := p.readLine()
	if err != nil {
		return "", err
	}
	if !ok || line == "" {
		// EOF or empty input → use default. If defaultLabel is empty,
		// fall back to options[0].
		if defaultLabel != "" {
			return defaultLabel, nil
		}
		return options[0], nil
	}
	// Try numeric selection first ("1", "2", ...).
	var idx int
	if _, perr := fmt.Sscanf(line, "%d", &idx); perr == nil {
		if idx >= 1 && idx <= len(options) {
			return options[idx-1], nil
		}
	}
	// Otherwise, accept exact label match (case-insensitive).
	for _, o := range options {
		if normalizeAffirmative(o) == normalizeAffirmative(line) {
			return o, nil
		}
	}
	// Unrecognized — fall back to default (or options[0]).
	if defaultLabel != "" {
		return defaultLabel, nil
	}
	return options[0], nil
}

// normalizeAffirmative lowercases ASCII letters and trims spaces
// without dragging in `strings`. Used by both Confirm and Select
// for case-insensitive input matching.
func normalizeAffirmative(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		out = append(out, c)
	}
	start, end := 0, len(out)
	for start < end && (out[start] == ' ' || out[start] == '\t') {
		start++
	}
	for end > start && (out[end-1] == ' ' || out[end-1] == '\t') {
		end--
	}
	return string(out[start:end])
}

// --- Fancy prompt ------------------------------------------------------

// fancyPrompt implements Prompt using huh.Form. We construct the
// form per-call (rather than caching) so each prompt is a fresh
// bubbletea program — this matches the lifecycle of a single question
// and avoids leaking state across prompts.
type fancyPrompt struct {
	in  io.Reader
	out io.Writer
}

func (p *fancyPrompt) Mode() Mode { return FancyMode }

func (p *fancyPrompt) Confirm(question string, defaultYes bool) (bool, error) {
	// Pre-fill the value so huh's Confirm starts on the right side.
	// If the user accepts the default (presses Enter), `got` keeps
	// this value and we return it. Otherwise huh updates `got` based
	// on what the user toggled.
	got := defaultYes
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title(question).
				Value(&got),
		),
	).WithInput(p.in).WithOutput(p.out)
	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return defaultYes, nil
		}
		return false, fmt.Errorf("prompt: %w", err)
	}
	return got, nil
}

func (p *fancyPrompt) Select(prompt string, options []string, defaultLabel string) (string, error) {
	if len(options) == 0 {
		return "", errors.New("ui: Select called with no options")
	}
	huhOptions := make([]huh.Option[string], len(options))
	for i, o := range options {
		huhOptions[i] = huh.NewOption(o, o)
	}
	var got string
	if defaultLabel != "" {
		got = defaultLabel
	} else {
		got = options[0]
	}
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title(prompt).
				Options(huhOptions...).
				Value(&got),
		),
	).WithInput(p.in).WithOutput(p.out)
	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			if defaultLabel != "" {
				return defaultLabel, nil
			}
			return options[0], nil
		}
		return "", fmt.Errorf("prompt: %w", err)
	}
	return got, nil
}
