package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dipto0321/nodeup/internal/packages"
	"github.com/dipto0321/nodeup/internal/ui"
)

// These tests cover the CLI half of the stale-sentinel fix (#141).
//
// The bug: the "Detected an interrupted upgrade" warning fired on every
// nodeup invocation whenever a sentinel file existed — even after the
// snapshot it pointed at had been deleted. The warning pointed at a
// dead file and told the user to run a command that could not succeed,
// and `nodeup packages cleanup` never touched the sentinel, so there
// was no way to dismiss it.
//
// The fix: the warning validates its own advice. A sentinel whose
// snapshot is gone is cleared and reported as stale; a sentinel whose
// snapshot is alive keeps the existing behavior exactly.

// redirectSentinelDataDir points nodeup's data directory at a temp dir
// so these tests never see (or touch) the developer's real sentinel.
// Mirrors redirectCleanupDataDir in cleanup_snapshots_test.go — the
// helpers aren't shared because the packages package has its own copy
// and the cli package can't import a test helper.
func redirectSentinelDataDir(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()

	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("APPDATA", "")
	t.Setenv("USERPROFILE", tmp)
	t.Setenv("HOME", tmp)
	return tmp
}

// writeSentinelAt plants an upgrade marker pointing at the given
// snapshot path and returns the sentinel.
func writeSentinelAt(t *testing.T, snapshotPath string) packages.UpgradeSentinel {
	t.Helper()
	return writeSentinelFor(t, "fnm", snapshotPath)
}

// writeSentinelFor plants an upgrade marker for a specific manager.
func writeSentinelFor(t *testing.T, manager, snapshotPath string) packages.UpgradeSentinel {
	t.Helper()
	s := packages.UpgradeSentinel{
		StartedAt:    time.Now().UTC().Truncate(time.Second),
		Manager:      manager,
		OldVersion:   "26.10.0",
		NewVersion:   "26.12.1",
		SnapshotPath: snapshotPath,
	}
	if err := packages.WriteSentinel(s); err != nil {
		t.Fatalf("WriteSentinel: %v", err)
	}
	return s
}

// newWarningWriter builds a PlainMode writer over the two buffers and
// returns it plus the buffers.
func newWarningWriter() (ui.Writer, *bytes.Buffer, *bytes.Buffer) {
	var out, errBuf bytes.Buffer
	return ui.NewWriter(ui.PlainMode, &out, &errBuf), &out, &errBuf
}

// TestWarnInterruptedUpgrade_StaleSentinelIsClearedAndNotWarned is the
// core regression test for #141.
//
// A sentinel whose snapshot no longer exists must NOT produce the
// "Detected an interrupted upgrade" warning, and it must be removed
// from disk so the warning does not come back on the next invocation.
func TestWarnInterruptedUpgrade_StaleSentinelIsClearedAndNotWarned(t *testing.T) {
	redirectSentinelDataDir(t)

	// The snapshot path deliberately points at a file that isn't there.
	missing := filepath.Join(t.TempDir(), "fnm-26.10.0.json")
	writeSentinelAt(t, missing)

	w, out, errBuf := newWarningWriter()
	warnInterruptedUpgrade(w)

	combined := out.String() + errBuf.String()
	if strings.Contains(combined, "Detected an interrupted upgrade") {
		t.Errorf("stale sentinel still produced the interrupted-upgrade warning:\n%s", combined)
	}
	if strings.Contains(combined, "packages restore --from") {
		t.Errorf("stale sentinel still produced a resume hint that cannot work:\n%s", combined)
	}
	if !strings.Contains(combined, "stale") {
		t.Errorf("expected the stale marker to be reported, got:\n%s", combined)
	}

	// And the sentinel must actually be gone from disk.
	after, err := packages.OrphanedSentinel()
	if err != nil {
		t.Fatalf("OrphanedSentinel after warning: %v", err)
	}
	if after != nil {
		t.Errorf("stale sentinel = %+v, want nil — it should have been cleared", after)
	}
}

// TestWarnInterruptedUpgrade_LiveSentinelStillWarns is the
// non-regression pin.
//
// When the snapshot IS still on disk, the sentinel represents a genuine
// interrupted migration. The warning and the resume hint must still
// appear, and the sentinel must NOT be cleared — the #46 contract says
// a failed restore leaves the breadcrumb armed so the user can replay
// the migration.
func TestWarnInterruptedUpgrade_LiveSentinelStillWarns(t *testing.T) {
	redirectSentinelDataDir(t)

	// A real snapshot file on disk.
	dir := t.TempDir()
	snap := filepath.Join(dir, "fnm-26.10.0.json")
	if err := os.WriteFile(snap, []byte(`{"manager":"fnm","packages":[]}`), 0o644); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}
	writeSentinelAt(t, snap)

	w, out, errBuf := newWarningWriter()
	warnInterruptedUpgrade(w)

	combined := out.String() + errBuf.String()
	if !strings.Contains(combined, "Detected an interrupted upgrade") {
		t.Errorf("live sentinel did not produce the warning:\n%s", combined)
	}
	if !strings.Contains(combined, "packages restore --from") {
		t.Errorf("live sentinel did not produce the resume hint:\n%s", combined)
	}
	if !strings.Contains(combined, snap) {
		t.Errorf("resume hint does not mention the snapshot path %q:\n%s", snap, combined)
	}

	// The sentinel must still be there — this is the resume breadcrumb.
	after, err := packages.OrphanedSentinel()
	if err != nil {
		t.Fatalf("OrphanedSentinel: %v", err)
	}
	if after == nil {
		t.Error("live sentinel was cleared — the resume breadcrumb was lost")
	}
}

