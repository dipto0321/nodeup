package cli

import (
	"bufio"
	"fmt"
	"io"
	"sort"

	"github.com/Masterminds/semver/v3"

	"github.com/dipto0321/nodeup/internal/detector"
)

// cleanupIO holds the input and output streams for a cleanup prompt
// run. Tests inject pipes here so they can drive stdin / capture
// stdout without touching the real terminal.
type cleanupIO struct {
	in  *bufio.Reader
	out io.Writer
}

// cleanupDecision captures the user's answers to the post-upgrade
// prompt, so the caller (upgrade.go) can either confirm or skip.
type cleanupDecision struct {
	// deleteAll means the user answered "y" to the all-or-nothing
	// prompt — every candidate will be deleted (one-by-one confirm
	// still happens so accidental "y" doesn't nuke everything).
	deleteAll bool
	// deleteOne is set when the user picks a specific version to
	// delete by name. Empty string means "no specific version
	// selected".
	deleteOne string
	// skip means the user picked "N" / empty at the all-or-nothing
	// prompt — nothing more to ask, the whole cleanup is over.
	skip bool
}

// cleanupConfig holds the behavior toggles for the prompt. The CLI
// upgrade flow reads these from cfg.Cleanup + the on-the-fly flags.
type cleanupConfig struct {
	// AutoDeleteAll, when true, skips the all-or-nothing prompt and
	// proceeds to delete every candidate (still with per-version
	// confirm unless PerVersion is also false).
	//
	// Sourced from either cfg.Cleanup.Auto OR --cleanup, both of
	// which map to AutoDeleteAll = true.
	AutoDeleteAll bool

	// PerVersion, when true, prompts before each individual
	// deletion. Sourced from cfg.Cleanup.Prompt (default true).
	// When false, the per-version prompt is skipped and we delete
	// everything that survived the all-or-nothing prompt.
	PerVersion bool

	// NonInteractive, when true, makes runCleanupPrompt a no-op
	// return so the upgrade flow can skip cleanup entirely.
	// Sourced from --no-cleanup.
	NonInteractive bool

	// Prefiltered, when non-nil, is the set of versions the user
	// explicitly asked to delete via --cleanup-version (repeatable).
	// When set, the all-or-nothing prompt is skipped and only
	// these versions are offered for deletion.
	Prefiltered []semver.Version
}

// cleanupResult summarizes what happened during a cleanup run. The
// upgrade command reports this to the user after the prompt loop
// completes (success or partial-failure).
type cleanupResult struct {
	Deleted []semver.Version
	Skipped []semver.Version
	Failed  []cleanupFailure
}

type cleanupFailure struct {
	Version semver.Version
	Err     error
}

