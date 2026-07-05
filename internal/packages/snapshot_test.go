package packages

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Masterminds/semver/v3"

	"github.com/dipto0321/nodeup/internal/platform"
)

// TestSnapshot_StoredVersionKeyMatchesArg verifies the on-disk path
// convention used by Snapshot() / SnapshotPath(): a snapshot taken for
// Node 20.10.0 lives at <DataDir>/snapshots/fnm-20.10.0.json — the
// file name is keyed on the version argument, not on the new version
// the upgrade flow is about to install.
//
// This test pins down the contract that the upgrade-loop fix in #42
// relies on: the upgrade flow snapshots the *previously installed*
// versions, then on restore uses RestoreFromSnapshot(<that snapshot
// path>) rather than looking up a snapshot file keyed on the
// newly-installed version (which would silently miss and no-op the
// migration).
func TestSnapshot_StoredVersionKeyMatchesArg(t *testing.T) {
	redirectDataDir(t)

	// No installed npm, no packages — Snapshot() reads
	// `npm ls -g --json --depth=0` and will fail in a test
	// environment. We bypass Snapshot() and call saveSnapshot()
	// directly with a known SnapshotData struct so the test stays
	// hermetic.
	snap := SnapshotData{
		Manager:     "fnm",
		NodeVersion: "20.10.0",
		Packages:    []Package{},
	}
	if err := saveSnapshot(snap); err != nil {
		t.Fatalf("saveSnapshot: %v", err)
	}

	// Conventional path should exist for the OLD-installed version.
	wantPath, err := SnapshotPath("fnm", "20.10.0")
	if err != nil {
		t.Fatalf("SnapshotPath old-version: %v", err)
	}
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("snapshot file not created at conventional path %s: %v", wantPath, err)
	}

	// The version-key the upgrade loop used to look up (BEFORE the
	// #42 fix) was the NEW installed version (20.11.0). That key
	// deliberately misses — verifying the miss here pins down the
	// regression that the fix prevents.
	newVerPath, err := SnapshotPath("fnm", "20.11.0")
	if err != nil {
		t.Fatalf("SnapshotPath new-version: %v", err)
	}
	if _, statErr := os.Stat(newVerPath); statErr == nil {
		t.Fatalf("new-version snapshot file should NOT exist at %s — if it does, the version-key mismatch regression has resurfaced", newVerPath)
	}
}

// TestRestore_LooksUpByArgVersion covers the existing behavior of
// Restore(ctx, manager, version): it loads the conventional
// <DataDir>/snapshots/<mgr>-<version>.json file. We don't exercise
// installPackages' actual `npm install -g` call (that needs a working
// npm binary), but verify the read-and-parse path returns a wrapped
// "read snapshot" error when the file does not exist — this is the
// failure mode that used to silently no-op the upgrade migration
// when the upgrade-loop called Restore with the new (not-yet-existing)
// version key.
func TestRestore_LooksUpByArgVersion(t *testing.T) {
	redirectDataDir(t)

	// There is no snapshot for fnm 99.99.99 — Restore must surface
	// that as a wrapped read error, not a silent success.
	v, err := semver.NewVersion("99.99.99")
	if err != nil {
		t.Fatalf("parse version: %v", err)
	}
	outcome, restoreErr := Restore(context.Background(), "fnm", *v)
	if restoreErr == nil {
		t.Fatal("expected error for missing snapshot, got nil")
	}
	if len(outcome.Results) != 0 {
		t.Errorf("expected no results on missing snapshot, got %d", len(outcome.Results))
	}
	if !strings.Contains(restoreErr.Error(), "read snapshot") {
		t.Errorf("error %q does not mention read snapshot failure", restoreErr)
	}
	// The user-facing error should be wrapped — `read snapshot:` is
	// the human-readable prefix and the underlying fs.PathError must
	// remain accessible via errors.Is so callers can branch on
	// fs.ErrNotExist. Stdlib's *PathError satisfies this naturally,
	// so we don't add an Is() method on our error type.
	if !errors.Is(restoreErr, fs.ErrNotExist) {
		t.Errorf("error %q does not unwrap to fs.ErrNotExist; callers cannot branch on missing-file", restoreErr)
	}
}

