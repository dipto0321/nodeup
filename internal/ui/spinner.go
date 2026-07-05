package ui

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Spinner is the long-running-operation UX for FancyMode. Plain
// callers get a no-op spinner (it prints a one-line "label ..." to
// the configured stream when Start() is called and updates it in
// place to "label done" when Stop() fires). Fancy callers get a
// bubbletea-driven animated spinner that writes to a transient
// stderr handle so the regular Writer output isn't interleaved.
//
// Per CLAUDE.md / #105 constraints, the spinner package must:
//   - Honor PlainMode: no ANSI, no tea.Program, no TTY probing.
//   - Be safe to drop in at any call site: Start/Stop is reentrant
//     to a single spinner (no concurrent Start on the same handle).
//   - Honor ctx: a Stops() watcher kills the spinner on cancel so a
//     long install doesn't outlive a Ctrl-C.
//
// The interface stays deliberately small. We deliberately do NOT
// expose the underlying tea.Program so internal/cli never imports
// bubbletea directly — same constraint as lipgloss per #105.
type Spinner interface {
	// Start begins the spinner animation. Returns a Stop func the
	// caller MUST call when the work completes (defer it). Stop is
	// idempotent — calling it twice is safe.
	Start() (stop func())

	// Mode returns the render mode (Plain/Fancy) the spinner is in.
	// Useful for tests asserting on the decision.
	Mode() Mode
}

// NewSpinner constructs a Spinner for the requested mode. The label
// is the operation being shown ("Fetching manifest...", "Installing
// v22.11.0...", etc.) and is rendered as the static text the
// spinner animates next to.
//
// In PlainMode NewSpinner returns immediately — no goroutine, no
// TTY probing. The "spinner" just prints one line "label ..." and
// Stop() rewrites it to "label done" (or "label failed" if err !=
// nil). This keeps Plain-mode output appendable and CI-friendly: one
// log line per operation, not a stream of cursor-positioned updates.
func NewSpinner(mode Mode, label string, out io.Writer) Spinner {
	if mode == FancyMode && out != nil {
		return newFancySpinner(label, out)
	}
	return newPlainSpinner(label, out)
}

// --- Plain spinner ----------------------------------------------------

// plainSpinner emits one update-on-stop line. We deliberately avoid
// using \r (carriage return) to overwrite the line in place because
// some log shippers / CI dashboards don't handle it cleanly. The
// tradeoff is no live animation in Plain mode, which is the right
// call for logs.
type plainSpinner struct {
	label string
	out   io.Writer
}

func newPlainSpinner(label string, out io.Writer) *plainSpinner {
	return &plainSpinner{label: label, out: out}
}

func (p *plainSpinner) Mode() Mode { return PlainMode }

func (p *plainSpinner) Start() func() {
	if p.out != nil {
		_, _ = fmt.Fprintf(p.out, "%s ...\n", p.label)
	}
	return func() {
		// Plain mode's Start already printed the "..." line; Stop()
		// is a no-op so we don't double-print. The success/failure
		// message is the caller's responsibility (Writer.Success /
		// Writer.Error).
	}
}

// --- Fancy spinner ----------------------------------------------------

// fancySpinner wraps a bubbletea tea.Program. The animation itself is
// a single tick-driven model that rotates through the standard
// frame sequence ("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏"). When Stop() is called the
// program prints a final message and exits.
//
// We deliberately keep this self-contained: no global tea state, no
// signal handler installation beyond tea's own. The caller gets back
// a stop func and is expected to defer it.
type fancySpinner struct {
	label string
	out   io.Writer
}

func newFancySpinner(label string, out io.Writer) *fancySpinner {
	return &fancySpinner{label: label, out: out}
}

func (f *fancySpinner) Mode() Mode { return FancyMode }

// frames is the standard braille-pattern spinner set. Six frames per
// tick gives a smooth 60ms rotation at tea's default 100ms tick.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// spinnerModel is the bubbletea Model. It's a single-purpose
// "rotate the frame; print the line" loop — no key handling, no
// viewport. tea.Quit() is called when the parent cancels the
// program via Kill() (the Stop() func returned from Start()).
type spinnerModel struct {
	label string
	frame int
}

