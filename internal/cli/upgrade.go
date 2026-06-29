package cli

import (
	"github.com/spf13/cobra"
)

// newUpgradeCmd is a stub for Phase 0. It will be implemented in Phase 4.
//
// See nodeup.md §4 (Core Algorithm) and §9 (CLI Design) for the planned
// behavior: detect manager → fetch versions → diff → snapshot → install →
// migrate → cleanup.
func newUpgradeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade Node.js LTS and/or Current versions",
		Long: `Upgrade Node.js LTS and Current to the latest versions and
migrate your global npm packages across automatically.

Implemented in Phase 4 (see .puku/plans/nodeup_v1_build_plan_*.plan.md).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.Println("nodeup upgrade — not yet implemented (Phase 4)")
			cmd.Println("See .puku/plans/ for the build plan.")
			return nil
		},
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