// runCleanupPrompt is the post-upgrade prompt entry point. It walks
// the user through deciding what (if anything) to delete, then
// invokes m.Uninstall for each chosen version. Returns a result for
// the caller to summarize.
//
// Parameters:
//
//   - cfg      — behavior toggles (AutoDeleteAll / PerVersion /
//     NonInteractive / Prefiltered). All derived from CLI flags +
//     config file before this call.
//   - toInstall — the versions nodeup just installed (LTS + Current).
//     These are excluded from "old versions" candidates.
//   - installed — every version the manager had BEFORE the upgrade.
//     Used to compute the candidate set.
//   - active   — the version that's currently active on PATH (e.g.,
//     the OLD default before we set a new one). Excluded from
//     candidates so the user's shell doesn't break.
//   - m        — the manager implementation, used for Uninstall().
//   - io       — input/output streams. Tests inject pipes here.
//
// Errors from individual Uninstall() calls are collected but do NOT
// stop the loop — one failed delete doesn't block the next attempt.
func runCleanupPrompt(cfg cleanupConfig, toInstall []semver.Version, installed []semver.Version, active semver.Version, m detector.Manager, streams cleanupIO) (cleanupResult, error) {
	var result cleanupResult

	// Step 1: Non-interactive skip.
	if cfg.NonInteractive {
		return result, nil
	}

	// Step 2: Compute the candidates set: installed \ {new LTS,
	// new Current, active}.
	candidates := cleanupCandidates(toInstall, installed, active)
	if len(candidates) == 0 {
		// Nothing eligible to delete — print a friendly note and
		// return success.
		fmt.Fprintln(streams.out, "No old versions to clean up.")
		return result, nil
	}

	// Step 3: Determine which candidates we'll offer.
	var toOffer []semver.Version
	if len(cfg.Prefiltered) > 0 {
		// User passed --cleanup-version; only offer those that are
		// actually in the candidate set.
		toOffer = intersectCandidates(candidates, cfg.Prefiltered)
		if len(toOffer) == 0 {
			fmt.Fprintln(streams.out, "No matching versions to clean up.")
			return result, nil
		}
	} else if !cfg.AutoDeleteAll {
		// Default path: prompt "delete all old versions? [y/N]"
		decision, err := promptAllOrNothing(candidates, streams)
		if err != nil {
			return result, fmt.Errorf("cleanup prompt: %w", err)
		}
		if decision.skip {
			fmt.Fprintln(streams.out, "Cleanup skipped.")
			return result, nil
		}
		if !decision.deleteAll && decision.deleteOne == "" {
			// User pressed something we didn't understand; treat as skip.
			fmt.Fprintln(streams.out, "Cleanup skipped (unrecognized answer).")
			return result, nil
		}
		if decision.deleteOne != "" {
			// User picked a specific version by number.
			v, perr := semver.NewVersion(decision.deleteOne)
			if perr != nil || !inCandidates(*v, candidates) {
				fmt.Fprintln(streams.out, "Cleanup skipped (invalid version choice).")
				return result, nil
			}
			toOffer = []semver.Version{*v}
		} else {
			// deleteAll: every candidate is on the table.
			toOffer = candidates
		}
	} else {
		// AutoDeleteAll (e.g. --cleanup or cfg.Cleanup.Auto): skip the
		// all-or-nothing prompt.
		toOffer = candidates
	}

	// Step 4: Per-version loop.
	for _, v := range toOffer {
		confirm := true
		if cfg.PerVersion {
			answer, err := promptPerVersion(v, streams)
			if err != nil {
				result.Failed = append(result.Failed, cleanupFailure{v, err})
				continue
			}
			if !answer {
				result.Skipped = append(result.Skipped, v)
				continue
			}
			_ = confirm
		}

		if err := m.Uninstall(v); err != nil {
			result.Failed = append(result.Failed, cleanupFailure{v, err})
			fmt.Fprintf(streams.out, "  Failed to delete %s: %v\n", v, err)
			continue
		}
		result.Deleted = append(result.Deleted, v)
		fmt.Fprintf(streams.out, "  Deleted %s\n", v)
	}

	return result, nil
}

// cleanupCandidates returns installed \ {new LTS, new Current, active},
// sorted ascending. The exclusions matter because:
//   - new LTS / new Current — we just installed them, so they ARE
//     in `installed` (via the `toInstall` list) and obviously must
//     not be deleted by the cleanup step.
//   - active — the OLD default. Deleting it would break the
//     user's shell until they re-`source` their manager rc file.
//     We never delete what's currently active.
//
// Empty inputs are tolerated: if none of toInstall / active are
// non-empty, only the empty-set exclusion applies.
func cleanupCandidates(toInstall []semver.Version, installed []semver.Version, active semver.Version) []semver.Version {
	exclude := make(map[string]struct{}, len(toInstall)+1)
	for _, v := range toInstall {
		exclude[v.String()] = struct{}{}
	}
	if !active.Equal(&semver.Version{}) {
		exclude[active.String()] = struct{}{}
	}

	out := make([]semver.Version, 0, len(installed))
	for _, v := range installed {
		if _, ok := exclude[v.String()]; ok {
			continue
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Compare(&out[j]) < 0
	})
	return out
}

// intersectCandidates returns the elements of `want` that are also in
// `candidates`. Order matches `want`'s order (so a user-supplied
// `--cleanup-version 22 20` keeps that order rather than alphabetical).
// We filter by string equality (semver.Compare should agree, but the
// string is what we'd print).
func intersectCandidates(candidates []semver.Version, want []semver.Version) []semver.Version {
	set := make(map[string]struct{}, len(candidates))
	for _, c := range candidates {
		set[c.String()] = struct{}{}
	}
	out := make([]semver.Version, 0, len(want))
	for _, w := range want {
		if _, ok := set[w.String()]; ok {
			out = append(out, w)
		}
	}
	return out
}

// inCandidates reports whether v is in the candidates set. Uses
// string equality to avoid any semver-compare edge cases (e.g.,
// build metadata).
func inCandidates(v semver.Version, candidates []semver.Version) bool {
	for _, c := range candidates {
		if c.String() == v.String() {
			return true
		}
	}
	return false
}