// TestRestoreFromSnapshot_ReadsSourcePathRegardlessOfNodeVersion is the
// regression test for the upgrade-loop fix: RestoreFromSnapshot reads
// the snapshot at the path it's given regardless of the Node version
// key in the filename. This is what the upgrade flow relies on —
// the snapshot path comes from the sentinel, which records the
// latest-INSTALLED-version's snapshot (e.g., fnm-20.10.0.json), not a
// fresh-version snapshot keyed on the just-installed Node.
func TestRestoreFromSnapshot_ReadsSourcePathRegardlessOfNodeVersion(t *testing.T) {
	redirectDataDir(t)

	// Write a snapshot at an arbitrary path (does not follow the
	// `<mgr>-<ver>.json` convention — RestoreFromSnapshot is
	// explicitly path-based).
	tmp := t.TempDir()
	arbitraryPath := filepath.Join(tmp, "old-installed-set.json")
	snap := SnapshotData{
		Manager:     "fnm",
		NodeVersion: "20.10.0",   // the OLD installed version's key
		Packages:    []Package{}, // no packages → install loop is a no-op
	}
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(arbitraryPath, data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// RestoreFromSnapshot should succeed: the install loop iterates
	// over zero packages and exits cleanly. This proves that the
	// path-based restore does NOT depend on the version-key in the
	// filename matching some "new version" expectation — it just
	// reads what's at the path.
	outcome, err := RestoreFromSnapshot(context.Background(), arbitraryPath)
	if err != nil {
		t.Errorf("RestoreFromSnapshot: %v", err)
	}
	if len(outcome.Results) != 0 {
		t.Errorf("expected no results for empty snapshot, got %d (%+v)", len(outcome.Results), outcome.Results)
	}
	// The parsed snapshot must round-trip back through RestoreOutcome —
	// the CLI uses this to populate MigrationReport metadata even on
	// the --from branch where no CLI-side manager was given.
	if outcome.Snapshot.Manager != "fnm" || outcome.Snapshot.NodeVersion != "20.10.0" {
		t.Errorf("outcome.Snapshot not populated: %+v", outcome.Snapshot)
	}
}

// --- #103: continue-past-failure + report write + ctx abort -------------

// withRunShellFake replaces the package-level runShell seam for the
// duration of a test, restoring the original (production) value on
// cleanup. Each test that needs to inject canned success/failure
// behavior calls this once; tests must not run in parallel because
// the seam is package-global.
func withRunShellFake(t *testing.T, fn func(ctx context.Context, name string, args ...string) (*platform.RunResult, error)) {
	t.Helper()
	orig := runShell
	runShell = fn
	t.Cleanup(func() { runShell = orig })
}

// TestInstallPackages_ContinuesPastFailure is the regression pin for
// issue #103 / #46: a failure on package N must NOT abort packages
// N+1..M. Each per-package outcome (ok vs failed) is recorded in
// the returned results slice; the aggregate error wraps the first
// failure so callers can detect "partial failure" but still have a
// per-package record for the MigrationReport.
func TestInstallPackages_ContinuesPastFailure(t *testing.T) {
	pkgs := []Package{
		{Name: "ok-1", Version: "1.0.0"},
		{Name: "broken", Version: "2.0.0"}, // this one fails
		{Name: "ok-2", Version: "3.0.0"},
		{Name: "broken-2", Version: "4.0.0"}, // and this one
		{Name: "ok-3", Version: "5.0.0"},
	}

	var calls atomic.Int32
	withRunShellFake(t, func(_ context.Context, name string, args ...string) (*platform.RunResult, error) {
		// Sanity-check: we only expect `npm install -g <spec>` calls.
		if name != "npm" || len(args) < 3 || args[0] != "install" || args[1] != "-g" {
			t.Errorf("unexpected shell-out: %s %v", name, args)
		}
		calls.Add(1)
		// Fail any package whose name starts with "broken". pkgSpec
		// emits "<name>@<version>", so we split on "@" and inspect the
		// name half. Matching by name avoids the prefix-overlap bug
		// where "broken" matches inside "broken-2@...".
		spec := args[2]
		nameOnly := spec
		if i := strings.Index(spec, "@"); i >= 0 {
			nameOnly = spec[:i]
		}
		if strings.HasPrefix(nameOnly, "broken") {
			return nil, errors.New("404 not found: " + spec)
		}
		return &platform.RunResult{Stdout: "+ " + spec}, nil
	})

	results, err := installPackages(context.Background(), pkgs)

	// 1. The aggregate error wraps the first failure and reports the count.
	if err == nil {
		t.Fatal("expected aggregate error for partial failure, got nil")
	}
	if !strings.Contains(err.Error(), "2 of 5 packages failed") {
		t.Errorf("aggregate error %q does not report 2 of 5", err)
	}
	if !strings.Contains(err.Error(), "broken@2.0.0") {
		t.Errorf("aggregate error %q does not name the first failed package", err)
	}

	// 2. The first underlying failure is reachable via errors.Is / errors.As
	// so callers can branch on it.
	if !strings.Contains(err.Error(), "404 not found: broken@2.0.0") {
		t.Errorf("aggregate error %q does not wrap the original failure", err)
	}

	// 3. ALL packages were attempted — the loop did NOT short-circuit.
	if got := calls.Load(); got != 5 {
		t.Errorf("expected 5 npm install calls, got %d", got)
	}

	// 4. Results carry the right per-package statuses.
	if len(results) != 5 {
		t.Fatalf("expected 5 results, got %d", len(results))
	}
	wantStatuses := []string{"ok", "failed", "ok", "failed", "ok"}
	for i, r := range results {
		if r.Status != wantStatuses[i] {
			t.Errorf("results[%d].Status = %q, want %q (name=%q)", i, r.Status, wantStatuses[i], r.Name)
		}
		if r.Name != pkgs[i].Name {
			t.Errorf("results[%d].Name = %q, want %q", i, r.Name, pkgs[i].Name)
		}
		if r.Error == "" && r.Status == "failed" {
			t.Errorf("results[%d] failed but Error is empty", i)
		}
		if r.Error != "" && r.Status == "ok" {
			t.Errorf("results[%d] ok but Error=%q", i, r.Error)
		}
	}
}

// TestInstallPackages_AllSucceed returns no error and a clean results
// slice when every package installs cleanly. Pins the happy-path
// contract that callers rely on to compute "restoreSucceeded".
func TestInstallPackages_AllSucceed(t *testing.T) {
	pkgs := []Package{
		{Name: "a", Version: "1.0.0"},
		{Name: "b", Version: "2.0.0"},
	}
	withRunShellFake(t, func(_ context.Context, _ string, _ ...string) (*platform.RunResult, error) {
		return &platform.RunResult{Stdout: "+ok"}, nil
	})

	results, err := installPackages(context.Background(), pkgs)
	if err != nil {
		t.Fatalf("expected nil error on all-success, got %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	for i, r := range results {
		if r.Status != "ok" {
			t.Errorf("results[%d].Status = %q, want ok", i, r.Status)
		}
	}
}

// TestInstallPackages_ContextCancelsMidLoop covers the Ctrl-C path:
// we cancel the ctx before iteration N, and the function MUST return
// whatever results it accumulated so far plus the ctx error (NOT the
// "N of M failed" aggregate). Callers can branch on
// errors.Is(err, context.Canceled) to distinguish user-abort from
// per-package failure.
func TestInstallPackages_ContextCancelsMidLoop(t *testing.T) {
	pkgs := []Package{
		{Name: "a", Version: "1.0.0"},
		{Name: "b", Version: "2.0.0"},
		{Name: "c", Version: "3.0.0"},
		{Name: "d", Version: "4.0.0"},
	}

	ctx, cancel := context.WithCancel(context.Background())

	var calls atomic.Int32
	withRunShellFake(t, func(_ context.Context, _ string, _ ...string) (*platform.RunResult, error) {
		n := calls.Add(1)
		// Cancel after the second successful install — packages
		// 3 and 4 must NOT run.
		if n >= 2 {
			cancel()
		}
		return &platform.RunResult{Stdout: "+ok"}, nil
	})

	results, err := installPackages(ctx, pkgs)

	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	// We can't strictly pin "exactly 2" — the cancel races with the
	// shell-out — but we MUST not have run all 4.
	if got := calls.Load(); got >= 4 {
		t.Errorf("expected fewer than 4 shell-outs after cancel, got %d", got)
	}
	// Results contain whatever we managed before the cancel; status is
	// "ok" for each completed install.
	for i, r := range results {
		if r.Status != "ok" {
			t.Errorf("results[%d].Status = %q, want ok", i, r.Status)
		}
	}
}

// TestInstallPackages_EmptyList is the trivial boundary: no packages,
// no shell-outs, no error. Pins the "fresh install, nothing to
// migrate" case the upgrade flow relies on for first-time runs.
func TestInstallPackages_EmptyList(t *testing.T) {
	withRunShellFake(t, func(_ context.Context, _ string, _ ...string) (*platform.RunResult, error) {
		t.Errorf("runShell should not be called for empty pkgs")
		return nil, nil
	})
	results, err := installPackages(context.Background(), nil)
	if err != nil {
		t.Errorf("empty pkgs: unexpected error %v", err)
	}
	if len(results) != 0 {
		t.Errorf("empty pkgs: expected no results, got %d", len(results))
	}
}

// TestMigrationReport_SavePersistsAndPathMatches is the wire-up test
// for #103 part 2: every restore — success or partial failure — must
// write a MigrationReport file to <DataDir>/reports/, and the path
// Save() actually writes to must equal Path() (so the CLI can print
// the same path back to the user).
func TestMigrationReport_SavePersistsAndPathMatches(t *testing.T) {
	redirectDataDir(t)

	report := NewMigrationReport("fnm", "20.10.0", "20.11.0")
	report.AddResult(PackageResult{Name: "typescript", Status: "ok", AttemptedVersion: "5.0.0"})
	report.AddResult(PackageResult{Name: "broken", Status: "failed", AttemptedVersion: "1.0.0", Error: "ENOENT"})

	wantPath, err := report.Path()
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	// Path() must be stable — calling it again must return the same
	// filename, so the CLI's "report at <path>" message matches the
	// file that Save() actually writes.
	wantPath2, err := report.Path()
	if err != nil {
		t.Fatalf("Path #2: %v", err)
	}
	if wantPath != wantPath2 {
		t.Errorf("Path() not stable across calls: %q vs %q", wantPath, wantPath2)
	}
	if err := report.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	data, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("read back report: %v", err)
	}
	var roundTrip MigrationReport
	if err := json.Unmarshal(data, &roundTrip); err != nil {
		t.Fatalf("parse report: %v", err)
	}
	if roundTrip.Manager != "fnm" || roundTrip.FromVersion != "20.10.0" || roundTrip.ToVersion != "20.11.0" {
		t.Errorf("round-trip metadata wrong: %+v", roundTrip)
	}
	if len(roundTrip.Results) != 2 {
		t.Fatalf("round-trip results len = %d, want 2", len(roundTrip.Results))
	}
	if roundTrip.Results[1].Status != "failed" || roundTrip.Results[1].Error != "ENOENT" {
		t.Errorf("failed-result not preserved: %+v", roundTrip.Results[1])
	}
}

// TestMigrationReport_PathCollisionResistance covers the sub-second
// collision-resistance edge case: two reports created in the same
// wall-clock second must produce distinct filenames. Without the
// random suffix (or with a non-crypto entropy source) a CI script
// that ran `nodeup upgrade` twice in a row would clobber the first
// report — defeating the "where did my migration go?" recovery flow.
func TestMigrationReport_PathCollisionResistance(t *testing.T) {
	redirectDataDir(t)

	// Two reports created back-to-back. If both report the same
	// timestamp (sub-second resolution), their filenames must still
	// differ.
	r1 := NewMigrationReport("fnm", "20.10.0", "20.11.0")
	r2 := NewMigrationReport("fnm", "20.10.0", "20.11.0")

	p1, err := r1.Path()
	if err != nil {
		t.Fatalf("r1.Path: %v", err)
	}
	p2, err := r2.Path()
	if err != nil {
		t.Fatalf("r2.Path: %v", err)
	}
	if p1 == p2 {
		t.Errorf("two reports within the same second produced the same path %q — collision risk", p1)
	}
	// Save both and verify both files exist independently on disk.
	if err := r1.Save(); err != nil {
		t.Fatalf("r1.Save: %v", err)
	}
	if err := r2.Save(); err != nil {
		t.Fatalf("r2.Save: %v", err)
	}
	if _, err := os.Stat(p1); err != nil {
		t.Errorf("r1 file missing after Save: %v", err)
	}
	if _, err := os.Stat(p2); err != nil {
		t.Errorf("r2 file missing after Save: %v", err)
	}
}
