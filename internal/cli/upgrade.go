package cli

import (
	"fmt"

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
	managerPref, _ := cmd.Flags().GetString("manager")
	yes, _ := cmd.Flags().GetBool("yes")
	_ = yes // TODO: use for non-interactive mode
	offline, _ := cmd.Flags().GetBool("offline")

	// Detect managers
	installed := detector.DetectAll()
	m, err := detector.ResolveManager(installed, managerPref)
	if err != nil {
		return fmt.Errorf("resolve manager: %w", err)
	}
	cmd.Printf("Using manager: %s\n", m.Name())

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
	if !noMigrate {
		ctx := cmd.Context()
		for _, v := range installedVersions {
			if err := packages.Snapshot(ctx, m.Name(), v); err != nil {
				cmd.Printf("Warning: snapshot failed for %s: %v\n", v, err)
			}
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
	if !noMigrate && len(toInstall) > 0 {
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
