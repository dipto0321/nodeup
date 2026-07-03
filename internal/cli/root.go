// Package cli wires the cobra command tree for nodeup.
//
// The CLI is intentionally thin: every subcommand lives in its own file
// (upgrade.go, list.go, check.go, ...) and registers itself against the root
// command returned by NewRootCmd. This keeps the entrypoint small and the
// per-command code discoverable.
//
// All user-facing output (prompts, spinners, summary tables) flows through
// internal/ui — never fmt.Println in business logic.
package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/dipto0321/nodeup/internal/packages"
	"github.com/dipto0321/nodeup/internal/ui"
)

// warnInterruptedUpgrade checks for an orphaned upgrade sentinel and
// prints a hint to stderr if one is found. We use PersistentPreRunE so
// this fires on EVERY subcommand invocation — including `nodeup version`
// or `nodeup packages list` — without each leaf having to remember.
//
// Output goes to stderr so it does not pollute machine-readable stdout
// (e.g., `nodeup list | jq .`). Errors from OrphanedSentinel other than
// "no sentinel" are deliberately swallowed: a corrupted sentinel file
// is a cosmetic issue and should not prevent the user's actual command
// from running.
func warnInterruptedUpgrade(w ui.Writer) {
	s, err := packages.OrphanedSentinel()
	if err != nil || s == nil {
		return
	}
	w.Warn(fmt.Sprintf("Detected an interrupted upgrade (snapshot: %s, started: %s).",
		s.SnapshotPath, s.StartedAt.Format("2006-01-02T15:04:05Z07:00")))
	w.Warn(fmt.Sprintf("To resume: `nodeup packages restore --from %s`",
		s.SnapshotPath))
}

// NewRootCmd builds the root `nodeup` command with all subcommands attached.
//
// version, commit, and date are build-time variables (see cmd/nodeup/main.go)
// injected via -ldflags during release builds. They appear in
// `nodeup version` and in GoReleaser's archive metadata.
func NewRootCmd(version, commit, date string) *cobra.Command {
	root := &cobra.Command{
		Use:   "nodeup",
		Short: "Automated Node.js version upgrade + global package migration",
		Long: `nodeup keeps your Node.js installation up to date — without
the manual churn.

It auto-detects your version manager (fnm, nvm, Volta, asdf, mise, n,
nodenv, nvm-windows), fetches the latest LTS and Current versions from
nodejs.org, installs them, and migrates your global npm packages
across so you don't lose anything.

Common workflows:
  nodeup upgrade           Upgrade both LTS and Current
  nodeup upgrade --lts     Upgrade only LTS
  nodeup check             See what's available, install nothing
  nodeup list              List installed Node versions
  nodeup packages snapshot Snapshot global packages for the active version

Docs: https://github.com/dipto0321/nodeup`,
		SilenceUsage:  true, // don't dump --help on every error
		SilenceErrors: true, // we print errors ourselves in main()
		// PersistentPreRunE runs before every subcommand. Two jobs:
		//   1. Resolve the ui.Writer for this invocation (single
		//      DecideMode call, result cached on the cmd.Context()).
		//   2. Fire the interrupted-upgrade warning.
		// cobra's docs guarantee PersistentPreRunE runs for the root
		// command too (e.g., bare `nodeup` showing help), so we get
		// both behaviors for free without needing PersistentPreRun
		// alongside.
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			noColor, _ := cmd.Flags().GetBool("no-color")
			out := cmd.OutOrStdout()
			errOut := cmd.ErrOrStderr()
			if errOut == nil {
				errOut = out
			}
			w := ui.NewWriter(ui.DecideMode(noColor), out, errOut)
			ctx := context.WithValue(cmd.Context(), writerCtxKey{}, w)
			cmd.SetContext(ctx)

			warnInterruptedUpgrade(w)
			return nil
		},
	}

	// Persistent flags shared by every subcommand. These are intentionally
	// minimal — per-command flags live on the leaf commands.
	root.PersistentFlags().BoolP("verbose", "v", false, "enable verbose logging")
	root.PersistentFlags().Bool("no-color", false, "disable colored output")

	// Register subcommands.
	root.AddCommand(newVersionCmd(version, commit, date))
	root.AddCommand(newUpgradeCmd())
	root.AddCommand(newCheckCmd())
	root.AddCommand(newListCmd())
	root.AddCommand(newPackagesCmd())
	root.AddCommand(newConfigCmd())

	return root
}
