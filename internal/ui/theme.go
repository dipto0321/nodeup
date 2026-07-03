package ui

import "github.com/charmbracelet/lipgloss"

// Theme is the shared palette + style definitions used by the
// FancyMode renderer. Constructed once per Writer; cheap to pass
// by value (lipgloss.Style is itself a value type).
//
// The colors here are deliberately muted — nodeup output is
// informational, not decorative. The accent colors (success green,
// error red, warning amber) are AA-readable against the standard
// dark terminal background. Light-background terminals fall back
// gracefully (lipgloss auto-dims).
type Theme struct {
	// Success styles a positive confirmation ("✓ upgraded to v22").
	Success lipgloss.Style
	// Error styles an error message header.
	Error lipgloss.Style
	// Warning styles a non-fatal warning ("⚠ cleanup skipped").
	Warning lipgloss.Style
	// Info styles an informational prefix ("Using manager: fnm").
	Info lipgloss.Style
	// Dim is for secondary text (timestamps, file paths in
	// machine-readable contexts).
	Dim lipgloss.Style
	// Heading styles section headings in the final report.
	Heading lipgloss.Style
}

// DefaultTheme returns the standard nodeup theme. Future PRs
// (per #74's phasing: 4. report.go + remaining migrations) can
// swap this for a `--theme=dark|light|...` flag without touching
// any call-site.
func DefaultTheme() Theme {
	return Theme{
		Success: lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true),
		Error:   lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true),
		Warning: lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true),
		Info:    lipgloss.NewStyle().Foreground(lipgloss.Color("39")),
		Dim:     lipgloss.NewStyle().Foreground(lipgloss.Color("245")),
		Heading: lipgloss.NewStyle().Foreground(lipgloss.Color("63")).Bold(true).Underline(true),
	}
}
