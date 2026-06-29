package cli

import (
	"context"
	"fmt"

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
	cmd := &cobra.Command{
		Use:   "snapshot",
		Short: "Snapshot the active version's global packages",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSnapshot(cmd, args)
		},
	}
	return cmd
}

func runSnapshot(cmd *cobra.Command, args []string) error {
	installed := detector.DetectAll()
	if len(installed.Found) == 0 {
		return fmt.Errorf("no Node.js version manager detected")
	}

	m := installed.Found[0]
	version, err := getCurrentVersion(m)
	if err != nil {
		return fmt.Errorf("get current version: %w", err)
	}

	if err := packages.Snapshot(context.Background(), m.Name(), version); err != nil {
		return fmt.Errorf("snapshot failed: %w", err)
	}

	cmd.Printf("Snapshot saved for %s %s\n", m.Name(), version)
	return nil
}

func getCurrentVersion(m detector.Manager) (semver.Version, error) {
	versions, err := m.ListInstalled()
	if err != nil {
		return semver.Version{}, err
	}
	if len(versions) == 0 {
		return semver.Version{}, fmt.Errorf("no installed versions")
	}
	return versions[0], nil
}

func newPackagesListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List packages from a snapshot",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPackagesList(cmd, args)
		},
	}
	return cmd
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
		Use:   "restore <manager> <version>",
		Short: "Re-install packages from a snapshot",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRestore(cmd, args)
		},
	}
	return cmd
}

func runRestore(cmd *cobra.Command, args []string) error {
	managerName := args[0]
	versionStr := args[1]

	v, err := semver.NewVersion(versionStr)
	if err != nil {
		return fmt.Errorf("invalid version: %w", err)
	}

	if err := packages.Restore(context.Background(), managerName, *v); err != nil {
		return fmt.Errorf("restore failed: %w", err)
	}

	cmd.Printf("Restored packages for %s %s\n", managerName, versionStr)
	return nil
}

func newDiffCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "diff <version1> <version2>",
		Short: "Diff two snapshots",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("diff not yet implemented")
		},
	}
	return cmd
}