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
//
// # THE SENTINEL CAN OUTLIVE ITS SNAPSHOT
//
// The sentinel's contract is "there is a migration you can replay; the
// payload for it lives at SnapshotPath". That contract only holds while
// the snapshot survives — and two things can delete it out from under
// the sentinel: the upgrade flow's own auto-reclaim (#128) and
// `nodeup packages cleanup`.
//
// When the snapshot is gone, the warning below becomes actively
// harmful. It points at a dead file and tells the user to run a
// command that cannot possibly succeed:
//
//	⚠ Detected an interrupted upgrade (snapshot: .../fnm-26.10.0.json, ...)
//	⚠ To resume: `nodeup packages restore --from .../fnm-26.10.0.json`
//	Error: restore failed: read snapshot ...: no such file or directory
//
// And since the sentinel lives at <DataDir>/upgrade-in-progress.json —
// outside the snapshots directory — `nodeup packages cleanup` never
// touched it, so the warning fired on every invocation forever with no
// way to dismiss it.
//
// So we validate the sentinel before acting on it:
//
//   - Snapshot still present → genuine interrupted upgrade. Print the
//     warning and the resume hint, exactly as before. (This is the
//     #46 contract: a failed restore MUST leave the sentinel armed so
//     the migration can be replayed.)
//   - Snapshot gone → the sentinel is stale. Say so plainly, clear it,
//     and do NOT print a resume hint that cannot work.
func warnInterruptedUpgrade(w ui.Writer) {
	s, err := packages.OrphanedSentinel()
	if err != nil || s == nil {
		return
	}

	// The snapshot is still there → this is a real interrupted upgrade.
	// Print the resume hint. Nothing is cleared: the sentinel is the
	// breadcrumb that lets the user replay the migration.
	if packages.SentinelSnapshotExists(s) {
		w.Warn(fmt.Sprintf("Detected an interrupted upgrade (snapshot: %s, started: %s).",
			s.SnapshotPath, s.StartedAt.Format("2006-01-02T15:04:05Z07:00")))
		w.Warn(fmt.Sprintf("To resume: `nodeup packages restore --from %s`",
			s.SnapshotPath))
		return
	}

	// The sentinel points at a snapshot that no longer exists. Clear it
	// so the warning stops firing, and tell the user why.
	//
	// We use the compare-and-delete variant so we cannot clobber a
	// FRESH sentinel armed by a concurrent `nodeup upgrade` between our
	// read and our delete.
	removed, rerr := packages.RemoveSentinelIfUnchanged(s)
	if rerr != nil {
		// Best-effort. A failure to remove the file is cosmetic — the
		// user's command still runs. We stay quiet here rather than
		// printing a second warning on top of the one below, which
		// would be noise; the stale sentinel will be retried on the
		// next invocation.
		return
	}
	if !removed {
		// The sentinel on disk changed since we read it — a concurrent
		// upgrade armed a fresh one. Leave it entirely alone.
		return
	}

	w.Warn(fmt.Sprintf("Cleared a stale upgrade marker (the snapshot it pointed at no longer exists, so there was nothing to resume). It was last used on %s.",
		s.StartedAt.Format("2006-01-02")))
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
nodenv, vite-plus, nvm-windows), fetches the latest LTS and Current
versions from nodejs.org, installs them, and migrates your global npm
packages across so you don't lose anything.

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
