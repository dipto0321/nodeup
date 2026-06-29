package cli

import "github.com/spf13/cobra"

// newListCmd is a stub for Phase 0. It will be implemented in Phase 1.
//
// See nodeup.md §5 (Version Manager Detection Engine). The command
// delegates to the detected manager's native "list" command, or
// enumerates versions directly from the manager's data directory.
func newListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List installed Node.js versions via the detected manager",
		Long: `List installed Node.js versions via the detected version manager
(fnm, nvm, Volta, asdf, mise, n, nodenv, nvm-windows).

Implemented in Phase 1.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.Println("nodeup list — not yet implemented (Phase 1)")
			return nil
		},
	}

	cmd.Flags().Bool("json", false, "output as JSON")

	return cmd
}