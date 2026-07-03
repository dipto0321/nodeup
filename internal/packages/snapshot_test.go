package packages

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Masterminds/semver/v3"
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
	restoreErr := Restore(context.Background(), "fnm", *v)
	if restoreErr == nil {
		t.Fatal("expected error for missing snapshot, got nil")
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
	if err := RestoreFromSnapshot(context.Background(), arbitraryPath); err != nil {
		t.Errorf("RestoreFromSnapshot: %v", err)
	}
}
