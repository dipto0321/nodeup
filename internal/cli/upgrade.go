package cli

import (
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
	_ = noCleanup // TODO: implement cleanup logic in Phase 7
	managerFlag, _ := cmd.Flags().GetString("manager")
	yes, _ := cmd.Flags().GetBool("yes")
	_ = yes // TODO: use for non-interactive mode
	offline, _ := cmd.Flags().GetBool("offline")

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

	// Snapshot current packages
	if !skipMigrate {
		ctx := cmd.Context()
		for _, v := range installedVersions {
			if err := packages.Snapshot(ctx, m.Name(), v); err != nil {
				cmd.Printf("Warning: snapshot failed for %s: %v\n", v, err)
			}
		}
	}

	// Arm the sentinel AFTER snapshots are on disk but BEFORE any
	// install mutation. If we crash between here and the deferred
	// cleanup, the sentinel is the "this upgrade was interrupted"
	// breadcrumb that the next `nodeup` invocation will pick up.
	//
	// We point the sentinel at the latest installed version's snapshot
	// since that is the most likely one we want to replay against —
	// it contains the package set the user had right before we started
	// installing the new versions.
	if !skipMigrate {
		newVersion := ""
		if len(toInstall) > 0 {
			newVersion = toInstall[len(toInstall)-1].String()
		}
		oldVersion := ""
		var snapshotPath string
		if len(installedVersions) > 0 {
			last := installedVersions[len(installedVersions)-1]
			oldVersion = last.String()
			// Resolve the snapshot path the conventional way so the
			// warning message can hand it to the user verbatim. We
			// tolerate a failure here — without a snapshot path the
			// sentinel is still useful (it tells the user a migration
			// was in flight) and we don't want to abort the upgrade
			// over a path-resolution glitch.
			if p, perr := packages.SnapshotPath(m.Name(), oldVersion); perr == nil {
				snapshotPath = p
			}
		}
		if err := packages.WriteSentinel(packages.UpgradeSentinel{
			Manager:      m.Name(),
			OldVersion:   oldVersion,
			NewVersion:   newVersion,
			SnapshotPath: snapshotPath,
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

	// Restore packages
	if !skipMigrate && len(toInstall) > 0 {
		ctx := cmd.Context()
		for _, v := range toInstall {
			if err := packages.Restore(ctx, m.Name(), *v); err != nil {
				cmd.Printf("Warning: restore failed: %v\n", err)
			}
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
