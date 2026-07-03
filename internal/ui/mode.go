// Package ui is the single source of truth for user-facing output
// in nodeup. Every cli/*.go command talks to internal/ui's Writer
// interface instead of touching cmd.Printf / fmt.Fprintf directly.
// This keeps the Charm stack dependencies contained to one package
// and makes the plain-vs-fancy switch a single decision point.
//
// The architectural rule (CLAUDE.md, "Output routing"): nothing in
// internal/ or cmd/ prints directly. Violation = anything that
// calls fmt.Fprintln / cmd.Printf outside this package.
package ui

import (
	"os"
)

// Mode describes how the UI layer renders. CLI commands call
// ui.Plain() / ui.Fancy() to construct the right Writer; the rest
// of the codebase never branches on Mode itself.
type Mode int

const (
	// PlainMode emits undecorated text with no ANSI codes. Used
	// when stdout isn't a real terminal (piped, redirected, CI)
	// or when --json / --yes was passed.
	PlainMode Mode = iota
	// FancyMode uses the full Charm stack (lipgloss + bubbletea +
	// huh) for colored output, spinners, and interactive prompts.
	FancyMode
)

// DecideMode is the single decision point for plain-vs-fancy. The
// rule, in order: if NO_COLOR env var is set OR the user passed
// --no-color OR stdout isn't a TTY OR stdin isn't a TTY (interactive
// prompts would deadlock), fall back to PlainMode. Otherwise
// FancyMode.
//
// The --no-color flag is plumbed through from cobra in root.go's
// NewRootCmd and read here via the noColor parameter.
func DecideMode(noColor bool) Mode {
	if noColor {
		return PlainMode
	}
	if os.Getenv("NO_COLOR") != "" {
		return PlainMode
	}
	if !isTerminal(os.Stdout) || !isTerminal(os.Stdin) {
		return PlainMode
	}
	return FancyMode
}

// isTerminal reports whether the given file descriptor is connected
// to a real terminal (vs a pipe / redirect / file). Used by
// DecideMode; centralizing it here means lipgloss's color profile
// detection and our TTY gating never disagree.
func isTerminal(f *os.File) bool {
	// lipgloss itself does its own TTY detection, but we need the
	// answer earlier (before the Writer is constructed) and for
	// stdin (which lipgloss doesn't check). A simple os.Stat on
	// the device-mode bit is portable across mac/linux/windows.
	stat, err := f.Stat()
	if err != nil {
		return false
	}
	return (stat.Mode() & os.ModeCharDevice) != 0
}

// String renders Mode for debugging (e.g., --verbose logs).
func (m Mode) String() string {
	switch m {
	case FancyMode:
		return "fancy"
	default:
		return "plain"
	}
}
