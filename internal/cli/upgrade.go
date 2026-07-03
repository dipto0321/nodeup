package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"

	"github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/dipto0321/nodeup/internal/detector"
	"github.com/dipto0321/nodeup/internal/node"
	"github.com/dipto0321/nodeup/internal/packages"
)

// newUpgradeCmd implements `nodeup upgrade` — upgrade LTS and/or Current versions.
// Wires the full flow: detect → resolve → fetch → snapshot → install → migrate → cleanup.
func newUpgradeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade Node.js LTS and/or Current versions",
		Long: `Upgrade Node.js LTS and Current to the latest versions and
migrate your global npm packages across automatically.`,
		RunE: runUpgrade,
	}

	cmd.Flags().Bool("lts", false, "upgrade LTS only")
	cmd.Flags().Bool("current", false, "upgrade Current only")
	cmd.Flags().Bool("dry-run", false, "show the plan without making changes")
	cmd.Flags().Bool("no-migrate", false, "skip global package migration")
	cmd.Flags().Bool("no-cleanup", false, "skip the prompt to remove old versions")
	cmd.Flags().Bool("cleanup", false, "auto-confirm the post-upgrade cleanup of old versions")
	cmd.Flags().StringSlice("cleanup-version", nil, "specify version(s) to delete (repeatable; pairs with --cleanup)")
	cmd.Flags().String("manager", "", "force a specific manager (fnm, nvm, volta, asdf, mise, n, nodenv)")
	cmd.Flags().BoolP("yes", "y", false, "non-interactive, assume yes to all prompts")
	cmd.Flags().Bool("offline", false, "use cached data, don't hit nodejs.org")

	return cmd
}

