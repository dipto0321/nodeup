package packages

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Masterminds/semver/v3"

	"github.com/dipto0321/nodeup/internal/platform"
)

// Package represents a globally installed npm package.
type Package struct {
	Name    string
	Version string
}

// SnapshotData represents the on-disk snapshot format.
type SnapshotData struct {
	Manager     string    `json:"manager"`
	NodeVersion string    `json:"node_version"`
	Packages    []Package `json:"packages"`
}

// Snapshot captures all globally installed npm packages for a given Node version.
// It calls `npm ls -g --json --depth=0` and writes the result to disk.
func Snapshot(ctx context.Context, managerName string, version semver.Version) error {
	output, err := runNpmListGlobal(ctx)
	if err != nil {
		return fmt.Errorf("list global packages: %w", err)
	}

	var npmOutput struct {
		Dependencies map[string]struct {
			Version string `json:"version"`
		} `json:"dependencies"`
	}
	if err := json.Unmarshal(output, &npmOutput); err != nil {
		return fmt.Errorf("parse npm output: %w", err)
	}

	pkgs := parsePackages(npmOutput.Dependencies)
	snap := SnapshotData{
		Manager:     managerName,
		NodeVersion: version.String(),
		Packages:    pkgs,
	}

	return saveSnapshot(snap)
}

func runNpmListGlobal(ctx context.Context) ([]byte, error) {
	res, err := platform.RunShell(ctx, "npm", "ls", "-g", "--json", "--depth=0")
	if err != nil {
		return nil, err
	}
	return []byte(res.Stdout), nil
}

func parsePackages(deps map[string]struct {
	Version string `json:"version"`
}) []Package {
	pkgs := make([]Package, 0, len(deps))
	for name, info := range deps {
		// Skip bundled packages
		if shouldSkipPackage(name) {
			continue
		}
		pkgs = append(pkgs, Package{Name: name, Version: info.Version})
	}
	return pkgs
}

// Default skip list matches docs/configuration.md
var skipPackages = map[string]bool{
	"npm":      true,
	"corepack": true,
	"npx":      true,
}

func shouldSkipPackage(name string) bool {
	return skipPackages[name] || strings.HasPrefix(name, "@npm:")
}

func snapshotPath(managerName, version string) (string, error) {
	dir, err := platform.SnapshotsDir()
	if err != nil {
		return "", err
	}
	filename := fmt.Sprintf("%s-%s.json", managerName, version)
	return filepath.Join(dir, filename), nil
}

func saveSnapshot(s SnapshotData) error {
	path, err := snapshotPath(s.Manager, s.NodeVersion)
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal snapshot: %w", err)
	}

	return os.WriteFile(path, data, 0o644)
}

// Restore reinstalls packages from a snapshot.
func Restore(ctx context.Context, managerName string, version semver.Version) error {
	path, err := snapshotPath(managerName, version.String())
	if err != nil {
		return err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read snapshot: %w", err)
	}

	var s SnapshotData
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("parse snapshot: %w", err)
	}

	return installPackages(ctx, s.Packages)
}

func installPackages(ctx context.Context, pkgs []Package) error {
	for _, pkg := range pkgs {
		_, err := platform.RunShell(ctx, "npm", "install", "-g", pkgSpec(pkg))
		if err != nil {
			return fmt.Errorf("install %s: %w", pkg.Name, err)
		}
	}
	return nil
}

func pkgSpec(p Package) string {
	if p.Version == "" {
		return p.Name
	}
	return fmt.Sprintf("%s@%s", p.Name, p.Version)
}

// LoadSnapshot reads a snapshot file.
func LoadSnapshot(managerName, version string) (SnapshotData, error) {
	path, err := snapshotPath(managerName, version)
	if err != nil {
		return SnapshotData{}, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return SnapshotData{}, err
	}

	var s SnapshotData
	if err := json.Unmarshal(data, &s); err != nil {
		return SnapshotData{}, fmt.Errorf("parse snapshot: %w", err)
	}

	return s, nil
}

// ListSnapshots returns all snapshot files in the snapshots directory.
func ListSnapshots() ([]SnapshotData, error) {
	dir, err := platform.SnapshotsDir()
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var result []SnapshotData
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		var s SnapshotData
		if err := json.Unmarshal(data, &s); err != nil {
			continue
		}
		result = append(result, s)
	}

	return result, nil
}

// DiffSnapshots compares two snapshots and returns added/removed packages.
func DiffSnapshots(v1, v2 []Package) (added, removed []Package) {
	v1Map := make(map[string]bool)
	for _, p := range v1 {
		v1Map[p.Name] = true
	}

	for _, p := range v2 {
		if !v1Map[p.Name] {
			added = append(added, p)
		}
	}

	v2Map := make(map[string]bool)
	for _, p := range v2 {
		v2Map[p.Name] = true
	}

	for _, p := range v1 {
		if !v2Map[p.Name] {
			removed = append(removed, p)
		}
	}

	return added, removed
}
