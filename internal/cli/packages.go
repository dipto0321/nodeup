package cli

import "github.com/spf13/cobra"

// newPackagesCmd is a stub for Phase 0. It will be implemented in Phase 3.
//
// See nodeup.md §7 (Global Package Migration). Subcommands:
//
//	nodeup packages snapshot   — capture global npm packages for the active version
//	nodeup packages list       — list packages for a snapshot
//	nodeup packages restore    — re-install packages from a snapshot
//	nodeup packages diff       — diff two snapshots
func newPackagesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "packages",
		Short: "Manage global npm package snapshots",
		Long: `Manage global npm package snapshots — capture, list, restore, and
diff the set of globally installed packages per Node.js version.

Implemented in Phase 3.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "snapshot",
		Short: "Snapshot the active version's global packages",
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.Println("nodeup packages snapshot — not yet implemented (Phase 3)")
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List packages from a snapshot",
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.Println("nodeup packages list — not yet implemented (Phase 3)")
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "restore",
		Short: "Re-install packages from a snapshot",
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.Println("nodeup packages restore — not yet implemented (Phase 3)")
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "diff",
		Short: "Diff two snapshots",
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.Println("nodeup packages diff — not yet implemented (Phase 3)")
			return nil
		},
	})

	return cmd
}