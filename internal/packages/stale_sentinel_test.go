package packages

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// These tests cover the "stale sentinel" class of bug: the upgrade
// marker outliving the snapshot it points at.
//
// The sentinel's contract is "there is a migration you can replay; the
// payload for it lives at SnapshotPath". Two things can delete the
// payload out from under it:
//
//   - the upgrade flow's own auto-reclaim, which removes the snapshot
//     files once the migration has replayed them (#128)
//   - `nodeup packages cleanup`, which lets the user delete any
//     snapshot by hand
//
// When that happens the marker keeps firing an "interrupted upgrade"
// warning that points at a dead file and tells the user to run a
// command that cannot succeed. See issue #141.

// writeSnapshotAt drops a snapshot file at an absolute path and returns
// it. The package already has a writeSnapshotFile helper, but that one
// always writes into the snapshots directory; these tests need to place
// a file at an arbitrary path (and to place a file at a path that does
// NOT exist), so we need the absolute-path variant.
func writeSnapshotAt(t *testing.T, path string) string {
	t.Helper()
	if err := os.WriteFile(path, []byte(`{"manager":"fnm","packages":[]}`), 0o644); err != nil {
		t.Fatalf("write snapshot %s: %v", path, err)
	}
	return path
}

// TestSentinelSnapshotExists_TrueWhenPresent is the happy path: a
// sentinel pointing at a snapshot that IS on disk reports true. This is
// the genuine interrupted-upgrade case, and the caller must keep
// warning about it — the #46 contract depends on it.
func TestSentinelSnapshotExists_TrueWhenPresent(t *testing.T) {
	redirectDataDir(t)

	dir := t.TempDir()
	snap := writeSnapshotAt(t, filepath.Join(dir, "fnm-20.10.0.json"))

	s := &UpgradeSentinel{Manager: "fnm", SnapshotPath: snap}
	if !SentinelSnapshotExists(s) {
		t.Errorf("SentinelSnapshotExists = false, want true (snapshot is on disk)")
	}
}

// TestSentinelSnapshotExists_FalseWhenMissing is the core regression
// test for #141: the snapshot is gone, so the sentinel is stale and the
// caller must NOT print a resume hint.
func TestSentinelSnapshotExists_FalseWhenMissing(t *testing.T) {
	redirectDataDir(t)

	dir := t.TempDir()
	snap := filepath.Join(dir, "fnm-20.10.0.json")

	s := &UpgradeSentinel{Manager: "fnm", SnapshotPath: snap}
	if SentinelSnapshotExists(s) {
		t.Errorf("SentinelSnapshotExists = true, want false (snapshot does not exist)")
	}
}

// TestSentinelSnapshotExists_FalseWhenPathEmpty covers the sentinel
// with no recorded snapshot path. There is nothing to replay, so there
// is nothing to warn about.
func TestSentinelSnapshotExists_FalseWhenPathEmpty(t *testing.T) {
	redirectDataDir(t)

	if SentinelSnapshotExists(&UpgradeSentinel{Manager: "fnm"}) {
		t.Errorf("SentinelSnapshotExists = true, want false (no snapshot path recorded)")
	}
}

// TestSentinelSnapshotExists_FalseWhenNil guards the nil case — the
// caller must not have to nil-check before calling.
func TestSentinelSnapshotExists_FalseWhenNil(t *testing.T) {
	redirectDataDir(t)

	if SentinelSnapshotExists(nil) {
		t.Errorf("SentinelSnapshotExists(nil) = true, want false")
	}
}

// TestSentinelSnapshotExists_FalseWhenPathIsDirectory covers the
// "a directory sits where the snapshot should be" case. The restore
// command cannot read a directory, so the hint would still be dead.
func TestSentinelSnapshotExists_FalseWhenPathIsDirectory(t *testing.T) {
	redirectDataDir(t)

	dir := t.TempDir()
	sub := filepath.Join(dir, "fnm-20.10.0.json")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	s := &UpgradeSentinel{Manager: "fnm", SnapshotPath: sub}
	if SentinelSnapshotExists(s) {
		t.Errorf("SentinelSnapshotExists = true, want false (path is a directory)")
	}
}

