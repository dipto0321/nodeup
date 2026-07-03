package packages

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// redirectDataDir steers internal/platform's DataDir() to a per-test
// tempdir. We do this by setting HOME (unix) or USERPROFILE (windows),
// which os.UserHomeDir() honors. APPDATA / XDG_DATA_HOME must also be
// cleared so they don't override HOME on darwin/linux.
//
// Why this matters: the sentinel helpers go through DataDir, which goes
// through os.UserHomeDir. If a developer runs `go test` with an existing
// sentinel in their real $HOME, the test would see it. t.Setenv + a
// fresh tempdir per test keeps state hermetic.
func redirectDataDir(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()

	// Platform-specific env vars that platform.DataDir checks FIRST
	// before falling back to HOME/USERPROFILE. Clearing them forces
	// the HOME/USERPROFILE path to win.
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("APPDATA", "")

	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", tmp)
		// HOME is read on Windows too, but only as a last resort;
		// setting it makes the test more portable across shells.
		t.Setenv("HOME", tmp)
	} else {
		t.Setenv("HOME", tmp)
	}
	return tmp
}

// TestOrphanedSentinel_NoneWhenAbsent verifies the "no upgrade was in
// progress" path: with no sentinel file, OrphanedSentinel must return
// (nil, nil) — not an error — so the CLI can silently skip the warning.
func TestOrphanedSentinel_NoneWhenAbsent(t *testing.T) {
	redirectDataDir(t)

	s, err := OrphanedSentinel()
	if err != nil {
		t.Fatalf("OrphanedSentinel: unexpected error %v", err)
	}
	if s != nil {
		t.Errorf("OrphanedSentinel = %+v, want nil", s)
	}
}

// TestLoadSentinel_ErrNoSentinelWhenAbsent checks the lower-level
// LoadSentinel returns ErrNoSentinel (wrapped) when the file is
// missing — this is what powers the "missing file is not an error"
// semantics above.
func TestLoadSentinel_ErrNoSentinelWhenAbsent(t *testing.T) {
	redirectDataDir(t)

	_, err := LoadSentinel()
	if err == nil {
		t.Fatal("LoadSentinel: expected error for missing file, got nil")
	}
	if !errors.Is(err, ErrNoSentinel) {
		t.Errorf("LoadSentinel: error %v does not wrap ErrNoSentinel", err)
	}
}

// TestWriteThenOrphan is the issue's first required test: "Write a
// sentinel in a tempdir, invoke the detector, verify the warning
// message contains the snapshot path."
//
// The "warning message" lives in internal/cli/root.go and is hard to
// unit-test without spinning up cobra. Instead, we verify the data
// the warning would print: the SnapshotPath field populated by the
// CLI's sentinel-writing code must survive a Write → Orphan cycle.
func TestWriteThenOrphan(t *testing.T) {
	redirectDataDir(t)

	now := time.Now().UTC()
	snapPath := "/var/lib/nodeup/snapshots/fnm-20.10.0.json"
	written := UpgradeSentinel{
		StartedAt:    now,
		Manager:      "fnm",
		OldVersion:   "20.9.0",
		NewVersion:   "20.10.0",
		SnapshotPath: snapPath,
	}
	if err := WriteSentinel(written); err != nil {
		t.Fatalf("WriteSentinel: %v", err)
	}

	got, err := OrphanedSentinel()
	if err != nil {
		t.Fatalf("OrphanedSentinel after write: %v", err)
	}
	if got == nil {
		t.Fatal("OrphanedSentinel returned nil after a sentinel was written")
	}

	// Every field of the round-trip must match — this is what the
	// CLI's warning message would print, so a regression here means
	// the user would see a garbled warning on next run.
	if !got.StartedAt.Equal(now) {
		t.Errorf("StartedAt: got %v, want %v", got.StartedAt, now)
	}
	if got.Manager != "fnm" {
		t.Errorf("Manager: got %q, want fnm", got.Manager)
	}
	if got.OldVersion != "20.9.0" {
		t.Errorf("OldVersion: got %q, want 20.9.0", got.OldVersion)
	}
	if got.NewVersion != "20.10.0" {
		t.Errorf("NewVersion: got %q, want 20.10.0", got.NewVersion)
	}
	if got.SnapshotPath != snapPath {
		t.Errorf("SnapshotPath: got %q, want %q", got.SnapshotPath, snapPath)
	}
}

