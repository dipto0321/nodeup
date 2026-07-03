package cli

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/dipto0321/nodeup/internal/detector"
)

// installedEntryJSON is a single manager's contributions to the
// `nodeup list --json` envelope. We mirror the field names chosen by
// `nodeup check --json` (`manager`, `versions`, `error`) so consumers
// piping both commands through jq don't have to special-case.
//
// `Versions` is rendered as strings (semver) rather than numbers
// because semver pre-release tags (`20.0.0-rc.1`, …) are not valid
// JSON numbers and we'd otherwise silently drop them on the wire.
//
// `Error` is omitted on success (nil) so the envelope stays compact
// for the common case; it surfaces only when ListInstalled failed.
type installedEntryJSON struct {
	Manager  string   `json:"manager"`
	Versions []string `json:"versions"`
	Error    string   `json:"error,omitempty"`
}

// listOutputJSON is the top-level envelope emitted by `nodeup list
// --json`. The shape is intentionally close to (but not identical to)
// `nodeup check --json`: `check` reports what nodejs.org offers, `list`
// reports what's installed locally, so the field names diverge there.
// `Installed` carries one entry per detected manager.
type listOutputJSON struct {
	Installed []installedEntryJSON `json:"installed"`
	// Current is the version currently active (via Manager.Current()),
	// when detectable. nil in the envelope means "not probed" or
	// "manager does not implement Current()".
	Current *string `json:"current,omitempty"`
}

// newListCmd implements `nodeup list` — show every Node.js version the
// detected manager(s) have installed, with `--json` for machine
// consumption. Mirrors the canonical `check.go` pattern (ResolveAll /
// per-manager loop / JSON-or-table rendering) so the two commands
// share a consistent scriptable shape.
func newListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List installed Node.js versions via the detected manager",
		Long: `List installed Node.js versions via the detected version manager
(fnm, nvm, Volta, asdf, mise, n, nodenv, nvm-windows).

With --json, emits a stable envelope consumable by jq/yq/etc. The
human-readable default prints one line per detected manager with
versions sorted ascending.`,
		RunE: runList,
	}

	cmd.Flags().Bool("json", false, "output as JSON")
	cmd.Flags().String("manager", "", "force a specific manager (fnm, nvm, volta, asdf, mise, n, nodenv, nvm-windows); default is auto-detect")

	return cmd
}

func runList(cmd *cobra.Command, args []string) error {
	asJSON, _ := cmd.Flags().GetBool("json")
	managerFlag, _ := cmd.Flags().GetString("manager")

	installed := detector.DetectAll()

	// When the user passes --manager, narrow the registry down to
	// only that one (matching ResolveManager's existing behavior on
	// upgrade/check). We don't call ResolveManager directly because
	// `list` is multi-manager by default — we want to keep the
	// "show me everything you found" UX when no flag is passed.
	var selected []detector.Manager
	if managerFlag != "" {
		match, ok := findManagerByName(installed, managerFlag)
		if !ok {
			return fmt.Errorf("manager %q not detected (found: %s)", managerFlag, registryNames(installed))
		}
		selected = []detector.Manager{match}
	} else {
		selected = installed.Found
	}

	// Per-manager listing. Errors from ListInstalled are captured as
	// strings in the JSON envelope, never returned, so a single
	// manager failing doesn't blackhole the whole listing — same
	// soft-fail policy as check.go.
	entries := make([]installedEntryJSON, 0, len(selected))
	// Same pattern as before — collect rich per-manager data. We also
	// probe the active version on the first manager that supports it
	// (one active Node per machine — typically one manager owns it).
	var active *string
	for _, m := range selected {
		versions, err := m.ListInstalled()
		entry := installedEntryJSON{Manager: m.Name()}
		if err != nil {
			entry.Error = err.Error()
		} else {
			sort.Slice(versions, func(i, j int) bool {
				return versions[i].Compare(&versions[j]) < 0
			})
			entry.Versions = make([]string, 0, len(versions))
			for _, v := range versions {
				entry.Versions = append(entry.Versions, v.String())
			}
		}
		entries = append(entries, entry)

		// Capture the active version from the first manager that
		// answers. This is best-effort: managers that don't implement
		// Current() return an error, which we silently drop.
		if active == nil {
			if cur, cerr := m.Current(); cerr == nil {
				s := cur.String()
				active = &s
			}
		}
	}

	if asJSON {
		return outputListJSON(cmd, listOutputJSON{Installed: entries, Current: active})
	}
	return outputListTable(cmd, entries)
}

func outputListJSON(cmd *cobra.Command, out listOutputJSON) error {
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	cmd.Println(string(data))
	return nil
}

func outputListTable(cmd *cobra.Command, entries []installedEntryJSON) error {
	cmd.Println()
	if len(entries) == 0 {
		cmd.Println("No Node.js version manager detected.")
		cmd.Println("Install one of: fnm, nvm, Volta, asdf, mise, n, nodenv, nvm-windows.")
		return nil
	}
	for _, e := range entries {
		if e.Error != "" {
			cmd.Printf("  %s: [error listing versions: %s]\n", e.Manager, e.Error)
			continue
		}
		if len(e.Versions) == 0 {
			cmd.Printf("  %s: (no versions installed)\n", e.Manager)
			continue
		}
		cmd.Printf("  %s: %s\n", e.Manager, formatListVersions(e.Versions))
	}
	return nil
}

// formatListVersions renders a comma-separated list of versions. We
// keep this separate from check.go's formatVersions (which operates
// on []semver.Version) because JSON-built entries give us strings.
// If a list is empty the caller already prints "(no versions
// installed)" so we can assume len > 0 here.
func formatListVersions(versions []string) string {
	out := ""
	for i, v := range versions {
		if i > 0 {
			out += ", "
		}
		out += v
	}
	return out
}

// findManagerByName scans a Registry and returns the Manager whose
// Name() matches the user's --manager flag (case-insensitive). Returns
// (nil, false) when no match is found; the caller surfaces the error.
//
// We resolve to a real Manager (not just a name string) so the rest
// of runList can call ListInstalled/Current on it directly without
// re-resolving via ResolveManager (which also implements the
// "preferred = empty → auto" semantics we don't want here).
func findManagerByName(reg detector.Registry, name string) (detector.Manager, bool) {
	want := lower(name)
	for _, m := range reg.Found {
		if lower(m.Name()) == want {
			return m, true
		}
	}
	return nil, false
}

// registryNames returns the comma-separated list of detected manager
// names. Used for "manager X not detected (found: …)" error messages.
func registryNames(reg detector.Registry) string {
	out := ""
	for i, m := range reg.Found {
		if i > 0 {
			out += ", "
		}
		out += m.Name()
	}
	return out
}

func lower(s string) string {
	out := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		out[i] = c
	}
	return string(out)
}
