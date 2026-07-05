package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"

	"github.com/dipto0321/nodeup/internal/config"
	"github.com/dipto0321/nodeup/internal/detector"
	"github.com/dipto0321/nodeup/internal/ui"
)

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

	// ForcePerVersion, when true, downgrades any auto-confirm path
	// (AutoDeleteAll or a PerVersion=false setting) to per-version
	// confirmation. Sourced from a defensive upgrade-flow knob: if
	// `Manager.Current()` failed to identify the active Node version,
	// we can't safely exclude it from candidates, so we force the
	// user to answer "y/N" per candidate instead of letting
	// --cleanup / --yes / cfg.Cleanup.Auto mass-delete. See #58.
	ForcePerVersion bool
}

// resolveCleanupConfig maps the post-upgrade cleanup toggles (CLI flags
// + config-file values) into the cleanupConfig struct that
// runCleanupPrompt consumes.
//
// Precedence, highest first:
//
//	--no-cleanup             → NonInteractive=true; nothing else applies.
//	                          --no-cleanup's documented contract is
//	                          "never prompt, never delete" and it beats
//	                          every other toggle, including --yes. A CI
//	                          script that always passes `-y`/`--yes`
//	                          must NOT silently delete Node installs
//	                          the user told it to keep. See #57.
//	--cleanup                → AutoDeleteAll=true.
//	cfg.Cleanup.Auto         → AutoDeleteAll=true.
//	--yes (and !noCleanup)   → AutoDeleteAll=true, PerVersion=false,
//	                          NonInteractive=false. So a non-interactive
//	                          run still makes progress without hanging
//	                          on an unserviced prompt.
//
// PerVersion defaults to cfg.Cleanup.Prompt (true by default), and
// Prefiltered is whatever --cleanup-version passed.
func resolveCleanupConfig(noCleanup, autoCleanup, yes bool, cleanupVersions []semver.Version, cfg config.CleanupConfig) cleanupConfig {
	out := cleanupConfig{
		NonInteractive: noCleanup,
		PerVersion:     cfg.Prompt,
		Prefiltered:    cleanupVersions,
	}
	switch {
	case noCleanup:
		// Already set; no other knobs apply. --no-cleanup wins.
	case autoCleanup:
		out.AutoDeleteAll = true
	case cfg.Auto:
		out.AutoDeleteAll = true
	}
	if yes && !noCleanup {
		// --yes implies auto-delete-all so non-interactive runs don't
		// hang on an unserviced prompt. Guarded by !noCleanup so a user
		// who explicitly says "never delete" doesn't have that contract
		// silently overridden by a CI script's `-y` flag. See #57.
		out.NonInteractive = false
		out.AutoDeleteAll = true
		out.PerVersion = false
	}
	return out
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
//   - p         — ui.Prompt, mode-resolved by the caller. FancyMode
//     gets huh.Select/huh.Confirm; PlainMode gets the in-package
//     line-reader fallback. The CLI layer constructs this once per
//     command so we don't re-TTY-probe inside cleanup.go.
//   - w         — ui.Writer for emitting Info / Success / Warn lines
//     (e.g., "Deleted v22", "Cleanup skipped"). Kept separate from
//     `p` because the writer targets stdout/stderr while the prompt
//     targets stdin/stdout; a single ui.Prompt would conflate them.
//   - cfg       — behavior toggles (AutoDeleteAll / PerVersion /
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
//
// Errors from individual Uninstall() calls are collected but do NOT
// stop the loop — one failed delete doesn't block the next attempt.
func runCleanupPrompt(p ui.Prompt, w ui.Writer, cfg cleanupConfig, toInstall, installed []semver.Version, active semver.Version, m detector.Manager) (cleanupResult, error) {
	var result cleanupResult

	// Step 1: Non-interactive skip.
	if cfg.NonInteractive {
		return result, nil
	}

	// Step 1b: ForcePerVersion downgrades any auto-confirm path. We
	// apply this AFTER the NonInteractive check so --no-cleanup still
	// wins (skipping cleanup entirely is the strongest fail-closed
	// stance — but a Current() failure doesn't necessarily mean
	// "don't cleanup at all"; it just means "be paranoid about which
	// version is currently powering the user's shell"). See #58.
	if cfg.ForcePerVersion {
		cfg.AutoDeleteAll = false
		cfg.PerVersion = true
	}

	// Step 2: Compute the candidates set: installed \ {new LTS,
	// new Current, active}.
	candidates := cleanupCandidates(toInstall, installed, active)
	if len(candidates) == 0 {
		if w != nil {
			w.Info("No old versions to clean up.")
		}
		return result, nil
	}

	// Step 3: Determine which candidates we'll offer.
	var toOffer []semver.Version
	if len(cfg.Prefiltered) > 0 {
		// User passed --cleanup-version; only offer those that are
		// actually in the candidate set.
		toOffer = intersectCandidates(candidates, cfg.Prefiltered)
		if len(toOffer) == 0 {
			if w != nil {
				w.Info("No matching versions to clean up.")
			}
			return result, nil
		}
	} else if !cfg.AutoDeleteAll {
		// Default path: prompt "delete all old versions? [y/N]"
		decision, err := promptAllOrNothing(p, w, candidates)
		if err != nil {
			return result, fmt.Errorf("cleanup prompt: %w", err)
		}
		if decision.skip {
			if w != nil {
				w.Info("Cleanup skipped.")
			}
			return result, nil
		}
		if !decision.deleteAll && decision.deleteOne == "" {
			if w != nil {
				w.Info("Cleanup skipped (unrecognized answer).")
			}
			return result, nil
		}
		if decision.deleteOne != "" {
			v, perr := semver.NewVersion(decision.deleteOne)
			if perr != nil || !inCandidates(*v, candidates) {
				if w != nil {
					w.Info("Cleanup skipped (invalid version choice).")
				}
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

	// Step 4: Per-version loop. (See sticky-up note in cleanup.go
	// history.) ForcePerVersion keeps cfg.PerVersion = true at this
	// point; everything else gets downgraded to false so users
	// who answered "y" once don't have to re-answer per candidate.
	if !cfg.ForcePerVersion {
		cfg.PerVersion = false
	}
	for _, v := range toOffer {
		if cfg.PerVersion {
			answer, err := promptPerVersion(p, w, v)
			if err != nil {
				result.Failed = append(result.Failed, cleanupFailure{v, err})
				continue
			}
			if !answer {
				result.Skipped = append(result.Skipped, v)
				continue
			}
		}

		if err := m.Uninstall(v); err != nil {
			result.Failed = append(result.Failed, cleanupFailure{v, err})
			if w != nil {
				w.Warn(fmt.Sprintf("Failed to delete %s: %v", v, err))
			}
			continue
		}
		result.Deleted = append(result.Deleted, v)
		if w != nil {
			w.Success(fmt.Sprintf("Deleted %s", v))
		}
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
func cleanupCandidates(toInstall, installed []semver.Version, active semver.Version) []semver.Version {
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
func intersectCandidates(candidates, want []semver.Version) []semver.Version {
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
// The "delete one specific version" path now goes through
// ui.Prompt.Select with a labeled option list, replacing the
// previous "type a version string" pattern. This matches what
// huh.Select does in FancyMode (arrow-key navigation over a list
// of options) and keeps the PlainMode UX consistent — same
// numbered list either way.
//
// Returns the decision and any I/O error.
func promptAllOrNothing(p ui.Prompt, w ui.Writer, candidates []semver.Version) (cleanupDecision, error) {
	if w != nil {
		w.Info(fmt.Sprintf("Old Node.js versions still on disk (%d):", len(candidates)))
		for _, v := range candidates {
			w.Println(fmt.Sprintf("  v%s", v))
		}
		w.Println("")
		w.Println("What would you like to do?")
	}

	// Build the option list. The numeric prefix is rendered by
	// ui.Prompt.Select itself (huh does it in FancyMode; the
	// plain fallback does it in PlainMode) so we don't prefix
	// here.
	options := []string{"Delete all of the above", "Skip cleanup"}
	for _, v := range candidates {
		options = append(options, fmt.Sprintf("Delete v%s", v))
	}

	picked, err := p.Select("Choose an action:", options, options[1]) // default: "Skip cleanup"
	if err != nil {
		return cleanupDecision{}, err
	}

	switch picked {
	case options[0]:
		return cleanupDecision{deleteAll: true}, nil
	case options[1]:
		return cleanupDecision{skip: true}, nil
	default:
		// Strip the "Delete v" prefix to recover the version.
		if rest, ok := strings.CutPrefix(picked, "Delete v"); ok {
			return cleanupDecision{deleteOne: rest}, nil
		}
		// Defensive: shouldn't happen if Select returned one of our
		// options, but if it didn't we treat as skip rather than
		// nuking the user's Node installs.
		return cleanupDecision{skip: true}, nil
	}
}

// promptPerVersion prints "Delete vX.Y.Z? [y/N]" and reads one line.
// Returns true on y/Y/yes (case-insensitive), false on anything
// else (including empty input).
func promptPerVersion(p ui.Prompt, w ui.Writer, v semver.Version) (bool, error) {
	question := fmt.Sprintf("Delete v%s?", v)
	// We rely on ui.Prompt.Confirm to render the [y/N] hint for
	// PlainMode; the huh-backed FancyMode renders its own toggle
	// label. Callers pass a real Writer (not nil) when they want
	// surface messages; we use it here to print the question line
	// in case the prompt's internal rendering puts the question
	// in an unexpected place on this terminal.
	if w != nil {
		w.Println(question + " [y/N]")
	}
	got, err := p.Confirm(question, false)
	if err != nil {
		return false, err
	}
	return got, nil
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
