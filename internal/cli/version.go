package cli

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/dipto0321/nodeup/internal/ui"
)

// newVersionCmd returns the `nodeup version` subcommand.
//
// It prints the version, git commit, build date, and runtime info (Go
// version, OS, architecture). The --check flag is reserved for a future
// self-update mechanism (out of scope for v1.0.0).
//
// This command is the PoC for the internal/ui migration (#74 PR1): it
// reads its ui.Writer out of cmd.Context() (stashed there by root.go's
// PersistentPreRunE) and routes every line through Writer.Println so
// the byte-level shape stays identical in both PlainMode and
// FancyMode. Future PRs (#74 PR2/3/4) will migrate the other
// commands.
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
			w := writerFromCmd(cmd)

			w.Println(fmt.Sprintf("nodeup version %s", version))
			w.Println(fmt.Sprintf("  commit:     %s", commit))
			w.Println(fmt.Sprintf("  built:      %s", date))
			w.Println(fmt.Sprintf("  go version: %s", runtime.Version()))
			w.Println(fmt.Sprintf("  platform:   %s/%s", runtime.GOOS, runtime.GOARCH))

			if check {
				// Self-update is a v2 feature — out of scope for v1.
				// We intentionally do nothing here but make the flag a
				// no-op rather than failing, so user scripts that pre-set
				// the flag don't break.
				w.Println("")
				w.Println("(update check is not yet implemented)")
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&check, "check", false, "check for a newer nodeup release")

	return cmd
}

// writerFromCmd pulls the ui.Writer out of cobra's context. root.go's
// PersistentPreRunE stashes a Writer under writerCtxKey so subcommands
// don't have to know how DecideMode was resolved or how to construct a
// Writer themselves. If the key is missing (e.g., a test constructs a
// leaf command in isolation without going through NewRootCmd), we fall
// back to a PlainMode writer backed by cmd.OutOrStdout/ErrOrStderr so
// tests still get sensible output.
func writerFromCmd(cmd *cobra.Command) ui.Writer {
	// cobra's Context() panics when called on a Command that was
	// never SetContext'd — which is the case for many unit tests
	// that build a bare cobra.Command. Guard the lookup so the
	// fallback path below always works.
	if ctx := cmd.Context(); ctx != nil {
		if v, ok := ctx.Value(writerCtxKey{}).(ui.Writer); ok && v != nil {
			return v
		}
	}
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()
	if errOut == nil {
		errOut = out
	}
	return ui.NewWriter(ui.PlainMode, out, errOut)
}

// writerCtxKey is the unexported context.Context key used by
// root.go to stash the active ui.Writer for subcommands. The empty
// struct type guarantees no collisions with keys from other packages.
type writerCtxKey struct{}