// promptAllOrNothing prints the candidate list and asks the user
// whether to delete them all, delete a specific one, or skip. The
// expected input is one of:
//
//	y / Y            — delete-all
//	<version>        — delete that specific version (matched against
//	                    the printed list)
//	empty / n / N    — skip
//	anything else    — re-prompt (we give one re-prompt then skip)
//
// Reads a single line from `in` (ignoring trailing newline). Returns
// the decision and any I/O error.
func promptAllOrNothing(candidates []semver.Version, streams cleanupIO) (cleanupDecision, error) {
	fmt.Fprintf(streams.out, "\nOld Node.js versions still on disk (%d):\n", len(candidates))
	for _, v := range candidates {
		fmt.Fprintf(streams.out, "  v%s\n", v)
	}
	fmt.Fprintln(streams.out, "")
	fmt.Fprintln(streams.out, "What would you like to do?")
	fmt.Fprintln(streams.out, "  y       Delete all of the above")
	fmt.Fprintln(streams.out, "  <num>   Delete one specific version (e.g. 22.11.0)")
	fmt.Fprintln(streams.out, "  N       Skip cleanup")

	// First attempt
	line, err := readPromptLine(streams)
	if err != nil {
		return cleanupDecision{}, err
	}
	answer := normalizeAnswer(line)
	switch answer {
	case "y", "yes":
		return cleanupDecision{deleteAll: true}, nil
	case "", "n", "no":
		return cleanupDecision{skip: true}, nil
	}
	// Treat a version-like answer as a one-shot specific-version pick.
	// We accept anything that has at least one digit, since the printed
	// versions are bare semvers.
	if v, perr := semver.NewVersion(answer); perr == nil {
		return cleanupDecision{deleteOne: v.String()}, nil
	}
	// Garbage input: re-prompt once.
	fmt.Fprintln(streams.out, "(Please answer y, a specific version, or N.)")
	line2, err := readPromptLine(streams)
	if err != nil {
		return cleanupDecision{}, err
	}
	answer2 := normalizeAnswer(line2)
	switch answer2 {
	case "y", "yes":
		return cleanupDecision{deleteAll: true}, nil
	case "", "n", "no":
		return cleanupDecision{skip: true}, nil
	}
	if v, perr := semver.NewVersion(answer2); perr == nil {
		return cleanupDecision{deleteOne: v.String()}, nil
	}
	// Still unrecognized — treat as skip.
	return cleanupDecision{skip: true}, nil
}

// promptPerVersion prints "Delete vX.Y.Z? [y/N]" and reads one line.
// Returns true on y/Y/yes (case-insensitive), false on anything
// else (including empty input).
func promptPerVersion(v semver.Version, streams cleanupIO) (bool, error) {
	fmt.Fprintf(streams.out, "Delete v%s? [y/N] ", v)
	line, err := readPromptLine(streams)
	if err != nil {
		return false, err
	}
	switch normalizeAnswer(line) {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}

// readPromptLine reads one trimmed line from `in`. We use a buffered
// reader rather than bufio.Scanner because Scanner drops trailing
// tokens on long lines (>64 KiB) — pathological for an `nodeup`
// prompt but worth handling defensively.
func readPromptLine(streams cleanupIO) (string, error) {
	line, err := streams.in.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	// Strip trailing \r\n or \n.
	line = trimNewline(line)
	return line, nil
}

func trimNewline(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}

// normalizeAnswer lower-cases and trims the input.
func normalizeAnswer(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		out = append(out, c)
	}
	// Trim leading/trailing spaces (manual loop avoids pulling in
	// strings just for this hot-path helper).
	start, end := 0, len(out)
	for start < end && (out[start] == ' ' || out[start] == '\t') {
		start++
	}
	for end > start && (out[end-1] == ' ' || out[end-1] == '\t') {
		end--
	}
	return string(out[start:end])
}

// formatCleanupResult renders the post-prompt summary the upgrade
// command appends to "Upgrade complete!". Intended for cases where
// the user ran the cleanup and saw per-version prompts — a single
// line at the bottom is enough.
func formatCleanupResult(r cleanupResult) string {
	if len(r.Deleted) == 0 && len(r.Failed) == 0 {
		return ""
	}
	out := ""
	if len(r.Deleted) > 0 {
		out += "Deleted: "
		for i, v := range r.Deleted {
			if i > 0 {
				out += ", "
			}
			out += "v" + v.String()
		}
	}
	if len(r.Skipped) > 0 {
		if out != "" {
			out += "  "
		}
		out += "Skipped: "
		for i, v := range r.Skipped {
			if i > 0 {
				out += ", "
			}
			out += "v" + v.String()
		}
	}
	if len(r.Failed) > 0 {
		if out != "" {
			out += "  "
		}
		out += fmt.Sprintf("%d failed", len(r.Failed))
	}
	return out
}
