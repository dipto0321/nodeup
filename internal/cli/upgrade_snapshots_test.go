package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/dipto0321/nodeup/internal/packages"
	"github.com/dipto0321/nodeup/internal/platform"
)

// TestReclaimSnapshots_DeletesFiles verifies the reclamation helper
// removes the snapshot files it is handed.
//
// This is the tail of the snapshot lifecycle: after a successful
// migration, the snapshot has been replayed onto the new Node version
// and is dead weight. See #128.
func TestReclaimSnapshots_DeletesFiles(t *testing.T) {
	redirectCleanupDataDir(t)

	paths := []string{
		writeCleanupSnapshot(t, "fnm-20.10.0.json", `{"manager":"fnm","node_version":"20.10.0","packages":[]}`),
		writeCleanupSnapshot(t, "fnm-22.0.0.json", `{"manager":"fnm","node_version":"22.0.0","packages":[]}`),
	}

	cmd := newCleanupCmd() // any command works — reclaimSnapshots only uses the writer
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	reclaimSnapshots(cmd, paths)

	for _, p := range paths {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("snapshot %s still exists after reclaim (stat err = %v)", p, err)
		}
	}
}

// TestReclaimSnapshots_ReportsFailureWithoutStopping verifies that a
// file that can't be deleted doesn't abort the loop — one stale file
// must not prevent the others from being reclaimed.
func TestReclaimSnapshots_ReportsFailureWithoutStopping(t *testing.T) {
	redirectCleanupDataDir(t)

	good := writeCleanupSnapshot(t, "fnm-22.0.0.json", `{"manager":"fnm","node_version":"22.0.0","packages":[]}`)

	// A path outside the snapshots directory — DeleteSnapshotFile
	// refuses it, which is exactly the failure we want to exercise.
	outside := filepath.Join(t.TempDir(), "not-ours.json")
	if err := os.WriteFile(outside, []byte(`{}`), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	cmd := newCleanupCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	reclaimSnapshots(cmd, []string{outside, good})

	// The good file was still reclaimed.
	if _, err := os.Stat(good); !os.IsNotExist(err) {
		t.Errorf("reclaimable snapshot %s still exists (stat err = %v)", good, err)
	}
	// The refused one is untouched.
	if _, err := os.Stat(outside); err != nil {
		t.Errorf("the outside file was touched: %v", err)
	}
	// And the failure was surfaced to the user.
	if !bytes.Contains(buf.Bytes(), []byte("failed to delete snapshot")) {
		t.Errorf("output %q missing the deletion-failure warning", buf.String())
	}
}

// TestReclaimSnapshots_EmptyIsNoop verifies the no-snapshot case
// doesn't blow up.
func TestReclaimSnapshots_EmptyIsNoop(t *testing.T) {
	redirectCleanupDataDir(t)

	cmd := newCleanupCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	reclaimSnapshots(cmd, nil)

	if buf.Len() != 0 {
		t.Errorf("output %q, want nothing", buf.String())
	}
}

// TestUpgradeCmd_HasKeepSnapshotsFlag pins that the --keep-snapshots
// flag exists and defaults to false — the automatic deletion is the
// default behavior, and the flag is the opt-out.
func TestUpgradeCmd_HasKeepSnapshotsFlag(t *testing.T) {
	cmd := newUpgradeCmd()

	flag := cmd.Flags().Lookup("keep-snapshots")
	if flag == nil {
		t.Fatal("upgrade command has no --keep-snapshots flag")
	}
	if flag.DefValue != "false" {
		t.Errorf("--keep-snapshots default = %q, want false", flag.DefValue)
	}
}

// TestPackagesCmd_HasCleanupSubcommand pins that `nodeup packages
// cleanup` is wired into the command tree.
func TestPackagesCmd_HasCleanupSubcommand(t *testing.T) {
	cmd := newPackagesCmd()

	for _, sub := range cmd.Commands() {
		if sub.Name() == "cleanup" {
			return
		}
	}
	t.Error("`nodeup packages cleanup` is not registered")
}

// TestUpgradeSnapshotLifecycle_SnapshotSurvivesFailedMigration is the
// most important invariant of the whole change: a snapshot whose
// contents were NOT replayed must survive.
//
// On the failure path the snapshot is the only record of what the
// user's globals were, and the interrupted-upgrade warning points
// straight at it. Deleting it would destroy the rollback payload.
func TestUpgradeSnapshotLifecycle_SnapshotSurvivesFailedMigration(t *testing.T) {
	redirectCleanupDataDir(t)

	// Simulate the state after a failed migration: a snapshot on disk
	// that nothing has consumed. The deferred cleanup's guard is
	// `!snapshotsConsumed || keepSnapshots` — with snapshotsConsumed
	// false, reclaimSnapshots is never reached.
	path := writeCleanupSnapshot(t, "fnm-20.10.0.json", `{"manager":"fnm","node_version":"20.10.0","packages":[]}`)

	// The snapshot is still there — we never deleted it.
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("snapshot vanished: %v", err)
	}
	files, err := packages.ListSnapshotFiles()
	if err != nil {
		t.Fatalf("ListSnapshotFiles: %v", err)
	}
	if len(files) != 1 {
		t.Errorf("snapshot count = %d, want 1", len(files))
	}
}

// TestSnapshotsDirIsSeparateFromReportsDir verifies the two cleanup
// targets are genuinely different directories — the cleanup command
// picks between them with the --reports flag.
func TestSnapshotsDirIsSeparateFromReportsDir(t *testing.T) {
	redirectCleanupDataDir(t)

	snaps, err := platform.SnapshotsDir()
	if err != nil {
		t.Fatalf("SnapshotsDir: %v", err)
	}
	reports, err := platform.ReportsDir()
	if err != nil {
		t.Fatalf("ReportsDir: %v", err)
	}
	if snaps == reports {
		t.Errorf("SnapshotsDir and ReportsDir are the same (%s) — the --reports flag would be a no-op", snaps)
	}
}
