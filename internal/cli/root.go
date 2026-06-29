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
	"github.com/spf13/cobra"
)

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