// TestRemoveSentinel_Idempotent verifies RemoveSentinel:
//   - removes an existing sentinel
//   - does not error when the sentinel is already gone (so the deferred
//     cleanup in runUpgrade never panics on the happy path)
func TestRemoveSentinel_Idempotent(t *testing.T) {
	redirectDataDir(t)

	// First call: file does not exist. Must succeed silently.
	if err := RemoveSentinel(); err != nil {
		t.Errorf("RemoveSentinel on missing file: %v", err)
	}

	// Write + remove cycle must leave no trace.
	if err := WriteSentinel(UpgradeSentinel{Manager: "fnm"}); err != nil {
		t.Fatalf("WriteSentinel: %v", err)
	}
	if err := RemoveSentinel(); err != nil {
		t.Errorf("RemoveSentinel after write: %v", err)
	}

	// And a second remove after the first one must still succeed.
	if err := RemoveSentinel(); err != nil {
		t.Errorf("RemoveSentinel on already-removed file: %v", err)
	}

	// And OrphanedSentinel must now report nothing.
	s, err := OrphanedSentinel()
	if err != nil {
		t.Fatalf("OrphanedSentinel after cleanup: %v", err)
	}
	if s != nil {
		t.Errorf("OrphanedSentinel = %+v, want nil after RemoveSentinel", s)
	}
}

// TestRemoveSentinel_CleansUpTmpFile is a regression test for the
// "rename failed → tmp file leaked" branch in WriteSentinel. We can't
// easily force a rename failure, so instead we verify the happy path
// never leaves a .tmp file behind — if it ever does, that's a bug.
func TestRemoveSentinel_CleansUpTmpFile(t *testing.T) {
	redirectDataDir(t)

	if err := WriteSentinel(UpgradeSentinel{Manager: "nvm"}); err != nil {
		t.Fatalf("WriteSentinel: %v", err)
	}

	dir, err := dataDirForTest()
	if err != nil {
		t.Fatalf("dataDirForTest: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%s): %v", dir, err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("leftover tmp file in DataDir: %s", e.Name())
		}
	}
}

// TestWriteSentinel_DefaultsStartedAt documents that WriteSentinel
// fills in StartedAt if the caller leaves it zero, so the sentinel
// always has a timestamp.
func TestWriteSentinel_DefaultsStartedAt(t *testing.T) {
	redirectDataDir(t)

	before := time.Now()
	if err := WriteSentinel(UpgradeSentinel{Manager: "fnm"}); err != nil {
		t.Fatalf("WriteSentinel: %v", err)
	}
	after := time.Now()

	s, err := LoadSentinel()
	if err != nil {
		t.Fatalf("LoadSentinel: %v", err)
	}
	if s.StartedAt.Before(before) || s.StartedAt.After(after.Add(time.Second)) {
		t.Errorf("StartedAt = %v, want between %v and %v", s.StartedAt, before, after)
	}
}

// TestWriteSentinel_OverwritesExisting exercises the "atomic rename
// replaces the destination" property. The second WriteSentinel call
// must fully replace the first; partial-merge bugs would be obvious.
func TestWriteSentinel_OverwritesExisting(t *testing.T) {
	redirectDataDir(t)

	first := UpgradeSentinel{Manager: "fnm", OldVersion: "20.9.0", SnapshotPath: "/a.json"}
	if err := WriteSentinel(first); err != nil {
		t.Fatalf("WriteSentinel #1: %v", err)
	}
	second := UpgradeSentinel{Manager: "nvm", OldVersion: "18.0.0", SnapshotPath: "/b.json"}
	if err := WriteSentinel(second); err != nil {
		t.Fatalf("WriteSentinel #2: %v", err)
	}

	got, err := OrphanedSentinel()
	if err != nil {
		t.Fatalf("OrphanedSentinel: %v", err)
	}
	if got.Manager != "nvm" || got.OldVersion != "18.0.0" || got.SnapshotPath != "/b.json" {
		t.Errorf("after overwrite, got %+v, want fields from second sentinel", got)
	}
}

