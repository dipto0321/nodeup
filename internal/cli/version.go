package cli

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

// newVersionCmd returns the `nodeup version` subcommand.
//
// It prints the version, git commit, build date, and runtime info (Go
// version, OS, architecture). The --check flag is reserved for a future
// self-update mechanism (out of scope for v1.0.0 per nodeup.md §3).
func newVersionCmd(version, commit, date string) *cobra.Command {
	var check bool

	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print nodeup version information",
		Long: `Print the nodeup version, build metadata, and runtime environment.

Example:
  nodeup version
  nodeup version --check    # check for a newer release (planned v2)`,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()

			fmt.Fprintf(out, "nodeup version %s\n", version)
			fmt.Fprintf(out, "  commit:     %s\n", commit)
			fmt.Fprintf(out, "  built:      %s\n", date)
			fmt.Fprintf(out, "  go version: %s\n", runtime.Version())
			fmt.Fprintf(out, "  platform:   %s/%s\n", runtime.GOOS, runtime.GOARCH)

			if check {
				// Self-update is a v2 feature (see nodeup.md §3 "Out of Scope").
				// We intentionally do nothing here but make the flag a
				// no-op rather than failing, so user scripts that pre-set
				// the flag don't break.
				fmt.Fprintln(out, "\n(update check is not yet implemented)")
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&check, "check", false, "check for a newer nodeup release")

	return cmd
}