func runUpgrade(cmd *cobra.Command, args []string) error {
	ltsOnly, _ := cmd.Flags().GetBool("lts")
	currentOnly, _ := cmd.Flags().GetBool("current")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	noMigrate, _ := cmd.Flags().GetBool("no-migrate")
	noCleanup, _ := cmd.Flags().GetBool("no-cleanup")
	autoCleanup, _ := cmd.Flags().GetBool("cleanup")
	cleanupVersionsRaw, _ := cmd.Flags().GetStringSlice("cleanup-version")
	managerFlag, _ := cmd.Flags().GetString("manager")
	yes, _ := cmd.Flags().GetBool("yes")
	offline, _ := cmd.Flags().GetBool("offline")

	// --cleanup-version must be parseable semvers — fail fast if not,
	// so a typo doesn't silently delete nothing.
	cleanupVersions := make([]semver.Version, 0, len(cleanupVersionsRaw))
	for _, raw := range cleanupVersionsRaw {
		v, err := parseVersion(raw)
		if err != nil {
			return fmt.Errorf("--cleanup-version %q: %w", raw, err)
		}
		cleanupVersions = append(cleanupVersions, *v)
	}

	// Load the effective config (defaults < file < env). The --manager
	// CLI flag, if provided, takes precedence over everything else.
	cfg, err := loadConfigOrDefault()
	if err != nil {
		return err
	}
	managerPref := managerFlag
	if managerPref == "" {
		managerPref = cfg.Manager
	}
	// --no-migrate / --no-cleanup flags beat config; otherwise follow cfg.
	skipMigrate := noMigrate || !cfg.Packages.Migrate
	_ = skipMigrate // referenced below in snapshot/restore sections

	// Cleanup behavior toggles. Resolution order (highest first):
	//   --no-cleanup           : never prompt, never delete
	//   --cleanup              : auto-delete all (no all-or-nothing prompt)
	//   --cleanup-version <v>  : delete only these versions
	//   cfg.Cleanup.Auto       : auto-delete all (no prompt)
	//   cfg.Cleanup.Prompt=false: skip the per-version confirm
	//   default                : prompt, one y/N per cleanup action
	//
	// The cleanupConfig struct is what runCleanupPrompt consumes.
	cleanupCfg := cleanupConfig{
		NonInteractive: noCleanup,
		PerVersion:     cfg.Cleanup.Prompt,
		Prefiltered:    cleanupVersions,
	}
	switch {
	case noCleanup:
		// already set; no other knobs apply
	case autoCleanup:
		cleanupCfg.AutoDeleteAll = true
		cleanupCfg.Prefiltered = cleanupVersions // combine with --cleanup-version if both set
	case cfg.Cleanup.Auto:
		cleanupCfg.AutoDeleteAll = true
		cleanupCfg.Prefiltered = cleanupVersions
	}
	// --yes implies auto-delete-all so non-interactive runs don't hang.
	if yes {
		cleanupCfg.NonInteractive = false
		cleanupCfg.AutoDeleteAll = true
		cleanupCfg.PerVersion = false
	}
	_ = yes

	// From here on, anything that mutates disk in a way that needs
	// replay-by-sentinel bookkeeping is wrapped in a sentinel lifecycle.
	// We use a flag (rather than os.Exit) so deferred cleanup runs even
	// on error paths.
	sentinelArmed := false
	defer func() {
		if sentinelArmed {
			if err := packages.RemoveSentinel(); err != nil {
				cmd.Printf("Warning: failed to remove upgrade sentinel: %v\n", err)
			}
		}
	}()

	// Detect managers
	installed := detector.DetectAll()
	m, err := detector.ResolveManager(installed, managerPref)
	if err != nil {
		return fmt.Errorf("resolve manager: %w", err)
	}
	cmd.Printf("Using manager: %s\n", m.Name())

	// Probe the system Node BEFORE we start touching anything. If it
	// turns out `node` on PATH is owned by the OS package manager, snap,
	// flatpak, or homebrew-core (i.e., NOT inside the manager's data
	// directory), surface a warning so the user understands nodeup will
	// leave that binary alone and what tool to use instead.
	//
	// We print to stderr, not stdout, so machine-readable consumers
	// (e.g., `nodeup upgrade | jq`) don't get the prose mixed into their
	// JSON / table output.
	warnSystemNodeIfNeeded(cmd.Context(), m, os.Stderr)

	// Fetch versions
	var manifest node.Manifest
	if offline {
		manifest, err = node.LoadCached()
	} else {
		manifest, err = node.FetchManifest()
	}
	if err != nil {
		return fmt.Errorf("fetch versions: %w", err)
	}

	var targetVersions []*node.ManifestVersion
	if ltsOnly {
		v, err := manifest.LatestLTS()
		if err != nil {
			return err
		}
		targetVersions = append(targetVersions, v)
	} else if currentOnly {
		v, err := manifest.LatestCurrent()
		if err != nil {
			return err
		}
		targetVersions = append(targetVersions, v)
	} else {
		lts, err := manifest.LatestLTS()
		if err != nil {
			return err
		}
		current, err := manifest.LatestCurrent()
		if err != nil {
			return err
		}
		targetVersions = append(targetVersions, lts, current)
	}

	// Get installed versions
	installedVersions, err := m.ListInstalled()
	if err != nil {
		return fmt.Errorf("list installed versions: %w", err)
	}

	// Compute plan
	var toInstall []*semver.Version
	for _, tv := range targetVersions {
		v, err := parseVersion(tv.Version)
		if err != nil {
			return fmt.Errorf("parse target version %q: %w", tv.Version, err)
		}
		needsInstall := true
		for _, iv := range installedVersions {
			if iv.Equal(v) {
				needsInstall = false
				break
			}
		}
		if needsInstall {
			toInstall = append(toInstall, v)
		}
	}

	if dryRun {
		cmd.Println("Dry run - would install:")
		for _, v := range toInstall {
			cmd.Printf("  - %s\n", v)
		}
		return nil
	}

	// Snapshot current packages. We record one snapshot per installed
	// version so the user can manually replay against any of them via
	// `nodeup packages restore <mgr> <version>`, but the upgrade flow
	// itself only replays from the latest installed version (below) —
	// the snapshots for older installed versions remain on disk for
	// the user's reference.
	//
	// While we're at it, resolve the conventional snapshot path for
	// the latest installed version — this is the path both the
	// sentinel arms and the restore step reads from, so we compute it
	// once and pass it down.
	var restoreSnapshotPath string
	var oldVersion string
	if !skipMigrate {
		ctx := cmd.Context()
		for _, v := range installedVersions {
			if err := packages.Snapshot(ctx, m.Name(), v); err != nil {
				cmd.Printf("Warning: snapshot failed for %s: %v\n", v, err)
			}
		}
		if len(installedVersions) > 0 {
			last := installedVersions[len(installedVersions)-1]
			oldVersion = last.String()
			// Resolve the snapshot path the conventional way. We
			// tolerate a failure here — without a snapshot path the
			// sentinel is still useful (it tells the user a
			// migration was in flight) and we don't want to abort
			// the upgrade over a path-resolution glitch.
			if p, perr := packages.SnapshotPath(m.Name(), oldVersion); perr == nil {
				restoreSnapshotPath = p
			}
		}
	}

	// Arm the sentinel AFTER snapshots are on disk but BEFORE any
	// install mutation. If we crash between here and the deferred
	// cleanup, the sentinel is the "this upgrade was interrupted"
	// breadcrumb that the next `nodeup` invocation will pick up.
	//
	// The sentinel records the snapshot path so that
	// `nodeup packages restore --from <sentinel path>` can replay
	// against exactly the same package set we were about to install.
	if !skipMigrate {
		newVersion := ""
		if len(toInstall) > 0 {
			newVersion = toInstall[len(toInstall)-1].String()
		}
		if err := packages.WriteSentinel(packages.UpgradeSentinel{
			Manager:      m.Name(),
			OldVersion:   oldVersion,
			NewVersion:   newVersion,
			SnapshotPath: restoreSnapshotPath,
		}); err != nil {
			cmd.Printf("Warning: failed to write upgrade sentinel: %v\n", err)
		} else {
			sentinelArmed = true
		}
	}

	// Install new versions
	for _, v := range toInstall {
		cmd.Printf("Installing %s...\n", v)
		if err := m.Install(*v); err != nil {
			return fmt.Errorf("install %s: %w", v, err)
		}
	}

	// Set default
	if len(toInstall) > 0 {
		latest := toInstall[len(toInstall)-1]
		if err := m.SetDefault(*latest); err != nil {
			return fmt.Errorf("set default: %w", err)
		}
	}

	// Restore packages under the NEW default. We replay the snapshot
	// of the latest previously-installed Node version's global npm
	// packages against the freshly-set-default Node — the user's
	// globals carry forward to the new install exactly once.
	//
	// We deliberately do NOT loop per-new-version here: each newly
	// installed Node has its own `npm install -g` environment, and
	// the right package set is the one from the user's most-recent
	// active Node, not from any freshly-installed (empty) one. The
	// sentinel already records this snapshot path for
	// replay-after-interrupt (`nodeup packages restore --from
	// <sentinel path>`); here we read the same path directly.
	if !skipMigrate && len(toInstall) > 0 {
		if restoreSnapshotPath == "" {
			cmd.Printf("Warning: no snapshot path available to restore from; skipping package migration\n")
		} else if err := packages.RestoreFromSnapshot(cmd.Context(), restoreSnapshotPath); err != nil {
			cmd.Printf("Warning: restore failed: %v\n", err)
		}
	}

	// Post-upgrade cleanup prompt. Phase 7: after a successful
	// upgrade, ask the user whether to delete old versions. We
	// exclude the versions we just installed and the currently-
	// active version (if we can detect it).
	if !cleanupCfg.NonInteractive {
		// Best-effort detection of the currently-active version.
		// A failure here is non-fatal: we just skip the exclusion
		// rather than aborting a successful upgrade.
		var active semver.Version
		if cur, cerr := m.Current(); cerr == nil {
			active = cur
		}

		// Build a values slice from the toInstall pointers so the
		// cleanup helpers (which take []semver.Version) don't have
		// to special-case pointers.
		newVersions := make([]semver.Version, 0, len(toInstall))
		for _, v := range toInstall {
			newVersions = append(newVersions, *v)
		}

		// Use cmd.IO for stdin/stdout when available; fall back to
		// os.Stdin / cmd.OutOrStdout() so non-interactive shells
		// still work. We deliberately do NOT use cmd.SetIn/SetOut
		// (test injection happens via the io package).
		result, cerr := runCleanupPrompt(
			cleanupCfg,
			newVersions,
			installedVersions,
			active,
			m,
			cleanupIO{
				in:  bufio.NewReader(cmd.InOrStdin()),
				out: cmd.OutOrStdout(),
			},
		)
		if cerr != nil {
			// Non-fatal: log and proceed to the success message.
			cmd.Printf("Warning: cleanup encountered an error: %v\n", cerr)
		}
		if summary := formatCleanupResult(result); summary != "" {
			cmd.Printf("Cleanup: %s\n", summary)
		}
	}

	cmd.Printf("Upgrade complete!\n")
	return nil
}

func parseVersion(s string) (*semver.Version, error) {
	if s != "" && s[0] == 'v' {
		s = s[1:]
	}
	return semver.NewVersion(s)
}

// warnSystemNodeIfNeeded is the upgrade-command hook for the
// system-node classifier. It calls ResolveSystemNode with the
// resolved manager, then prints the warning to w (typically
// os.Stderr) when the classifier flags the path as non-managed.
//
// Failure to locate `node` at all is treated as "nothing to warn
// about" — that's a separate concern handled by other code paths.
// Errors during the `which node` probe are silently swallowed
// because the worst case is "we don't print the warning" which is
// strictly better than aborting the upgrade over a path-resolution
// glitch.
func warnSystemNodeIfNeeded(ctx context.Context, m detector.Manager, w io.Writer) {
	info, err := detector.ResolveSystemNode(ctx, m)
	if err != nil {
		// No node on PATH, or `which` itself failed. Not a warning.
		return
	}
	detector.WarnSystemNode(w, info)
}
