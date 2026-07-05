package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/dipto0321/nodeup/internal/detector"
	"github.com/dipto0321/nodeup/internal/packages"
)

// newPackagesCmd implements `nodeup packages` — manage global npm package snapshots.
// Subcommands: snapshot, list, restore, diff.
func newPackagesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "packages",
		Short: "Manage global npm package snapshots",
		Long: `Manage global npm package snapshots — capture, list, restore, and
diff the set of globally installed packages per Node.js version.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newSnapshotCmd())
	cmd.AddCommand(newPackagesListCmd())
	cmd.AddCommand(newRestoreCmd())
	cmd.AddCommand(newDiffCmd())

	return cmd
}

func newSnapshotCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "snapshot",
		Short: "Snapshot the active version's global packages",
		RunE:  runSnapshot,
	}
}

func runSnapshot(cmd *cobra.Command, args []string) error {
	installed := detector.DetectAll()
	if len(installed.Found) == 0 {
		return fmt.Errorf("no Node.js version manager detected")
	}

	m := installed.Found[0]
	version, err := getCurrentVersion(cmd.Context(), m)
	if err != nil {
		return fmt.Errorf("get current version: %w", err)
	}

	if err := packages.Snapshot(cmd.Context(), m.Name(), version); err != nil {
		return fmt.Errorf("snapshot failed: %w", err)
	}

	cmd.Printf("Snapshot saved for %s %s\n", m.Name(), version)
	return nil
}

func getCurrentVersion(ctx context.Context, m detector.Manager) (semver.Version, error) {
	versions, err := m.ListInstalled(ctx)
	if err != nil {
		return semver.Version{}, err
	}
	if len(versions) == 0 {
		return semver.Version{}, fmt.Errorf("no installed versions")
	}
	return versions[0], nil
}

func newPackagesListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List packages from a snapshot",
		RunE:  runPackagesList,
	}
}

func runPackagesList(cmd *cobra.Command, args []string) error {
	snapshots, err := packages.ListSnapshots()
	if err != nil {
		return fmt.Errorf("list snapshots: %w", err)
	}

	if len(snapshots) == 0 {
		cmd.Println("No snapshots found.")
		return nil
	}

	for _, s := range snapshots {
		cmd.Printf("\n%s (Node %s):\n", s.Manager, s.NodeVersion)
		for _, p := range s.Packages {
			cmd.Printf("  - %s@%s\n", p.Name, p.Version)
		}
	}
	return nil
}

func newRestoreCmd() *cobra.Command {
	cmd := &cobra.Command{
		// Two ways to invoke restore:
		//
		//   1. `nodeup packages restore <manager> <version>` — look up
		//      <DataDir>/snapshots/<manager>-<version>.json by name. This
		//      is the path users hit when they deliberately migrate by
		//      saying "give me the packages from fnm 20.10.0".
		//
		//   2. `nodeup packages restore --from <path>` — restore from an
		//      explicit snapshot file. This is the "interrupted-upgrade
		//      replay" path: when `nodeup` detects an orphaned sentinel it
		//      prints the snapshot path verbatim in the warning, so the
		//      user can copy-paste it back into `restore --from`.
		Use:   "restore [<manager> <version>] [--from <path>]",
		Short: "Re-install packages from a snapshot",
		Long: `Re-install global npm packages from a snapshot.

