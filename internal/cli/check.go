package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/dipto0321/nodeup/internal/detector"
	"github.com/dipto0321/nodeup/internal/node"
	"github.com/dipto0321/nodeup/internal/ui"
)

// systemNodeJSON describes the on-disk `node` binary found on PATH,
// if any. Marshal-safe so it can sit inside the top-level check
// JSON envelope. nil in the envelope means "not probed" or "no
// node on PATH", distinguishing that from `path == ""` which can
// only arise if the probe itself succeeded but returned an empty
// path (a defensive guard we don't expect to surface).
type systemNodeJSON struct {
	Path    string `json:"path"`
	Kind    string `json:"kind"`
	Manager string `json:"manager,omitempty"`
}

// newCheckCmd implements `nodeup check` — show available LTS and Current versions.
// It fetches the nodejs.org/dist/index.json manifest and compares against
// installed versions (if a manager is detected).
func newCheckCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Check what Node.js LTS and Current versions are available",
		Long: `Check what Node.js LTS and Current versions are available from
nodejs.org without installing anything. Compares against installed versions.`,
		RunE: runCheck,
	}

	cmd.Flags().Bool("json", false, "output as JSON")
	cmd.Flags().Bool("offline", false, "use cached manifest only, don't hit the network")

	return cmd
}

func runCheck(cmd *cobra.Command, args []string) error {
	asJSON, _ := cmd.Flags().GetBool("json")
	offline, _ := cmd.Flags().GetBool("offline")

	var m node.Manifest
	var err error

	if offline {
		m, err = node.LoadCached()
		if err != nil {
			return fmt.Errorf("failed to load cached manifest: %w", err)
		}
	} else {
		// Ctx-aware variant: Ctrl-C cancels an in-flight fetch and
		// httpClient.Timeout bounds a hung nodejs.org response. See
		// #48.
		m, err = node.FetchManifestCtx(cmd.Context())
		if err != nil {
			return fmt.Errorf("failed to fetch manifest: %w", err)
		}
	}

	lts, err := m.LatestLTS()
	if err != nil {
		return fmt.Errorf("resolve LTS: %w", err)
	}

	current, err := m.LatestCurrent()
	if err != nil {
		return fmt.Errorf("resolve Current: %w", err)
	}

	// Get installed versions if a manager is available
	installed := detector.DetectAll()

	// Probe `node` on PATH and classify how it's installed. When
	// exactly one manager was detected we pass it to the classifier
	// so a manager-owned binary on PATH classifies as `manager`
	// rather than the path-only fallback (which would otherwise
	// surface an "unrecognized layout" for any node living under
	// ~/.fnm/ or similar — a perfectly normal manager install).
	// With zero or multiple managers we pass nil: nothing to
	// attribute, the path classifier handles it.
	var sysMgr detector.Manager
	if len(installed.Found) == 1 {
		sysMgr = installed.Found[0]
	}

	// The warning text is captured for both the JSON envelope and
	// the table renderer.
	var sysNode *systemNodeJSON
	if info, err := detector.ResolveSystemNode(cmd.Context(), sysMgr); err == nil {
		sysNode = &systemNodeJSON{
			Path:    info.Path,
			Kind:    info.Kind.String(),
			Manager: info.Manager,
		}
	}

	w := writerFromCmd(cmd)
	if asJSON {
		return outputCheckJSON(w, cmd.Context(), lts, current, installed, sysNode)
	}

	return outputCheckTable(w, cmd.Context(), lts, current, installed, sysNode)
}