// TestWarnInterruptedUpgrade_NoSentinelIsSilent covers the quiet case:
// no sentinel at all means no output whatsoever.
func TestWarnInterruptedUpgrade_NoSentinelIsSilent(t *testing.T) {
	redirectSentinelDataDir(t)

	w, out, errBuf := newWarningWriter()
	warnInterruptedUpgrade(w)

	if combined := out.String() + errBuf.String(); combined != "" {
		t.Errorf("expected no output with no sentinel, got:\n%s", combined)
	}
}

// TestWarnInterruptedUpgrade_DoesNotClobberAFreshSentinel guards against the
// fix being too eager: if the sentinel on disk changed since the
// warning inspected it (a concurrent upgrade armed a fresh one), the
// fresh one must survive.
//
// The deterministic version of this race — the one where the swap lands
// exactly between our read and our claim — lives in
// internal/packages/stale_sentinel_test.go, where the claim seam is.
// This test covers the CLI's half: it must not delete a sentinel whose
// snapshot is alive.
func TestWarnInterruptedUpgrade_DoesNotClobberAFreshSentinel(t *testing.T) {
	redirectSentinelDataDir(t)

	// A fresh sentinel whose snapshot IS alive. The warning must not
	// clear it.
	live := filepath.Join(t.TempDir(), "nvm-20.0.0.json")
	if err := os.WriteFile(live, []byte(`{"manager":"nvm","packages":[]}`), 0o644); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}
	writeSentinelFor(t, "nvm", live)

	w, _, _ := newWarningWriter()
	warnInterruptedUpgrade(w)

	after, err := packages.OrphanedSentinel()
	if err != nil {
		t.Fatalf("OrphanedSentinel: %v", err)
	}
	if after == nil {
		t.Fatal("the live sentinel was deleted — the warning clobbered it")
	}
	if after.Manager != "nvm" {
		t.Errorf("sentinel Manager = %q, want nvm", after.Manager)
	}
}

// TestCleanupCmd_StaleSentinelFlagClearsIt covers the explicit
// recovery path: `nodeup packages cleanup --stale-sentinel`.
//
// The sentinel lives at <DataDir>/upgrade-in-progress.json — outside
// the snapshots and reports directories the command otherwise works
// on — so without an explicit flag there was no way for the user to
// dismiss a stale marker by hand.
func TestCleanupCmd_StaleSentinelFlagClearsIt(t *testing.T) {
	redirectSentinelDataDir(t)

	missing := filepath.Join(t.TempDir(), "fnm-26.10.0.json")
	writeSentinelAt(t, missing)

	cmd := newCleanupCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--stale-sentinel"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("cleanup --stale-sentinel: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Cleared the stale upgrade marker") {
		t.Errorf("output %q does not confirm the marker was cleared", out)
	}

	after, err := packages.OrphanedSentinel()
	if err != nil {
		t.Fatalf("OrphanedSentinel after: %v", err)
	}
	if after != nil {
		t.Errorf("sentinel = %+v, want nil after --stale-sentinel", after)
	}
}

// TestCleanupCmd_StaleSentinelFlagRefusesLiveSentinel is the safety
// pin: a sentinel whose snapshot still exists represents a genuine
// interrupted migration. Clearing it on request would throw away the
// only breadcrumb telling the user how to replay their globals.
func TestCleanupCmd_StaleSentinelFlagRefusesLiveSentinel(t *testing.T) {
	redirectSentinelDataDir(t)

	dir := t.TempDir()
	snap := filepath.Join(dir, "fnm-26.10.0.json")
	if err := os.WriteFile(snap, []byte(`{"manager":"fnm","packages":[]}`), 0o644); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}
	writeSentinelAt(t, snap)

	cmd := newCleanupCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--stale-sentinel"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("cleanup --stale-sentinel: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "genuine interrupted upgrade") {
		t.Errorf("output %q does not explain why the live marker was kept", out)
	}

	after, err := packages.OrphanedSentinel()
	if err != nil {
		t.Fatalf("OrphanedSentinel after: %v", err)
	}
	if after == nil {
		t.Fatal("the live sentinel was cleared — the resume breadcrumb was lost")
	}
}

// TestCleanupCmd_StaleSentinelFlagNoMarkerIsQuiet covers the
// already-clean case.
func TestCleanupCmd_StaleSentinelFlagNoMarkerIsQuiet(t *testing.T) {
	redirectSentinelDataDir(t)

	cmd := newCleanupCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--stale-sentinel"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("cleanup --stale-sentinel: %v", err)
	}

	if out := buf.String(); !strings.Contains(out, "No upgrade marker found") {
		t.Errorf("output %q does not report that there was no marker", out)
	}
}