// TestRemoveSentinelIfUnchanged_RemovesMatching pins the happy path:
// when the file on disk still describes the same upgrade the caller
// inspected, it is deleted.
func TestRemoveSentinelIfUnchanged_RemovesMatching(t *testing.T) {
	redirectDataDir(t)

	written := UpgradeSentinel{
		StartedAt:    time.Now().UTC().Truncate(time.Second),
		Manager:      "fnm",
		OldVersion:   "26.10.0",
		NewVersion:   "26.12.1",
		SnapshotPath: "/gone/fnm-26.10.0.json",
	}
	if err := WriteSentinel(written); err != nil {
		t.Fatalf("WriteSentinel: %v", err)
	}

	// Read it back the way the CLI does, so we compare against exactly
	// what a caller would hold.
	loaded, err := OrphanedSentinel()
	if err != nil {
		t.Fatalf("OrphanedSentinel: %v", err)
	}
	if loaded == nil {
		t.Fatal("OrphanedSentinel returned nil after write")
	}

	removed, rerr := RemoveSentinelIfUnchanged(loaded)
	if rerr != nil {
		t.Fatalf("RemoveSentinelIfUnchanged: %v", rerr)
	}
	if !removed {
		t.Fatal("RemoveSentinelIfUnchanged = false, want true (sentinel unchanged)")
	}

	// And the file must actually be gone.
	after, err := OrphanedSentinel()
	if err != nil {
		t.Fatalf("OrphanedSentinel after removal: %v", err)
	}
	if after != nil {
		t.Errorf("OrphanedSentinel = %+v, want nil after removal", after)
	}
}

// TestRemoveSentinelIfUnchanged_LeavesDifferentSentinel is the
// race-safety pin.
//
// The warning fires on EVERY nodeup invocation, so between a caller
// reading the sentinel and deleting it, a concurrent `nodeup upgrade`
// could arm a FRESH sentinel whose snapshot is alive. An unconditional
// delete would clobber that live breadcrumb.
//
// The compare-and-delete must detect the change and leave the new
// sentinel in place.
func TestRemoveSentinelIfUnchanged_LeavesDifferentSentinel(t *testing.T) {
	redirectDataDir(t)

	// The stale one the caller inspects.
	stale := &UpgradeSentinel{
		StartedAt:    time.Now().UTC().Truncate(time.Second),
		Manager:      "fnm",
		OldVersion:   "26.10.0",
		SnapshotPath: "/gone/fnm-26.10.0.json",
	}
	if err := WriteSentinel(*stale); err != nil {
		t.Fatalf("WriteSentinel: %v", err)
	}

	// A concurrent upgrade arms a FRESH sentinel, overwriting the stale
	// one. This one has a live snapshot.
	fresh := UpgradeSentinel{
		StartedAt:    time.Now().UTC().Truncate(time.Second).Add(time.Minute),
		Manager:      "nvm",
		OldVersion:   "20.0.0",
		NewVersion:   "22.0.0",
		SnapshotPath: "/alive/nvm-20.0.0.json",
	}
	if err := WriteSentinel(fresh); err != nil {
		t.Fatalf("WriteSentinel (fresh): %v", err)
	}

	removed, rerr := RemoveSentinelIfUnchanged(stale)
	if rerr != nil {
		t.Fatalf("RemoveSentinelIfUnchanged: %v", rerr)
	}
	if removed {
		t.Error("RemoveSentinelIfUnchanged = true, want false (sentinel changed on disk)")
	}

	// The FRESH sentinel must survive — its snapshot is alive, so the
	// resume breadcrumb is still valid.
	after, err := OrphanedSentinel()
	if err != nil {
		t.Fatalf("OrphanedSentinel after: %v", err)
	}
	if after == nil {
		t.Fatal("the fresh sentinel was deleted — the compare-and-delete clobbered it")
	}
	if after.Manager != "nvm" || after.SnapshotPath != fresh.SnapshotPath {
		t.Errorf("sentinel = %+v, want the fresh one (%+v)", after, fresh)
	}
}

