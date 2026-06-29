// Package main is the entry point for the nodeup CLI.
//
// nodeup is an automated Node.js version upgrade + global package migration
// tool. It detects which Node.js version manager is installed (fnm, nvm,
// Volta, asdf, mise, n, nodenv, nvm-windows), fetches the latest LTS and
// Current versions from the nodejs.org release API, snapshots global npm
// packages, installs the new Node versions, and migrates the packages over.
package main

import (
	"fmt"
	"os"

	"github.com/dipto0321/nodeup/internal/cli"
)

// version is set at build time via -ldflags "-X main.version=vX.Y.Z".
// It is intentionally a package-level variable so GoReleaser can inject the
// value during release builds. See .goreleaser.yaml.
var version = "dev"

// commit is the git short-hash of the build, also injected via -ldflags.
var commit = "none"

// date is the build timestamp in RFC3339 format, injected via -ldflags.
var date = "unknown"

func main() {
	cmd := cli.NewRootCmd(version, commit, date)

	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
