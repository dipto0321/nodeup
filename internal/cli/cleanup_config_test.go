package cli

import (
	"testing"

	"github.com/Masterminds/semver/v3"

	"github.com/dipto0321/nodeup/internal/config"
)

// TestResolveCleanupConfig_NoCleanupBeatsYes pins the precedence fix
// for #57. Before the fix, --yes ran unconditionally AFTER the switch
// that was supposed to make --no-cleanup win — the body flipped
// NonInteractive back to false and set AutoDeleteAll=true, so
// `nodeup upgrade --yes --no-cleanup` silently deleted every old
// Node.js version. After the fix, --no-cleanup's documented contract
// ("never prompt, never delete") survives any combination of the
// other flags.
func TestResolveCleanupConfig_NoCleanupBeatsYes(t *testing.T) {
	cfg := config.CleanupConfig{Auto: false, Prompt: true}

	got := resolveCleanupConfig(
		/*noCleanup*/ true,
		/*autoCleanup*/ false,
		/*yes*/ true,
		nil,
		cfg,
	)

	if !got.NonInteractive {
		t.Errorf("--no-cleanup + --yes: NonInteractive = false, want true (--no-cleanup must win)")
	}
	if got.AutoDeleteAll {
		t.Errorf("--no-cleanup + --yes: AutoDeleteAll = true, want false (--no-cleanup must win)")
	}
	// Sanity: a regression that flipped the `&& !noCleanup` guard
	// back to a bare `yes` would also need to leave the per-version
	// prompt flag alone. Pin both.
	if !got.PerVersion {
		t.Errorf("--no-cleanup + --yes: PerVersion = false, want true (cfg.Cleanup.Prompt default)")
	}
}

// TestResolveCleanupConfig_YesAutoDeletesWithoutNoCleanup verifies
// that --yes still works as documented when --no-cleanup is NOT set.
// A regression that guarded the block with `!yes` instead of `&& !noCleanup`
// would silently break every CI script relying on --yes.
func TestResolveCleanupConfig_YesAutoDeletesWithoutNoCleanup(t *testing.T) {
	cfg := config.CleanupConfig{Auto: false, Prompt: true}

	got := resolveCleanupConfig(
		/*noCleanup*/ false,
		/*autoCleanup*/ false,
		/*yes*/ true,
		nil,
		cfg,
	)

	if got.NonInteractive {
		t.Errorf("--yes alone: NonInteractive = true, want false (we're not skipping cleanup, just auto-confirming)")
	}
	if !got.AutoDeleteAll {
		t.Errorf("--yes alone: AutoDeleteAll = false, want true")
	}
	if got.PerVersion {
		t.Errorf("--yes alone: PerVersion = true, want false (auto-confirm should skip per-version y/N)")
	}
}

// TestResolveCleanupConfig_CleanupFlagBeatsYesAndCfgAuto documents
// that --cleanup and cfg.Cleanup.Auto both map to AutoDeleteAll=true.
// Both are subsumed under --yes's "skip the per-version confirm"
// behavior — they're the same end state.
func TestResolveCleanupConfig_CleanupFlagSetsAutoDeleteAll(t *testing.T) {
	cfg := config.CleanupConfig{Auto: false, Prompt: true}

	got := resolveCleanupConfig(false, true, false, nil, cfg)
	if !got.AutoDeleteAll {
		t.Errorf("--cleanup: AutoDeleteAll = false, want true")
	}
	if got.NonInteractive {
		t.Errorf("--cleanup: NonInteractive = true, want false")
	}
}

func TestResolveCleanupConfig_ConfigAutoSetsAutoDeleteAll(t *testing.T) {
	cfg := config.CleanupConfig{Auto: true, Prompt: true}

	got := resolveCleanupConfig(false, false, false, nil, cfg)
	if !got.AutoDeleteAll {
		t.Errorf("cfg.Cleanup.Auto=true: AutoDeleteAll = false, want true")
	}
}