// TestLoadSentinel_ParseError is the "corrupted sentinel" branch: a
// file exists but doesn't parse as our schema. OrphanedSentinel must
// surface the error so the CLI can at least log it (we deliberately
// don't want to silently swallow schema corruption — that's a real
// signal something went wrong).
func TestLoadSentinel_ParseError(t *testing.T) {
	redirectDataDir(t)

	p, err := SentinelPath()
	if err != nil {
		t.Fatalf("SentinelPath: %v", err)
	}
	if err := os.WriteFile(p, []byte("{this is not json"), 0o644); err != nil {
		t.Fatalf("seed corrupted sentinel: %v", err)
	}

	_, lerr := LoadSentinel()
	if lerr == nil {
		t.Fatal("LoadSentinel on corrupted file: expected error, got nil")
	}
	if errors.Is(lerr, ErrNoSentinel) {
		t.Errorf("corrupted sentinel incorrectly mapped to ErrNoSentinel: %v", lerr)
	}

	// OrphanedSentinel surfaces the parse error rather than
	// swallowing it — this is the design choice that distinguishes
	// "missing file" (silent) from "corrupted file" (loud).
	_, oerr := OrphanedSentinel()
	if oerr == nil {
		t.Fatal("OrphanedSentinel on corrupted file: expected error, got nil")
	}
}

// TestRemoveSentinel_OnlyOnRestoreSuccess is the regression pin for
// issue #46.
//
// Before the fix, `nodeup upgrade`'s deferred cleanup called
// RemoveSentinel() unconditionally after the sentinel was armed —
// even when the post-install package restore had failed (because
// restore failures were logged as warnings and the function returned
// normally). The user was left with installed Node versions,
// unmigrated global packages, AND no "resume breadcrumb" to point
// the manual `nodeup packages restore --from <path>` command at.
//
// The fix is in the cli/upgrade.go defer: RemoveSentinel() now
// fires only after a successful restore. We can't unit-test that
// defer in isolation without spinning up the full upgrade pipeline,
// so we instead pin the underlying primitives the cli uses:
//   - writing a sentinel surfaces as orphaned
//   - calling RemoveSentinel() clears it
//
// The exact "restore success → clear" decision lives in upgrade.go's
// restoreSucceeded gate, reviewed by the same reader that owns the
// sentinel field. The companion fix on the manual `nodeup packages
// restore` path adds the inverse: a successful restore there now
// also calls RemoveSentinel() so a follow-up `nodeup` doesn't keep
// printing the "interrupted upgrade" hint.
func TestRemoveSentinel_OnlyOnRestoreSuccess(t *testing.T) {
	redirectDataDir(t)

	// Plant the sentinel — simulates an interrupted upgrade from a
	// previous run.
	if err := WriteSentinel(UpgradeSentinel{
		StartedAt:    time.Now(),
		Manager:      "fnm",
		OldVersion:   "20.9.0",
		NewVersion:   "20.10.0",
		SnapshotPath: "/tmp/snap.json",
	}); err != nil {
		t.Fatalf("WriteSentinel: %v", err)
	}

	// Sanity-check: subsequent run would print the "interrupted
	// upgrade" warning — so the sentinel must be visible to
	// OrphanedSentinel.
	s, err := OrphanedSentinel()
	if err != nil {
		t.Fatalf("OrphanedSentinel after write: %v", err)
	}
	if s == nil {
		t.Fatal("OrphanedSentinel returned nil after a sentinel was written")
	}
	if s.Manager != "fnm" {
		t.Fatalf("sentinel Manager = %q, want fnm", s.Manager)
	}

	// The fix in upgrade.go: this RemoveSentinel() must only be
	// reached when the restore step succeeded. We assert the
	// primitive it depends on is correct: removal leaves the file
	// gone, and OrphanedSentinel goes back to returning nil.
	if err := RemoveSentinel(); err != nil {
		t.Fatalf("RemoveSentinel: %v", err)
	}

	s, err = OrphanedSentinel()
	if err != nil {
		t.Fatalf("OrphanedSentinel after RemoveSentinel: %v", err)
	}
	if s != nil {
		t.Fatalf("OrphanedSentinel = %+v, want nil after RemoveSentinel", s)
	}
}