func outputCheckJSON(w ui.Writer, ctx context.Context, lts, current *node.ManifestVersion, installed detector.Registry, sysNode *systemNodeJSON) error {
	type checkOutput struct {
		LTS        *node.ManifestVersion `json:"lts"`
		Current    *node.ManifestVersion `json:"current"`
		Installed  []string              `json:"installed"`
		SystemNode *systemNodeJSON       `json:"systemNode,omitempty"`
	}

	installedVersions := make([]string, 0)
	for _, mgr := range installed.Found {
		versions, err := mgr.ListInstalled(ctx)
		if err != nil {
			continue
		}
		for _, v := range versions {
			installedVersions = append(installedVersions, v.String())
		}
	}

	out := checkOutput{
		LTS:        lts,
		Current:    current,
		Installed:  installedVersions,
		SystemNode: sysNode,
	}

	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	w.Println(string(data))
	return nil
}

func outputCheckTable(w ui.Writer, ctx context.Context, lts, current *node.ManifestVersion, installed detector.Registry, sysNode *systemNodeJSON) error {
	w.Println("")
	w.Info(fmt.Sprintf("  LTS:     %s (released %s)", lts.Version, lts.Date))
	w.Info(fmt.Sprintf("  Current: %s (released %s)", current.Version, current.Date))
	w.Println("")

	if len(installed.Found) == 0 {
		w.Info("No Node.js version manager detected.")
	} else {
		w.Println("Installed versions:")
		for _, mgr := range installed.Found {
			versions, err := mgr.ListInstalled(ctx)
			if err != nil {
				w.Warn(fmt.Sprintf("  - %s: [error listing versions]", mgr.Name()))
				continue
			}
			w.Info(fmt.Sprintf("  - %s: %s", mgr.Name(), formatVersions(versions)))
		}
	}

	// Surface the on-PATH `node` classification. When sysNode is nil
	// the probe didn't run (or `which node` failed) — we say so
	// explicitly rather than staying silent, so the user has a
	// single source of truth for what nodeup sees.
	w.Println("")
	if sysNode == nil {
		w.Info("System node:  (could not probe `node` on PATH)")
		return nil
	}
	switch sysNode.Kind {
	case "manager":
		w.Info(fmt.Sprintf("System node:  %s (managed by %s)", sysNode.Path, sysNode.Manager))
	case "unknown":
		// Path matched no known layout. Don't print a long warning
		// here — `nodeup upgrade` is where the warning belongs.
		w.Info(fmt.Sprintf("System node:  %s (unrecognized layout)", sysNode.Path))
	default:
		// OS-package / snap / flatpak / homebrew-core. Render the
		// same warning text that `nodeup upgrade` would print, so
		// `check` is a useful diagnostic on its own. We capture
		// into a buffer rather than hitting stderr directly to keep
		// the table layout coherent.
		var buf strings.Builder
		_, _ = detector.WarnSystemNode(&buf, detector.SystemNodeInfo{
			Path:    sysNode.Path,
			Kind:    parseSystemNodeKind(sysNode.Kind),
			Manager: sysNode.Manager,
		})
		// Indent each rendered line by two spaces so it lines up
		// with the rest of the table block.
		for _, line := range strings.Split(strings.TrimRight(buf.String(), "\n"), "\n") {
			w.Info("  " + line)
		}
	}

	return nil
}

// parseSystemNodeKind round-trips a kind label produced by
// SystemNodeKind.String() back to the enum, so outputCheckTable can
// re-render the warning text without re-classifying the path. The
// mapping is intentionally exhaustive: any unknown label resolves
// to SystemNodeUnknown so the caller falls into the soft-warning
// branch.
func parseSystemNodeKind(s string) detector.SystemNodeKind {
	switch s {
	case "os-package":
		return detector.SystemNodeOSPackage
	case "snap":
		return detector.SystemNodeSnap
	case "flatpak":
		return detector.SystemNodeFlatpak
	case "homebrew-core":
		return detector.SystemNodeHomebrewCore
	case "manager":
		return detector.SystemNodeManaged
	default:
		return detector.SystemNodeUnknown
	}
}

func formatVersions(versions []semver.Version) string {
	if len(versions) == 0 {
		return "(none)"
	}
	out := ""
	for i, v := range versions {
		if i > 0 {
			out += ", "
		}
		out += v.String()
	}
	return out
}
