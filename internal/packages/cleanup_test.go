package packages

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dipto0321/nodeup/internal/platform"
)

// writeSnapshotFile drops a valid snapshot file into the snapshots
// directory under the given name and returns its full path.
func writeSnapshotFile(t *testing.T, name string, body any) string {
	t.Helper()
	dir, err := platform.SnapshotsDir()
	if err != nil {
		t.Fatalf("SnapshotsDir: %v", err)
	}
	path := filepath.Join(dir, name)
	var data []byte
	switch v := body.(type) {
	case string:
		data = []byte(v)
	default:
		var err error
		data, err = json.MarshalIndent(v, "", "  ")
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// TestListSnapshotFiles_ListsCorruptFiles verifies the cleanup
// listing surfaces snapshots whose JSON body is unparseable.
//
// This is the whole reason ListSnapshotFiles exists as a separate API
// from ListSnapshots: a snapshot truncated by a crash mid-write is
// skipped by ListSnapshots (it doesn't parse) and therefore invisible
// to every code path — yet it still occupies disk. A cleanup command
// that can't see the file can't delete it. See #128.
func TestListSnapshotFiles_ListsCorruptFiles(t *testing.T) {
	redirectDataDir(t)

	writeSnapshotFile(t, "fnm-20.10.0.json", SnapshotData{
		Manager:     "fnm",
		NodeVersion: "20.10.0",
		Packages:    []Package{{Name: "typescript", Version: "5.4.0"}},
	})
	writeSnapshotFile(t, "fnm-18.0.0.json", "{ this is not json")

	files, err := ListSnapshotFiles()
	if err != nil {
		t.Fatalf("ListSnapshotFiles: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("ListSnapshotFiles returned %d files, want 2 (the corrupt one must still be listed)", len(files))
	}

	byName := map[string]SnapshotFile{}
	for _, f := range files {
		byName[f.Name] = f
	}

	good, ok := byName["fnm-20.10.0.json"]
	if !ok {
		t.Fatal("valid snapshot missing from listing")
	}
	if good.ParseError != "" {
		t.Errorf("valid snapshot has ParseError %q, want empty", good.ParseError)
	}
	if good.Manager != "fnm" || good.Version != "20.10.0" {
		t.Errorf("valid snapshot manager/version = %q/%q, want fnm/20.10.0", good.Manager, good.Version)
	}
	if good.Size == 0 {
		t.Error("valid snapshot Size = 0, want the actual byte count")
	}

	bad, ok := byName["fnm-18.0.0.json"]
	if !ok {
		t.Fatal("corrupt snapshot missing from listing")
	}
	if bad.ParseError == "" {
		t.Error("corrupt snapshot has empty ParseError, want a description")
	}
}

// TestListSnapshotFiles_SkipsNonJSON verifies that files which are not
// snapshots at all (or directories) are ignored.
func TestListSnapshotFiles_SkipsNonJSON(t *testing.T) {
	redirectDataDir(t)

	dir, err := platform.SnapshotsDir()
	if err != nil {
		t.Fatalf("SnapshotsDir: %v", err)
	}
	writeSnapshotFile(t, "fnm-20.10.0.json", SnapshotData{Manager: "fnm", NodeVersion: "20.10.0"})
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write notes.txt: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "subdir.json"), 0o755); err != nil {
		t.Fatalf("mkdir subdir.json: %v", err)
	}

	files, err := ListSnapshotFiles()
	if err != nil {
		t.Fatalf("ListSnapshotFiles: %v", err)
	}
	if len(files) != 1 {
		names := make([]string, len(files))
		for i, f := range files {
			names[i] = f.Name
		}
		t.Fatalf("ListSnapshotFiles returned %v, want exactly [fnm-20.10.0.json]", names)
	}
}

// TestListSnapshotFiles_ListsLeftoverTmpFiles verifies the half-written
// ".json.tmp" siblings an interrupted atomic write leaves behind are
// listed (and therefore deletable). Nothing else in nodeup looks at
// them, so without this they'd accumulate unnoticed.
func TestListSnapshotFiles_ListsLeftoverTmpFiles(t *testing.T) {
	redirectDataDir(t)

	writeSnapshotFile(t, "fnm-20.10.0.json.tmp", `{"manager":"fnm"`)

	files, err := ListSnapshotFiles()
	if err != nil {
		t.Fatalf("ListSnapshotFiles: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("ListSnapshotFiles returned %d files, want 1", len(files))
	}
	if files[0].Name != "fnm-20.10.0.json.tmp" {
		t.Errorf("listed %q, want fnm-20.10.0.json.tmp", files[0].Name)
	}
	// The manager/version are still recoverable from the filename even
	// though the body is truncated.
	if files[0].Manager != "fnm" || files[0].Version != "20.10.0" {
		t.Errorf("manager/version = %q/%q, want fnm/20.10.0", files[0].Manager, files[0].Version)
	}
}

// TestDeleteSnapshotFile_RemovesFile verifies the happy path.
func TestDeleteSnapshotFile_RemovesFile(t *testing.T) {
	redirectDataDir(t)

	path := writeSnapshotFile(t, "fnm-20.10.0.json", SnapshotData{Manager: "fnm", NodeVersion: "20.10.0"})

	if err := DeleteSnapshotFile(path); err != nil {
		t.Fatalf("DeleteSnapshotFile: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("snapshot still exists after delete (stat err = %v)", err)
	}
}

// TestDeleteSnapshotFile_MissingIsSuccess verifies that deleting a file
// that is already gone is not an error — the desired end state is
// already true.
func TestDeleteSnapshotFile_MissingIsSuccess(t *testing.T) {
	redirectDataDir(t)

	dir, err := platform.SnapshotsDir()
	if err != nil {
		t.Fatalf("SnapshotsDir: %v", err)
	}
	missing := filepath.Join(dir, "fnm-99.99.99.json")

	if err := DeleteSnapshotFile(missing); err != nil {
		t.Errorf("DeleteSnapshotFile(missing) = %v, want nil (already gone is success)", err)
	}
}

// TestDeleteSnapshotFile_RefusesOutsideDir is the security guard: a
// path outside the snapshots directory must be rejected, not deleted.
//
// The path can come from user input (the cleanup CLI feeds it back
// from a listing, but the API is public), so we refuse anything that
// doesn't resolve inside the snapshots directory. See #51.
func TestDeleteSnapshotFile_RefusesOutsideDir(t *testing.T) {
	redirectDataDir(t)

	outside := filepath.Join(t.TempDir(), "important.json")
	if err := os.WriteFile(outside, []byte(`{"keep":"me"}`), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	err := DeleteSnapshotFile(outside)
	if err == nil {
		t.Fatal("DeleteSnapshotFile(outside) = nil, want a refusal")
	}
	if !strings.Contains(err.Error(), "not inside the snapshots directory") {
		t.Errorf("err = %v, want a 'not inside the snapshots directory' refusal", err)
	}
	if _, statErr := os.Stat(outside); statErr != nil {
		t.Errorf("the outside file was deleted anyway: %v", statErr)
	}
}

// TestDeleteSnapshotFile_RefusesTraversal is the same guard expressed
// the way a user would actually hit it: a ".." path that escapes the
// snapshots directory.
func TestDeleteSnapshotFile_RefusesTraversal(t *testing.T) {
	redirectDataDir(t)

	dir, err := platform.SnapshotsDir()
	if err != nil {
		t.Fatalf("SnapshotsDir: %v", err)
	}
	// <tmp>/evil.json sits next to <tmp>/snapshots — a traversal from
	// the snapshots dir reaches it.
	evil := filepath.Join(filepath.Dir(dir), "evil.json")
	if err := os.WriteFile(evil, []byte(`{}`), 0o644); err != nil {
		t.Fatalf("write evil.json: %v", err)
	}

	traversal := filepath.Join(dir, "..", "evil.json")
	if err := DeleteSnapshotFile(traversal); err == nil {
		t.Fatal("DeleteSnapshotFile(traversal) = nil, want a refusal")
	}
	if _, statErr := os.Stat(evil); statErr != nil {
		t.Errorf("the traversal target was deleted anyway: %v", statErr)
	}
}

// TestDeleteSnapshotFile_RefusesSymlink verifies that a symlink
// pointing outside the snapshots directory is rejected rather than
// followed. The "inside the dir" check runs on the *resolved* path, so
// the symlink's target is what gets checked.
func TestDeleteSnapshotFile_RefusesSymlink(t *testing.T) {
	redirectDataDir(t)

	outside := filepath.Join(t.TempDir(), "target.json")
	if err := os.WriteFile(outside, []byte(`{}`), 0o644); err != nil {
		t.Fatalf("write target: %v", err)
	}

	dir, err := platform.SnapshotsDir()
	if err != nil {
		t.Fatalf("SnapshotsDir: %v", err)
	}
	link := filepath.Join(dir, "fnm-20.10.0.json")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable on this platform: %v", err)
	}

	if err := DeleteSnapshotFile(link); err == nil {
		t.Fatal("DeleteSnapshotFile(symlink-to-outside) = nil, want a refusal")
	}
	if _, statErr := os.Stat(outside); statErr != nil {
		t.Errorf("the symlink target was deleted anyway: %v", statErr)
	}
}

// TestIsInsideDir covers the boundary cases of the directory guard.
//
// The naive strings.HasPrefix check would consider "/tmp/ab" to be
// inside "/tmp/a" — filepath.Rel handles that correctly. Same guard the
// tar-extraction path uses (see #65).
func TestIsInsideDir(t *testing.T) {
	tests := []struct {
		dir, path string
		want      bool
	}{
		{"/tmp/a", "/tmp/a/b.json", true},
		{"/tmp/a", "/tmp/a", true},
		{"/tmp/a", "/tmp/a/b/c.json", true},
		{"/tmp/a", "/tmp/ab.json", false},
		{"/tmp/a", "/tmp/b.json", false},
		{"/tmp/a", "/tmp/a/../b.json", false},
		{"/tmp/a", "/etc/passwd", false},
	}
	for _, tc := range tests {
		got := isInsideDir(tc.dir, tc.path)
		if got != tc.want {
			t.Errorf("isInsideDir(%q, %q) = %v, want %v", tc.dir, tc.path, got, tc.want)
		}
	}
}

// TestParseSnapshotFilename covers the naming convention, including
// manager names that themselves contain a hyphen.
func TestParseSnapshotFilename(t *testing.T) {
	tests := []struct {
		name        string
		wantMgr     string
		wantVersion string
	}{
		{"fnm-20.10.0.json", "fnm", "20.10.0"},
		{"vite-plus-20.10.0.json", "vite-plus", "20.10.0"},
		{"nvm-windows-20.10.0.json", "nvm-windows", "20.10.0"},
		{"fnm-20.10.0.json.tmp", "fnm", "20.10.0"},
		{"noverhyphen.json", "", ""},
		{"-20.10.0.json", "", ""},
		{"fnm-.json", "", ""},
	}
	for _, tc := range tests {
		mgr, ver := parseSnapshotFilename(tc.name)
		if mgr != tc.wantMgr || ver != tc.wantVersion {
			t.Errorf("parseSnapshotFilename(%q) = %q/%q, want %q/%q", tc.name, mgr, ver, tc.wantMgr, tc.wantVersion)
		}
	}
}

// TestListReportFiles verifies the reports listing enumerates the
// migration-report files (which leak in the same way the snapshots do).
func TestListReportFiles(t *testing.T) {
	redirectDataDir(t)

	dir, err := platform.ReportsDir()
	if err != nil {
		t.Fatalf("ReportsDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "migration-20260101-abcdef.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatalf("write report: %v", err)
	}

	files, err := ListReportFiles()
	if err != nil {
		t.Fatalf("ListReportFiles: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("ListReportFiles returned %d files, want 1", len(files))
	}
	if files[0].Name != "migration-20260101-abcdef.json" {
		t.Errorf("listed %q, want migration-20260101-abcdef.json", files[0].Name)
	}
}

// TestDeleteReportFile verifies the report deletion happy path and
// the already-gone case.
func TestDeleteReportFile(t *testing.T) {
	redirectDataDir(t)

	dir, err := platform.ReportsDir()
	if err != nil {
		t.Fatalf("ReportsDir: %v", err)
	}
	path := filepath.Join(dir, "migration-20260101-abcdef.json")
	if err := os.WriteFile(path, []byte(`{}`), 0o644); err != nil {
		t.Fatalf("write report: %v", err)
	}

	if err := DeleteReportFile(path); err != nil {
		t.Fatalf("DeleteReportFile: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("report still exists after delete (stat err = %v)", err)
	}
	if err := DeleteReportFile(path); err != nil {
		t.Errorf("DeleteReportFile(missing) = %v, want nil", err)
	}
}

// TestDeleteReportFile_RefusesOutsideDir is the same guard as for
// snapshots, applied to the reports directory.
func TestDeleteReportFile_RefusesOutsideDir(t *testing.T) {
	redirectDataDir(t)

	outside := filepath.Join(t.TempDir(), "important.json")
	if err := os.WriteFile(outside, []byte(`{}`), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	if err := DeleteReportFile(outside); err == nil {
		t.Fatal("DeleteReportFile(outside) = nil, want a refusal")
	}
	if _, statErr := os.Stat(outside); statErr != nil {
		t.Errorf("the outside file was deleted anyway: %v", statErr)
	}
}

// TestSaveSnapshot_IsAtomic verifies the snapshot write leaves no
// temporary sibling behind and produces a complete, parseable file.
//
// The atomicity itself (tmp + rename) is what guarantees a reader
// never sees a truncated body — see the saveSnapshot doc comment.
func TestSaveSnapshot_IsAtomic(t *testing.T) {
	redirectDataDir(t)

	snap := SnapshotData{
		Manager:     "fnm",
		NodeVersion: "20.10.0",
		Packages:    []Package{{Name: "typescript", Version: "5.4.0"}},
	}
	if err := saveSnapshot(snap); err != nil {
		t.Fatalf("saveSnapshot: %v", err)
	}

	dir, err := platform.SnapshotsDir()
	if err != nil {
		t.Fatalf("SnapshotsDir: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Fatalf("snapshots dir contains %v, want exactly the one snapshot", names)
	}
	if entries[0].Name() != "fnm-20.10.0.json" {
		t.Errorf("snapshot file = %q, want fnm-20.10.0.json", entries[0].Name())
	}

	got, err := LoadSnapshot("fnm", "20.10.0")
	if err != nil {
		t.Fatalf("LoadSnapshot: %v", err)
	}
	if len(got.Packages) != 1 || got.Packages[0].Name != "typescript" {
		t.Errorf("round-tripped packages = %+v, want one typescript entry", got.Packages)
	}
}
