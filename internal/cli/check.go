package cli

import (
	"encoding/json"
	"fmt"

	"github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/dipto0321/nodeup/internal/detector"
	"github.com/dipto0321/nodeup/internal/node"
)

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
		m, err = node.FetchManifest()
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

	if asJSON {
		return outputCheckJSON(cmd, lts, current, installed)
	}

	return outputCheckTable(cmd, lts, current, installed)
}

func outputCheckJSON(cmd *cobra.Command, lts, current *node.ManifestVersion, installed detector.Registry) error {
	type checkOutput struct {
		LTS       *node.ManifestVersion `json:"lts"`
		Current   *node.ManifestVersion `json:"current"`
		Installed []string              `json:"installed"`
	}

	installedVersions := make([]string, 0)
	for _, m := range installed.Found {
		versions, err := m.ListInstalled()
		if err != nil {
			continue
		}
		for _, v := range versions {
			installedVersions = append(installedVersions, v.String())
		}
	}

	out := checkOutput{
		LTS:       lts,
		Current:   current,
		Installed: installedVersions,
	}

	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	cmd.Println(string(data))
	return nil
}

func outputCheckTable(cmd *cobra.Command, lts, current *node.ManifestVersion, installed detector.Registry) error {
	cmd.Println()
	cmd.Printf("  LTS:     %s (released %s)\n", lts.Version, lts.Date)
	cmd.Printf("  Current: %s (released %s)\n", current.Version, current.Date)
	cmd.Println()

	if len(installed.Found) == 0 {
		cmd.Println("No Node.js version manager detected.")
		return nil
	}

	cmd.Println("Installed versions:")
	for _, m := range installed.Found {
		versions, err := m.ListInstalled()
		if err != nil {
			cmd.Printf("  - %s: [error listing versions]\n", m.Name())
			continue
		}
		cmd.Printf("  - %s: %s\n", m.Name(), formatVersions(versions))
	}

	return nil
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
