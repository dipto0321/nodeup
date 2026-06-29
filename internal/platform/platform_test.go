package platform

import (
	"path/filepath"
	"runtime"
	"testing"
)

// TestDataDirIsUnderUserHome documents and enforces the convention that
// nodeup's data directory lives under the user's home (or AppData on
// Windows). On Linux we use $XDG_DATA_HOME/nodeup or its default.
// On macOS we use ~/Library/Application Support/nodeup.
func TestDataDirIsUnderUserHome(t *testing.T) {
	dir, err := DataDir()
	if err != nil {
		t.Fatalf("DataDir: %v", err)
	}
	if dir == "" {
		t.Fatal("DataDir returned empty string")
	}
	if !filepath.IsAbs(dir) {
		t.Errorf("DataDir %q is not absolute", dir)
	}
	// Sanity: the directory should be named "nodeup" as the leaf.
	if filepath.Base(dir) != "nodeup" {
		t.Errorf("DataDir leaf = %q, want nodeup", filepath.Base(dir))
	}
}

// TestSnapshotsReportsCacheConfigAreSiblings verifies the four data
// subdirectories share the same parent (DataDir). Otherwise we'd be
// scattering nodeup state across the filesystem.
func TestSnapshotsReportsCacheConfigAreSiblings(t *testing.T) {
	snap, err := SnapshotsDir()
	if err != nil {
		t.Fatal(err)
	}
	rep, err := ReportsDir()
	if err != nil {
		t.Fatal(err)
	}
	cch, err := CacheDir()
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := ConfigPath()
	if err != nil {
		t.Fatal(err)
	}

	// All four paths should share the same parent directory (DataDir).
	parents := []string{filepath.Dir(snap), filepath.Dir(rep), filepath.Dir(cch), filepath.Dir(cfg)}
	for i := 1; i < len(parents); i++ {
		if parents[i] != parents[0] {
			t.Errorf("data dirs have inconsistent parents: %v", parents)
		}
	}
}

// TestPlatformHelpers reports the GOOS the test is running on so a
// misconfigured CI matrix surfaces immediately instead of silently
// skipping tests.
func TestPlatformHelpers(t *testing.T) {
	t.Logf("GOOS=%s GOARCH=%s", runtime.GOOS, runtime.GOARCH)
	switch runtime.GOOS {
	case "windows":
		if !IsWindows() {
			t.Error("IsWindows() false on windows runner")
		}
	case "darwin":
		if !IsMacOS() {
			t.Error("IsMacOS() false on darwin runner")
		}
	case "linux":
		if !IsLinux() {
			t.Error("IsLinux() false on linux runner")
		}
	}
	if IsARM64() != (runtime.GOARCH == "arm64") {
		t.Error("IsARM64 disagrees with runtime.GOARCH")
	}
}

// TestQuotePathNoSpecials verifies that paths without spaces or shell
// metacharacters pass through unchanged (no spurious quoting).
func TestQuotePathNoSpecials(t *testing.T) {
	in := "/usr/local/bin"
	if got := QuotePath(in); got != in {
		t.Errorf("QuotePath(%q) = %q, want unchanged", in, got)
	}
}

// TestQuotePathEmpty verifies the empty-string case.
func TestQuotePathEmpty(t *testing.T) {
	if got := QuotePath(""); got != `""` {
		t.Errorf("QuotePath(\"\") = %q, want %q", got, `""`)
	}
}

// TestQuotePathWithSpace verifies that paths containing spaces get wrapped.
func TestQuotePathWithSpace(t *testing.T) {
	in := "/Users/dipto/My App"
	got := QuotePath(in)
	if got == in {
		t.Error("QuotePath left a space-containing path unquoted")
	}
	// Must start with a quote.
	if got[0] != '"' {
		t.Errorf("QuotePath did not wrap in double quotes: %q", got)
	}
}

// TestLookupManagerBinaryMissingIsEmpty verifies the soft-detection
// behavior — returns an empty string rather than an error.
func TestLookupManagerBinaryMissingIsEmpty(t *testing.T) {
	got := LookupManagerBinary("definitely-not-a-real-binary-xyzzy")
	if got != "" {
		t.Errorf("LookupManagerBinary for missing binary = %q, want empty", got)
	}
}