Either pass <manager> <version> to look up <DataDir>/snapshots/<manager>-<version>.json,
or pass --from <path> to restore from an arbitrary snapshot file (the path printed by
the "interrupted upgrade" warning).`,
		Args: func(cmd *cobra.Command, args []string) error {
			fromPath, _ := cmd.Flags().GetString("from")
			if fromPath != "" {
				// --from is mutually exclusive with positional args.
				if len(args) != 0 {
					return fmt.Errorf("--from is mutually exclusive with <manager> and <version>")
				}
				return nil
			}
			return cobra.ExactArgs(2)(cmd, args)
		},
		RunE: runRestore,
	}
	cmd.Flags().String("from", "", "restore from an explicit snapshot file path")
	return cmd
}

func runRestore(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	fromPath, _ := cmd.Flags().GetString("from")

	// runRestore doubles as the "replay the upgrade-in-progress sentinel"
	// command — PersistentPreRunE's hint (root.go:21-40) tells the user
	// to invoke `nodeup packages restore --from <sentinel path>`. If the
	// restore succeeds, the sentinel's job is done and we should clear
	// it so the next `nodeup` invocation doesn't keep warning about an
	// "interrupted upgrade" that has in fact been resolved.
	//
	// We unconditionally attempt the removal after success: a sentinel
	// from a *different* (older) upgrade is harmless stale state, and
	// the next upgrade would overwrite it anyway. We log (don't fail)
	// on a removal error because the user's actual goal — restored
	// packages — has already been achieved.
	clearSentinel := func() {
		if err := packages.RemoveSentinel(); err != nil {
			cmd.Printf("Warning: failed to clear upgrade sentinel: %v\n", err)
		}
	}

	// writeReport persists a MigrationReport so the user has a record
	// of what migrated. We only write when at least one package was
	// attempted — skipping the write on an empty results slice avoids
	// producing a useless "0 packages attempted" file for fresh installs
	// with no globals. The snapshot's recorded Manager / NodeVersion
	// are preferred over the CLI's inferred values when present, so the
	// --from branch (which has no CLI-side manager argument) gets an
	// accurate report.
	writeReport := func(snapshot packages.SnapshotData, fallbackMgr, fallbackFrom string, results []packages.PackageResult) {
		if len(results) == 0 {
			return
		}
		mgr := snapshot.Manager
		if mgr == "" {
			mgr = fallbackMgr
		}
		fromVer := snapshot.NodeVersion
		if fromVer == "" {
			fromVer = fallbackFrom
		}
		report := packages.NewMigrationReport(mgr, fromVer, "")
		for _, r := range results {
			report.AddResult(r)
		}
		if err := report.Save(); err != nil {
			cmd.Printf("Warning: failed to write migration report: %v\n", err)
			return
		}
		if p, perr := report.Path(); perr == nil {
			cmd.Printf("Migration report: %s\n", p)
		}
	}

	// --from branch: read the path straight off disk, no manager or
	// version parsing required.
	if fromPath != "" {
		outcome, err := packages.RestoreFromSnapshot(ctx, fromPath)
		if err != nil {
			// Even on partial failure, persist whatever results we
			// collected so the user has a file to inspect — the
			// returned slice still contains entries for every package
			// attempted, including failures (status="failed").
			writeReport(outcome.Snapshot, "", "", outcome.Results)
			return fmt.Errorf("restore failed: %w", err)
		}
		writeReport(outcome.Snapshot, "", "", outcome.Results)
		clearSentinel()
		cmd.Printf("Restored packages from %s\n", fromPath)
		return nil
	}

	// Positional-arg branch: parse <manager> <version>, look up the
	// conventional <DataDir>/snapshots/<manager>-<version>.json, and
	// re-install its packages onto the currently active Node.
	managerName := args[0]
	versionStr := args[1]

	// Validate the manager name against the canonical allowlist before
	// it touches any file path. A name like `../../tmp/evil` would
	// otherwise pass straight into snapshotPath and, after
	// `filepath.Join` collapses the `..` segments, resolve outside
	// <DataDir>/snapshots — letting an attacker with a local
	// file-placement primitive redirect the snapshot read. See #51.
	if !detector.IsAllowedManagerName(managerName) {
		return fmt.Errorf("invalid manager name %q (allowed: %s)", managerName, strings.Join(detector.AllowedManagerNames(), ", "))
	}

	v, err := semver.NewVersion(versionStr)
	if err != nil {
		return fmt.Errorf("invalid version: %w", err)
	}

	outcome, err := packages.Restore(ctx, managerName, *v)
	if err != nil {
		// Partial failure: persist a report (with the manager name
		// and source version so the user can correlate) so they know
		// which packages to retry.
		writeReport(outcome.Snapshot, managerName, versionStr, outcome.Results)
		return fmt.Errorf("restore failed: %w", err)
	}
	writeReport(outcome.Snapshot, managerName, versionStr, outcome.Results)
	clearSentinel()

	cmd.Printf("Restored packages for %s %s\n", managerName, versionStr)
	return nil
}

func newDiffCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "diff <manager> <version1> <version2>",
		Short: "Diff two snapshots",
		Args:  cobra.ExactArgs(3),
		RunE:  runDiff,
	}
}

func runDiff(cmd *cobra.Command, args []string) error {
	managerName := args[0]

	// Validate before constructing any snapshot path — same reasoning
	// as runRestore above. See #51.
	if !detector.IsAllowedManagerName(managerName) {
		return fmt.Errorf("invalid manager name %q (allowed: %s)", managerName, strings.Join(detector.AllowedManagerNames(), ", "))
	}

	s1, err := packages.LoadSnapshot(managerName, args[1])
	if err != nil {
		return fmt.Errorf("load snapshot %s: %w", args[1], err)
	}

	s2, err := packages.LoadSnapshot(managerName, args[2])
	if err != nil {
		return fmt.Errorf("load snapshot %s: %w", args[2], err)
	}

	added, removed := packages.DiffSnapshots(s1.Packages, s2.Packages)

	cmd.Printf("Added packages:\n")
	for _, p := range added {
		cmd.Printf("  + %s@%s\n", p.Name, p.Version)
	}

	cmd.Printf("Removed packages:\n")
	for _, p := range removed {
		cmd.Printf("  - %s@%s\n", p.Name, p.Version)
	}

	return nil
}
