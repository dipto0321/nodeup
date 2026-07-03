package ui

import (
	"io"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// Writer is the interface every cli/*.go command talks to instead
// of touching cmd.Printf / fmt.Fprintf directly. The two
// implementations are PlainWriter (no ANSI, no spinners, no
// interactive prompts) and FancyWriter (lipgloss-styled output;
// spinners + prompts added in follow-up PRs per #74's phasing).
//
// Construction:
//
//	ui.NewWriter(ui.DecideMode(noColor), os.Stdout, os.Stderr)
//
// Then call sites do:
//
//	ui.Out(w).Success("Upgraded to v22.11.0")
//	ui.Out(w).Info("Using manager: fnm")
//	ui.Out(w).Warn("cleanup skipped")
//	ui.Out(w).Error("snapshot failed: %w")
//
// The out/err writers are passed at construction time (not
// hard-coded to os.Stdout/Stderr) so tests can inject bytes.Buffers.
type Writer interface {
	// Success prints a positive confirmation to stdout.
	Success(msg string)
	// Info prints an informational line to stdout.
	Info(msg string)
	// Warn prints a non-fatal warning to stderr.
	Warn(msg string)
	// Error prints an error line to stderr.
	Error(msg string)
	// Println writes a plain line to stdout with no decoration.
	// For machine-readable output (JSON envelopes, table rows
	// whose format is part of the API).
	Println(msg string)
	// Mode returns the writer's render mode (PlainMode /
	// FancyMode). Useful for tests asserting on the decision.
	Mode() Mode
}

// NewWriter returns the right Writer for the given Mode.
//
// In PlainMode the underlying Writer wraps the supplied io.Writer
// directly — every Success / Info / Println becomes a single
// fmt.Fprintln with no ANSI codes. Error / Warn route to errOut
// (also no decoration).
//
// In FancyMode the Writer applies lipgloss styles from
// DefaultTheme() to the same io.Writers. Spinners and interactive
// prompts are wired in by follow-up PRs (#74 phasing).
func NewWriter(mode Mode, out, errOut io.Writer) Writer {
	if mode == FancyMode {
		// Force TrueColor so ANSI escapes are emitted regardless of
		// what lipgloss's own TTY detection thinks. The whole point
		// of FancyMode is "the user wants color" — by the time
		// DecideMode returned FancyMode we've already confirmed both
		// stdout and stdin are TTYs. Re-probing here would only ever
		// downgrade to Ascii in piped unit-test scenarios where the
		// test is explicitly checking that the escapes get emitted.
		lipgloss.SetColorProfile(termenv.TrueColor)
		return &fancyWriter{out: out, errOut: errOut, theme: DefaultTheme()}
	}
	return &plainWriter{out: out, errOut: errOut}
}

// plainWriter is the PlainMode implementation. Every method writes
// a single line to the appropriate sink with no styling.
type plainWriter struct {
	out    io.Writer
	errOut io.Writer
}

func (p *plainWriter) Success(msg string) { _, _ = io.WriteString(p.out, msg+"\n") }
func (p *plainWriter) Info(msg string)    { _, _ = io.WriteString(p.out, msg+"\n") }
func (p *plainWriter) Warn(msg string)    { _, _ = io.WriteString(p.errOut, msg+"\n") }
func (p *plainWriter) Error(msg string)   { _, _ = io.WriteString(p.errOut, msg+"\n") }
func (p *plainWriter) Println(msg string) { _, _ = io.WriteString(p.out, msg+"\n") }
func (p *plainWriter) Mode() Mode         { return PlainMode }

// fancyWriter is the FancyMode implementation. Each method
// applies a lipgloss style from the theme. Spinner / prompt
// methods are added in follow-up PRs per #74's phasing; this PR
// just establishes the Writer surface so cli/*.go has one
// well-defined dependency to migrate to.
type fancyWriter struct {
	out    io.Writer
	errOut io.Writer
	theme  Theme
}

func (f *fancyWriter) Success(msg string) {
	_, _ = io.WriteString(f.out, f.theme.Success.Render("✓ "+msg)+"\n")
}
func (f *fancyWriter) Info(msg string) {
	_, _ = io.WriteString(f.out, f.theme.Info.Render("• "+msg)+"\n")
}
func (f *fancyWriter) Warn(msg string) {
	_, _ = io.WriteString(f.errOut, f.theme.Warning.Render("⚠ "+msg)+"\n")
}
func (f *fancyWriter) Error(msg string) {
	_, _ = io.WriteString(f.errOut, f.theme.Error.Render("✗ "+msg)+"\n")
}
func (f *fancyWriter) Println(msg string) { _, _ = io.WriteString(f.out, msg+"\n") }
func (f *fancyWriter) Mode() Mode         { return FancyMode }
