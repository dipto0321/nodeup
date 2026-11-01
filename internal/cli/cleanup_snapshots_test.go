package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dipto0321/nodeup/internal/packages"
	"github.com/dipto0321/nodeup/internal/platform"
)

// redirectCleanupDataDir points nodeup's data directory at a temp dir
// so the cleanup command's file operations never touch the developer's
// real snapshots. Mirrors the helper in internal/packages — the CLI
// package has its own because the packages helper isn't exported.
func redirectCleanupDataDir(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()

	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("APPDATA", "")
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", tmp)
		t.Setenv("HOME", tmp)
	} else {
		t.Setenv("HOME", tmp)
	}
	return tmp
}

// writeCleanupSnapshot drops a snapshot file into the snapshots dir.
func writeCleanupSnapshot(t *testing.T, name, body string) string {
	t.Helper()
	dir, err := platform.SnapshotsDir()
	if err != nil {
		t.Fatalf("SnapshotsDir: %v", err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// TestCleanupCmd_ListOnlyDeletesNothing verifies --list shows what
// would be deleted and leaves everything on disk.
func TestCleanupCmd_ListOnlyDeletesNothing(t *testing.T) {
	redirectCleanupDataDir(t)
	writeCleanupSnapshot(t, "fnm-20.10.0.json", `{"manager":"fnm","node_version":"20.10.0","packages":[]}`)

	cmd := newCleanupCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("cleanup --list: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "fnm-20.10.0.json") {
		t.Errorf("output %q missing the snapshot name", out)
	}
	if !strings.Contains(out, "Nothing deleted") {
		t.Errorf("output %q missing the 'Nothing deleted' notice", out)
	}

	files, err := packages.ListSnapshotFiles()
	if err != nil {
		t.Fatalf("ListSnapshotFiles: %v", err)
	}
	if len(files) != 1 {
		t.Errorf("snapshot count after --list = %d, want 1 (--list must not delete)", len(files))
	}
}

// TestCleanupCmd_AllYesDeletesEverything verifies the --all --yes
// path deletes every snapshot without prompting.
func TestCleanupCmd_AllYesDeletesEverything(t *testing.T) {
	redirectCleanupDataDir(t)
	writeCleanupSnapshot(t, "fnm-20.10.0.json", `{"manager":"fnm","node_version":"20.10.0","packages":[]}`)
	writeCleanupSnapshot(t, "fnm-22.0.0.json", `{"manager":"fnm","node_version":"22.0.0","packages":[]}`)

	cmd := newCleanupCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--all", "--yes"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("cleanup --all --yes: %v", err)
	}

	files, err := packages.ListSnapshotFiles()
	if err != nil {
		t.Fatalf("ListSnapshotFiles: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("snapshot count after --all --yes = %d, want 0", len(files))
	}
}

// TestCleanupCmd_InteractiveSelectionDeletesOnlyPicked verifies the
// multi-select path: the user picks specific files and only those
// are removed.
//
// The prompt is PlainMode here (the leaf command built in isolation
// has no writer in its context, so writerFromCmd falls back to a
// PlainMode writer) and reads a comma-separated list of numbers.
func TestCleanupCmd_InteractiveSelectionDeletesOnlyPicked(t *testing.T) {
	redirectCleanupDataDir(t)
	writeCleanupSnapshot(t, "fnm-20.10.0.json", `{"manager":"fnm","node_version":"20.10.0","packages":[]}`)
	writeCleanupSnapshot(t, "fnm-22.0.0.json", `{"manager":"fnm","node_version":"22.0.0","packages":[]}`)
	writeCleanupSnapshot(t, "fnm-24.0.0.json", `{"manager":"fnm","node_version":"24.0.0","packages":[]}`)

	cmd := newCleanupCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	// "1" picks the first listed file; then "y" confirms.
	cmd.SetIn(strings.NewReader("1\ny\n"))

	if err := cmd.Execute(); err != nil {
		t.Fatalf("cleanup (interactive): %v", err)
	}

	files, err := packages.ListSnapshotFiles()
	if err != nil {
		t.Fatalf("ListSnapshotFiles: %v", err)
	}
	if len(files) != 2 {
		names := make([]string, len(files))
		for i, f := range files {
			names[i] = f.Name
		}
		t.Errorf("after picking 1 file: %d remain (%v), want 2", len(files), names)
	}
}

// TestCleanupCmd_InteractiveBlankSelectsNothing verifies that answering
// the multi-select with a blank line deletes NOTHING — the safe
// direction for a destructive prompt.
func TestCleanupCmd_InteractiveBlankSelectsNothing(t *testing.T) {
	redirectCleanupDataDir(t)
	writeCleanupSnapshot(t, "fnm-20.10.0.json", `{"manager":"fnm","node_version":"20.10.0","packages":[]}`)

	cmd := newCleanupCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetIn(strings.NewReader("\n"))

	if err := cmd.Execute(); err != nil {
		t.Fatalf("cleanup (blank selection): %v", err)
	}

	files, err := packages.ListSnapshotFiles()
	if err != nil {
		t.Fatalf("ListSnapshotFiles: %v", err)
	}
	if len(files) != 1 {
		t.Errorf("snapshot count after blank selection = %d, want 1 (nothing selected = nothing deleted)", len(files))
	}
}

// TestCleanupCmd_InteractiveConfirmationDeclined verifies that
// answering "n" to the confirmation keeps the files.
func TestCleanupCmd_InteractiveConfirmationDeclined(t *testing.T) {
	redirectCleanupDataDir(t)
	writeCleanupSnapshot(t, "fnm-20.10.0.json", `{"manager":"fnm","node_version":"20.10.0","packages":[]}`)

	cmd := newCleanupCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	// Pick the file, then decline the confirmation.
	cmd.SetIn(strings.NewReader("1\nn\n"))

	if err := cmd.Execute(); err != nil {
		t.Fatalf("cleanup (declined): %v", err)
	}

	files, err := packages.ListSnapshotFiles()
	if err != nil {
		t.Fatalf("ListSnapshotFiles: %v", err)
	}
	if len(files) != 1 {
		t.Errorf("snapshot count after declined confirmation = %d, want 1", len(files))
	}
}

// TestCleanupCmd_ReportsFlag verifies --reports targets the migration
// reports directory instead of the snapshots directory.
func TestCleanupCmd_ReportsFlag(t *testing.T) {
	redirectCleanupDataDir(t)

	// A snapshot AND a report — only the report should be removed.
	writeCleanupSnapshot(t, "fnm-20.10.0.json", `{"manager":"fnm","node_version":"20.10.0","packages":[]}`)
	reportDir, err := platform.ReportsDir()
	if err != nil {
		t.Fatalf("ReportsDir: %v", err)
	}
	reportPath := filepath.Join(reportDir, "migration-20260101-abcdef.json")
	if err := os.WriteFile(reportPath, []byte(`{}`), 0o644); err != nil {
		t.Fatalf("write report: %v", err)
	}

	cmd := newCleanupCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--reports", "--all", "--yes"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("cleanup --reports: %v", err)
	}

	if _, statErr := os.Stat(reportPath); !os.IsNotExist(statErr) {
		t.Errorf("report still exists after --reports --all --yes (stat err = %v)", statErr)
	}
	snaps, err := packages.ListSnapshotFiles()
	if err != nil {
		t.Fatalf("ListSnapshotFiles: %v", err)
	}
	if len(snaps) != 1 {
		t.Errorf("snapshot count = %d, want 1 (--reports must not touch snapshots)", len(snaps))
	}
}

// TestCleanupCmd_NoFiles verifies the empty case is a friendly
// no-op rather than an error.
func TestCleanupCmd_NoFiles(t *testing.T) {
	redirectCleanupDataDir(t)

	cmd := newCleanupCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--all", "--yes"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("cleanup with no files: %v", err)
	}
	if !strings.Contains(buf.String(), "No snapshot files found") {
		t.Errorf("output %q missing the 'No snapshot files found' notice", buf.String())
	}
}

// TestFormatBytes covers the human-readable size rendering.
func TestFormatBytes(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1024 * 1024, "1.0 MB"},
		{1024 * 1024 * 1024, "1.0 GB"},
	}
	for _, tc := range tests {
		if got := formatBytes(tc.in); got != tc.want {
			t.Errorf("formatBytes(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
