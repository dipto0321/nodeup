package cli

import "github.com/spf13/cobra"

// newCheckCmd is a stub for Phase 0. It will be implemented in Phase 2.
//
// It calls the nodejs.org/dist/index.json endpoint, resolves the latest
// LTS and Current versions, and prints them alongside the user's
// installed versions without making any changes.
func newCheckCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Check what Node.js LTS and Current versions are available",
		Long: `Check what Node.js LTS and Current versions are available from
nodejs.org without installing anything. Compares against installed versions.

Implemented in Phase 2.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.Println("nodeup check — not yet implemented (Phase 2)")
			return nil
		},
	}

	cmd.Flags().Bool("json", false, "output as JSON")

	return cmd
}
