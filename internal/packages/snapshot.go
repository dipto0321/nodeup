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

// runShell is the package-level seam used by Restore/RestoreFromSnapshot
// to invoke `npm install -g`. Tests overwrite it to capture arguments
// and return canned output (success or per-package failure) without
// spawning a real subprocess. Production code never reassigns it.
//
// Signature matches platform.RunShell so a direct assignment works.
var runShell = platform.RunShell

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

// SnapshotPath returns the conventional on-disk path of a snapshot
// file given the manager name and Node version, e.g.
// "<DataDir>/snapshots/fnm-20.10.0.json". It does not check that the
// file exists — it only computes the path. Exported so the upgrade
// command can record the snapshot path inside the upgrade-in-progress
// sentinel; the restore CLI likewise uses it to look up a snapshot by
// name.
func SnapshotPath(managerName, version string) (string, error) {
	return snapshotPath(managerName, version)
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

// RestoreOutcome bundles the parsed snapshot metadata with the
// per-package install results so callers can build an accurate
// MigrationReport without having to re-parse the snapshot file
// themselves. The Snapshot field is populated even on partial failure
// (it's available the moment we successfully unmarshal the file).
type RestoreOutcome struct {
	// Snapshot is the parsed snapshot we replayed. Useful for the
	// MigrationReport's Manager / FromVersion fields.
	Snapshot SnapshotData
	// Results carries one entry per package that installPackages
	// attempted (success or failure). On partial failure, every
	// attempted package is present — including the ones that failed.
	Results []PackageResult
}

// Restore reinstalls packages from a snapshot.
//
// Returns the parsed snapshot and the per-package results so the
// caller can build a MigrationReport (see internal/packages/report.go).
// When one or more packages failed to install, the returned error
// wraps the per-package failures and the returned slice still
// contains an entry for every package — including the failures — so
// callers can persist the full outcome for the user to inspect.
func Restore(ctx context.Context, managerName string, version semver.Version) (RestoreOutcome, error) {
	path, err := snapshotPath(managerName, version.String())
	if err != nil {
		return RestoreOutcome{}, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return RestoreOutcome{}, fmt.Errorf("read snapshot: %w", err)
	}

	var s SnapshotData
	if err := json.Unmarshal(data, &s); err != nil {
		return RestoreOutcome{}, fmt.Errorf("parse snapshot: %w", err)
	}

	results, err := installPackages(ctx, s.Packages)
	return RestoreOutcome{Snapshot: s, Results: results}, err
}

// RestoreFromSnapshot reinstalls the packages contained in an arbitrary
// snapshot file on disk. Unlike Restore, it does not look the snapshot up
// by name in <DataDir>/snapshots/ — it reads exactly the path given.
//
// This is the explicit-replay entrypoint used by `nodeup packages restore
// --from <path>` after an interrupted upgrade has been detected via the
// sentinel file. The path can be:
//
//   - the snapshot the upgrade wrote (its absolute path is recorded in
//     the sentinel under snapshot_path), or
//   - any user-provided snapshot file the user wants to replay.
//
// We deliberately use os.ReadFile directly instead of LoadSnapshot so
// the path does not have to live under <DataDir>/snapshots.
//
// Returns the parsed snapshot and per-package results, plus a non-nil
// error when at least one package failed. Callers can use the
// snapshot metadata to populate the MigrationReport accurately —
// even on the --from branch where the CLI never saw a manager name
// from the user.
func RestoreFromSnapshot(ctx context.Context, path string) (RestoreOutcome, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return RestoreOutcome{}, fmt.Errorf("read snapshot %s: %w", path, err)
	}

	var s SnapshotData
	if err := json.Unmarshal(data, &s); err != nil {
		return RestoreOutcome{}, fmt.Errorf("parse snapshot %s: %w", path, err)
	}

	results, err := installPackages(ctx, s.Packages)
	return RestoreOutcome{Snapshot: s, Results: results}, err
}

// installPackages runs `npm install -g <pkg>` for each package in pkgs,
// recording a PackageResult per package. It LOOP-ALL — a failure on
// package N does NOT short-circuit packages N+1..M, so a single broken
// or yanked package no longer prevents the rest of the user's globals
// from migrating. Per-package npm failures are reflected in the
// returned results slice as Status="failed"; an aggregate error is
// returned when any package failed so callers can detect partial
// failure and persist a MigrationReport.
//
// Context cancellation still aborts the loop promptly: we check
// ctx.Err() at the top of each iteration. On cancellation, the
// returned results contain whatever we managed to record before the
// signal arrived, and the returned error is the ctx error itself
// (NOT a wrapped "N of M failed" message — the caller can branch on
// errors.Is(err, context.Canceled) for that path).
//
// Note: this intentionally diverges from the previous "return on first
// failure" behavior. That behavior is what issue #46 / #103 called
// out as a silent data-loss bug — restoring 30 globals and losing 27
// of them because package #3 was renamed.
func installPackages(ctx context.Context, pkgs []Package) ([]PackageResult, error) {
	results := make([]PackageResult, 0, len(pkgs))
	var failed int
	var firstFailedName string
	var firstFailedErr error

	for _, pkg := range pkgs {
		// Honor cancellation (Ctrl-C / parent-ctx Done) before each
		// shell-out so a single long install doesn't block the abort.
		if cerr := ctx.Err(); cerr != nil {
			return results, cerr
		}

		r := PackageResult{
			Name:             pkg.Name,
			Status:           "ok",
			AttemptedVersion: pkg.Version,
		}
		_, err := runShell(ctx, "npm", "install", "-g", pkgSpec(pkg))
		if err != nil {
			r.Status = "failed"
			r.Error = err.Error()
			failed++
			if firstFailedName == "" {
				firstFailedName = pkg.Name
				firstFailedErr = err
			}
		}
		results = append(results, r)
	}

	if failed > 0 {
		// Wrap the first failure so callers can still errors.Is / errors.As
		// on the original; the aggregate "N of M" message gives users the
		// headline number at a glance.
		return results, fmt.Errorf("%d of %d packages failed to install (first failure: %s): %w",
			failed, len(pkgs), firstFailedName, firstFailedErr)
	}
	return results, nil
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