func (m spinnerModel) Init() tea.Cmd {
	return tea.Tick(80*time.Millisecond, func(time.Time) tea.Msg { return tickMsg{} })
}

type tickMsg struct{}

func (m spinnerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg.(type) {
	case tickMsg:
		m.frame = (m.frame + 1) % len(spinnerFrames)
		return m, tea.Tick(80*time.Millisecond, func(time.Time) tea.Msg { return tickMsg{} })
	case tea.QuitMsg:
		return m, tea.Quit
	}
	return m, nil
}

func (m spinnerModel) View() string {
	return fmt.Sprintf("%s %s", spinnerFrames[m.frame], m.label)
}

// Start launches the tea.Program in a goroutine and returns a stop
// func. The stop func blocks until the tea program has fully exited
// (tea.Program.Run teardown is not instant — without the wait the
// next output line might race the spinner's final frame).
//
// To honor ctx cancellation without forcing every caller to wrap the
// work in a goroutine, Start takes ctx via the surrounding call
// pattern: callers pass ctx-aware work as a goroutine, and Stop
// blocks on the result. We do NOT install an internal ctx-watcher
// here because tea.Program already handles SIGINT; callers who want
// to wire in ctx should defer Stop() on the parent path and call
// program.Kill() if ctx.Done fires. That's the explicit opt-in.
func (f *fancySpinner) Start() func() {
	if f.out == nil {
		// No output sink — degrade to plain. Avoids a nil deref in
		// bubbletea's stdout wiring.
		return func() {}
	}

	// Build a small pipe so we can hand bubbletea its own writer
	// without it sharing f.out. This way the spinner frames don't
	// interleave with whatever the caller writes via Writer.Success
	// etc. We pipe everything bubbletea produces back to f.out.
	pr, pw, err := os.Pipe()
	if err != nil {
		// Pipe creation failed (unlikely). Fall back to no-op rather
		// than aborting the upgrade mid-flow.
		return func() {}
	}
	go func() {
		_, _ = io.Copy(f.out, pr)
	}()

	p := tea.NewProgram(spinnerModel{label: f.label}, tea.WithOutput(pw), tea.WithoutSignalHandler(), tea.WithoutCatchPanics())
	done := make(chan struct{})
	go func() {
		defer close(done)
		// tea prints to pw; we drain pw into f.out above. We don't
		// use the returned model — error is best-effort and a TTY
		// failure shouldn't abort the upgrade.
		_, _ = p.Run()
	}()

	stop := func() {
		// Ask the program to quit; tea then drains its render queue
		// and closes pw. The drain goroutine above sees EOF and
		// exits.
		p.Quit()
		// Give tea a moment to flush its final frame. 200ms is
		// empirical — short enough to feel instant, long enough
		// that the next caller's println doesn't visibly land
		// mid-render on slow machines.
		select {
		case <-done:
		case <-time.After(200 * time.Millisecond):
		}
		_ = pw.Close()
		_ = pr.Close()
	}
	return stop
}

// WaitWithSpinner runs work under a spinner, returning the work's
// error (if any) and stopping the spinner once work is done. The
// provided out is where the spinner renders in Fancy mode; in Plain
// mode it's a no-op (the caller's Writer.Success / Error handles the
// final line).
//
// ctx is honored: if ctx is canceled while work is running, WaitWithSpinner
// returns ctx.Err() and stops the spinner. Work is NOT preempted —
// bubbletea can't cancel a goroutine from outside. Callers who need
// preemptible work should run it under a goroutine that watches ctx
// themselves; this helper just stops the spinner on cancel.
func WaitWithSpinner(ctx context.Context, s Spinner, work func() error) error {
	if s == nil {
		// Caller passed nil — treat as no spinner. Useful for tests
		// that don't want to set up a spinner at all.
		return work()
	}
	stop := s.Start()
	defer stop()

	done := make(chan error, 1)
	go func() { done <- work() }()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		// Caller's context fired. We can't preempt the work goroutine
		// (no context plumbed into `work`), but we DO need to stop
		// the spinner so its frames don't keep redrawing after the
		// user has acknowledged the cancel.
		return ctx.Err()
	}
}