// TestResolveCleanupConfig_PrefilteredPropagates covers --cleanup-version:
// the parsed semver slice must pass through unchanged.
func TestResolveCleanupConfig_PrefilteredPropagates(t *testing.T) {
	cfg := config.CleanupConfig{Auto: false, Prompt: true}

	pre := []semver.Version{
		mustVer(t, "20.18.0"),
		mustVer(t, "18.20.4"),
	}

	got := resolveCleanupConfig(false, false, false, pre, cfg)
	if len(got.Prefiltered) != len(pre) {
		t.Fatalf("Prefiltered length = %d, want %d", len(got.Prefiltered), len(pre))
	}
	for i := range pre {
		if got.Prefiltered[i].String() != pre[i].String() {
			t.Errorf("Prefiltered[%d] = %s, want %s", i, got.Prefiltered[i], pre[i])
		}
	}
}

// TestResolveCleanupConfig_DefaultsAreInteractive covers the default
// state: no flags, default config (Auto=false, Prompt=true). Cleanup
// should run interactively with per-version y/N prompts.
func TestResolveCleanupConfig_DefaultsAreInteractive(t *testing.T) {
	cfg := config.CleanupConfig{Auto: false, Prompt: true}

	got := resolveCleanupConfig(false, false, false, nil, cfg)
	if got.NonInteractive {
		t.Errorf("defaults: NonInteractive = true, want false")
	}
	if got.AutoDeleteAll {
		t.Errorf("defaults: AutoDeleteAll = true, want false")
	}
	if !got.PerVersion {
		t.Errorf("defaults: PerVersion = false, want true (cfg.Cleanup.Prompt default)")
	}
	if len(got.Prefiltered) != 0 {
		t.Errorf("defaults: Prefiltered = %v, want empty", got.Prefiltered)
	}
}

// TestResolveCleanupConfig_CfgPromptFalseSkipsPerVersion covers
// cfg.Cleanup.Prompt=false: per-version y/N is skipped but we still
// hit the all-or-nothing prompt (unless Auto is also true).
func TestResolveCleanupConfig_CfgPromptFalseSkipsPerVersion(t *testing.T) {
	cfg := config.CleanupConfig{Auto: false, Prompt: false}

	got := resolveCleanupConfig(false, false, false, nil, cfg)
	if got.PerVersion {
		t.Errorf("cfg.Cleanup.Prompt=false: PerVersion = true, want false")
	}
	if got.AutoDeleteAll {
		t.Errorf("cfg.Cleanup.Prompt=false: AutoDeleteAll = true, want false (only Prompt was false, not Auto)")
	}
}

// TestResolveCleanupConfig_NoCleanupBeatsEverythingElse is the
// negative-path matrix for #57. Each row sets one knob that would
// otherwise drive AutoDeleteAll=true and asserts --no-cleanup
// wins on every one. The bug from #57 was a single unguarded
// `if yes` block; this test pins all the other places the same
// regression could re-appear.
func TestResolveCleanupConfig_NoCleanupBeatsEverythingElse(t *testing.T) {
	cases := []struct {
		name        string
		autoCleanup bool
		yes         bool
		cfg         config.CleanupConfig
	}{
		{"noCleanup + autoCleanup", true, false, config.CleanupConfig{Auto: false, Prompt: true}},
		{"noCleanup + yes", false, true, config.CleanupConfig{Auto: false, Prompt: true}},
		{"noCleanup + cfg.Auto", false, false, config.CleanupConfig{Auto: true, Prompt: true}},
		{"noCleanup + every knob", true, true, config.CleanupConfig{Auto: true, Prompt: true}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveCleanupConfig(true, tc.autoCleanup, tc.yes, nil, tc.cfg)
			if !got.NonInteractive {
				t.Errorf("NonInteractive = false, want true (--no-cleanup must always win)")
			}
			if got.AutoDeleteAll {
				t.Errorf("AutoDeleteAll = true, want false (--no-cleanup must always win)")
			}
		})
	}
}
