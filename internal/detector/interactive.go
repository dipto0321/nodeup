package detector

import (
	"errors"
	"fmt"
	"io"
	"sort"

	"github.com/dipto0321/nodeup/internal/ui"
)

// ErrInteractiveRequired is returned when ResolveManager can't
// pick a manager on its own (multiple detected, no preference
// given). The caller should invoke ResolveInteractive next, or
// surface a hint to the user.
//
// We export this so the CLI layer can decide whether to call
// ResolveInteractive (real terminal + flags allowed interactive)
// or to error out with a "use --manager" message (CI / --yes /
// --json).
var ErrInteractiveRequired = errors.New("interactive manager selection required")

// ResolveManagerAuto is a thin wrapper around ResolveManager that
// returns ErrInteractiveRequired (with a "use --manager" hint
// already in the error message) when ResolveManager would
// otherwise error with a multi-manager hint. Callers that want
// the auto-prompt path invoke ResolveInteractive directly instead
// of inspecting ResolveManager's error.
//
// This exists so the CLI layer has a single decision point
// ("can I prompt? if so, use ResolveInteractive; if not, surface
// the error from ResolveManager") — see upgrade.go and check.go.
func ResolveManagerAuto(reg Registry, preferred string) (Manager, error) {
	m, err := ResolveManager(reg, preferred)
	if err != nil {
		// ResolveManager's multi-manager error always includes
		// "Use --manager <name> or `nodeup config set manager`..."
		// so we don't need to rewrap — just promote it.
		if preferred == "" && len(reg.Found) > 1 {
			return nil, fmt.Errorf("%w (multiple managers detected: %s)",
				ErrInteractiveRequired, managerNames(reg.Found))
		}
		return nil, err
	}
	return m, nil
}

// ResolveInteractive prompts the user to pick one of the
// detected managers when no `--manager` flag or config-file
// preference is set. It uses the ui.Prompt abstraction so:
//
//   - FancyMode: huh.Select renders a styled list with arrow-key
//     navigation.
//   - PlainMode: a numbered list rendered to `out` with a
//     single-line read from `in`.
//
// Parameters:
//   - reg          — Registry from DetectAll.
//   - p            — ui.Prompt (already mode-resolved by the caller via
//     DecideMode). We deliberately DON'T construct one ourselves so
//     tests can inject a mock or a bytes-backed reader.
//   - nonInteractive — when true, ResolveInteractive returns
//     ErrInteractiveRequired immediately instead of calling the
//     prompt. This is the "don't hang on stdin" path for CI
//     scripts that forgot to set --manager.
//   - in, out      — I/O streams. Mainly used for fallback when p is
//     nil (we degrade to a plain prompt without a ui.Prompt).
//
// Returns the picked Manager and any I/O / parse errors. If only
// one manager is detected, returns it without prompting — there's
// no decision to make.
//
// If the user pressed Ctrl-C / aborts the prompt, returns
// ErrInteractiveRequired so the caller can decide between
// "use the first detected manager" and "abort with a hint".
func ResolveInteractive(reg Registry, p ui.Prompt, nonInteractive bool, in io.Reader, out io.Writer) (Manager, error) {
	switch len(reg.Found) {
	case 0:
		return nil, ErrNoManager
	case 1:
		// Single manager — no decision to make.
		return reg.Found[0], nil
	}

	if nonInteractive {
		// Caller is in a non-interactive run (CI, --yes, --json).
		// Don't hang on stdin; surface the requirement so the CLI
		// can print "use --manager" instead.
		return nil, fmt.Errorf("%w (multiple managers detected: %s; pass --manager or set NODEUP_MANAGER)",
			ErrInteractiveRequired, managerNames(reg.Found))
	}

	// Build the option list in priority order (DetectAll already
	// sorted by Priority, but we re-sort defensively in case a
	// caller constructs the Registry by hand).
	sorted := make([]Manager, len(reg.Found))
	copy(sorted, reg.Found)
	sort.SliceStable(sorted, func(i, j int) bool {
		return Priority(sorted[i].Name()) < Priority(sorted[j].Name())
	})
	options := make([]string, len(sorted))
	for i, m := range sorted {
		options[i] = m.Name()
	}

	var picked string
	var err error
	if p != nil {
		picked, err = p.Select("Multiple Node.js version managers detected. Pick one:", options, options[0])
	} else {
		// Fallback path: caller passed nil for p. Build a fresh
		// plain prompt against the streams they gave us. This is
		// the path tests use to avoid constructing a whole
		// ui.Prompt just for one question.
		pp := ui.NewPrompt(ui.PlainMode, in, out)
		picked, err = pp.Select("Multiple Node.js version managers detected. Pick one:", options, options[0])
	}
	if err != nil {
		return nil, fmt.Errorf("manager prompt: %w", err)
	}

	// Defensive — ui.Select should always return one of the
	// options or the default, but if it doesn't, error rather
	// than return a half-picked manager.
	for _, m := range sorted {
		if m.Name() == picked {
			return m, nil
		}
	}
	return nil, fmt.Errorf("manager prompt returned unrecognized option %q", picked)
}
