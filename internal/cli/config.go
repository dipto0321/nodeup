package cli

import "github.com/spf13/cobra"

// newConfigCmd is a stub for Phase 0. It will be implemented in Phase 5.
//
// The config file lives at ~/.nodeup/config.yaml and stores the manager
// preference, tracked channels, migration strategy, cleanup policy,
// and cache TTL.
func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage nodeup configuration",
		Long: `Manage the nodeup configuration file (~/.nodeup/config.yaml).

Implemented in Phase 5.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a config value",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.Println("nodeup config set — not yet implemented (Phase 5)")
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "get <key>",
		Short: "Get a config value",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.Println("nodeup config get — not yet implemented (Phase 5)")
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "show",
		Short: "Show the merged effective config",
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.Println("nodeup config show — not yet implemented (Phase 5)")
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "init",
		Short: "Write a scaffolded config file to ~/.nodeup/config.yaml",
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.Println("nodeup config init — not yet implemented (Phase 5)")
			return nil
		},
	})

	return cmd
}
