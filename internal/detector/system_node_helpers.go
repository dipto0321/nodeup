// system_node_helpers.go holds the test-seam helpers used by
// system_node.go. Splitting them into their own file keeps the
// production logic in system_node.go uncluttered while still
// making the seams discoverable to test authors.
//
// Both helpers wrap package-level vars so tests can stub them
// with t.Cleanup, matching the pattern used by runShell in fnm.go
// and runScript in nvm.go.
package detector

import "os"

// getenv is a package-level seam around os.Getenv used by
// managerManagedRoots. Tests overwrite it to inject canned
// environment-variable values without mutating process state.
// Production code never reassigns it.
//
// Signature matches os.Getenv so a direct assignment works.
var getenv = os.Getenv

// userHomeDir is a package-level seam around os.UserHomeDir used
// by managerManagedRoots. Tests overwrite it to return canned
// home-directory paths without touching the real filesystem.
// Production code never reassigns it.
//
// Signature matches os.UserHomeDir so a direct assignment works.
var userHomeDir = os.UserHomeDir