// TestRemoveSentinelIfUnchanged_ConcurrentUpgradeWins is the
// deterministic race test.
//
// The window is narrow — between our read of the sentinel and our
// claim of it — so triggering it by timing would be flaky. The
// `claimSentinelFile` seam lets us reproduce it exactly: the seam
// fires at the moment of the claim, which is precisely where a
// concurrent `nodeup upgrade` would arm its fresh sentinel.
//
// The fresh sentinel MUST survive. Clobbering it would destroy the
// resume breadcrumb for a migration the user genuinely needs to
// replay.
func TestRemoveSentinelIfUnchanged_ConcurrentUpgradeWins(t *testing.T) {
	redirectDataDir(t)

	stale := UpgradeSentinel{
		StartedAt:    time.Now().UTC().Truncate(time.Second),
		Manager:      "fnm",
		OldVersion:   "26.10.0",
		SnapshotPath: "/gone/fnm-26.10.0.json",
	}
	if err := WriteSentinel(stale); err != nil {
		t.Fatalf("WriteSentinel: %v", err)
	}

	fresh := UpgradeSentinel{
		StartedAt:    stale.StartedAt.Add(time.Minute),
		Manager:      "nvm",
		OldVersion:   "20.0.0",
		NewVersion:   "22.0.0",
		SnapshotPath: "/alive/nvm-20.0.0.json",
	}

	// Swap the seam for the duration of this test. The replacement
	// writes the fresh sentinel (simulating the concurrent upgrade
	// winning the race) and then performs the real rename — so we
	// CLAIM the fresh sentinel, not the stale one.
	orig := claimSentinelFile
	t.Cleanup(func() { claimSentinelFile = orig })

	claimSentinelFile = func(path, claimPath string) error {
		if err := WriteSentinel(fresh); err != nil {
			return err
		}
		return os.Rename(path, claimPath)
	}

	removed, rerr := RemoveSentinelIfUnchanged(&stale)
	if rerr != nil {
		t.Fatalf("RemoveSentinelIfUnchanged: %v", rerr)
	}
	if removed {
		t.Error("RemoveSentinelIfUnchanged = true, want false (we claimed a different sentinel)")
	}

	// The FRESH sentinel must be back in place — put back, not
	// destroyed. This is the property that matters: the concurrent
	// upgrade's breadcrumb must not be lost.
	after, err := OrphanedSentinel()
	if err != nil {
		t.Fatalf("OrphanedSentinel after: %v", err)
	}
	if after == nil {
		t.Fatal("the fresh sentinel was deleted — the concurrent upgrade lost its breadcrumb")
	}
	if after.Manager != "nvm" || after.SnapshotPath != fresh.SnapshotPath {
		t.Errorf("sentinel = %+v, want the fresh one (%+v)", after, fresh)
	}
}

// TestRemoveSentinelIfUnchanged_NoOpWhenAbsent covers the case where
// the sentinel is already gone (someone else cleaned it up). The
// desired end state holds, so it is not an error.
func TestRemoveSentinelIfUnchanged_NoOpWhenAbsent(t *testing.T) {
	redirectDataDir(t)

	removed, rerr := RemoveSentinelIfUnchanged(&UpgradeSentinel{Manager: "fnm"})
	if rerr != nil {
		t.Fatalf("RemoveSentinelIfUnchanged on missing file: %v", rerr)
	}
	if removed {
		t.Error("RemoveSentinelIfUnchanged = true, want false (file was already absent)")
	}
}

// TestRemoveSentinelIfUnchanged_NilTarget covers the nil guard.
func TestRemoveSentinelIfUnchanged_NilTarget(t *testing.T) {
	redirectDataDir(t)

	removed, rerr := RemoveSentinelIfUnchanged(nil)
	if rerr != nil {
		t.Fatalf("RemoveSentinelIfUnchanged(nil): %v", rerr)
	}
	if removed {
		t.Error("RemoveSentinelIfUnchanged(nil) = true, want false")
	}
}