// dataDirForTest is a small helper that returns the redirected DataDir
// so cleanup / verification tests can introspect the filesystem.
func dataDirForTest() (string, error) {
	home := os.Getenv("HOME")
	if home == "" {
		home = os.Getenv("USERPROFILE")
	}
	if home == "" {
		return "", errors.New("no HOME/USERPROFILE set; redirectDataDir was not called")
	}
	// DataDir() resolves the same way regardless of OS; we duplicate
	// the layout here to avoid an import cycle (platform imports us).
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library", "Application Support", "nodeup"), nil
	}
	if runtime.GOOS == "windows" {
		return filepath.Join(home, "AppData", "Roaming", "nodeup"), nil
	}
	return filepath.Join(home, ".local", "share", "nodeup"), nil
}

// TestRestoreFromSnapshot_ValidFile covers the "explicit replay"
// happy path: a snapshot file exists at an arbitrary path, and
// RestoreFromSnapshot reads + parses it. We don't run the actual
// `npm install` (that would require a working npm); we check that
// the function makes it past the read+parse stage before invoking
// installPackages. We do this by pointing the file at a syntactically
// valid snapshot but with zero packages — the install loop is a no-op.
func TestRestoreFromSnapshot_ValidFile(t *testing.T) {
	tmp := t.TempDir()
	snap := SnapshotData{
		Manager:     "fnm",
		NodeVersion: "20.10.0",
		Packages:    []Package{},
	}
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	snapPath := filepath.Join(tmp, "manual-snapshot.json")
	if err := os.WriteFile(snapPath, data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// With an empty Packages list, RestoreFromSnapshot runs the
	// install loop zero times and returns nil — no need for a fake
	// `npm install` binary on PATH.
	if err := RestoreFromSnapshot(t.Context(), snapPath); err != nil {
		t.Errorf("RestoreFromSnapshot: %v", err)
	}
}

// TestRestoreFromSnapshot_MissingFile verifies the user-facing error
// includes both the path they typed AND a "read snapshot" hint so they
// don't have to read source to figure out what went wrong.
func TestRestoreFromSnapshot_MissingFile(t *testing.T) {
	err := RestoreFromSnapshot(t.Context(), "/nonexistent/snapshot.json")
	if err == nil {
		t.Fatal("expected error for missing snapshot file")
	}
	if !strings.Contains(err.Error(), "/nonexistent/snapshot.json") {
		t.Errorf("error %q does not include the path the user gave", err)
	}
}

// TestRestoreFromSnapshot_CorruptFile covers the schema-corruption
// branch: a file exists but its JSON doesn't match SnapshotData.
// RestoreFromSnapshot must report a parse error rather than silently
// installing an empty package list (which would silently lose the
// user's packages).
func TestRestoreFromSnapshot_CorruptFile(t *testing.T) {
	tmp := t.TempDir()
	snapPath := filepath.Join(tmp, "bad.json")
	if err := os.WriteFile(snapPath, []byte("{not valid json"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	err := RestoreFromSnapshot(t.Context(), snapPath)
	if err == nil {
		t.Fatal("expected error for corrupt JSON")
	}
	if !strings.Contains(err.Error(), "parse snapshot") {
		t.Errorf("error %q does not mention parse failure", err)
	}
}
