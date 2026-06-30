package platform

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"
	"time"
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

// TestQuotePathTable exercises QuotePath across the cases that matter for
// cross-platform support: spaces, embedded quotes, backslashes (Windows
// path separators), shell metacharacters on unix ($, `, |), and edges
// (empty, single space, trailing slash).
//
// Expected output is computed by branching on runtime.GOOS because
// QuotePath uses a different unsafe-char set on Windows vs unix: on
// Windows only `"` is unsafe; on unix ` "$`<>|'` are all unsafe.
//
// We run the same table on every OS so CI catches regressions on all
// three platforms (ubuntu, macos, windows in our matrix).
func TestQuotePathTable(t *testing.T) {
	type tc struct {
		name string
		in   string
		want string
	}

	// Unix-style cases (forward slashes, no backslashes). On Windows the
	// unsafe-char set is narrower — backslash is treated as a regular
	// char and not escaped — so paths containing backslashes are only
	// valid inputs on Windows. We split the table by OS to keep the
	// expectations unambiguous.
	unixCases := []tc{
		{"empty", "", `""`},
		{"plain_unix_path", "/usr/local/bin", "/usr/local/bin"},
		{"single_space", " ", `" "`},
		{"trailing_space", "/Users/dipto/My App ", `"/Users/dipto/My App "`},
		{"embedded_space", "/Users/dipto/My App/bin", `"/Users/dipto/My App/bin"`},
		{"multiple_spaces", "/Users/My  App/bin", `"/Users/My  App/bin"`},
		{"dollar_sign", "/home/$USER/foo", `"/home/$USER/foo"`},
		{"backtick", "/tmp/`whoami`/x", "\"/tmp/`whoami`/x\""},
		{"pipe", "/tmp/a|b/c", `"/tmp/a|b/c"`},
		{"redirect", "/tmp/a>b/c", `"/tmp/a>b/c"`},
		{"single_quote", "/tmp/it's/x", `"/tmp/it's/x"`},
		{"embedded_dquote", `/tmp/oops"quote/x`, `"/tmp/oops\"quote/x"`},
		{"trailing_slash", "/var/log/", "/var/log/"},
	}
	windowsCases := []tc{
		{"empty", "", `""`},
		{"plain_windows_path", `C:\node`, `C:\node`},
		{"single_space", " ", `" "`},
		{"program_files", `C:\Program Files\nodejs`, `"C:\\Program Files\\nodejs"`},
		{"trailing_backslash", `C:\Users\Name\App Data\`, `"C:\\Users\\Name\\App Data\\"`},
		{"embedded_dquote", `C:\tmp\oops"quote`, `"C:\\tmp\\oops\"quote"`},
		{"plain_forward_slash", "/usr/local/bin", "/usr/local/bin"},
	}

	cases := unixCases
	if runtime.GOOS == "windows" {
		cases = windowsCases
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			got := QuotePath(c.in)
			if got != c.want {
				t.Errorf("QuotePath(%q) = %q, want %q (GOOS=%s)",
					c.in, got, c.want, runtime.GOOS)
			}
		})
	}
}

// TestQuotePathRoundTrip confirms that the output of QuotePath, when
// embedded in a shell script, re-evaluates to the original path. We
// shell out via RunShellScript and `printf '%s'` the quoted form, then
// compare to the original input.
//
// We only test cases that QuotePath is *designed* to protect: paths
// with spaces, embedded double-quotes, and shell metacharacters that
// are not subject to variable expansion (|, <, >). Paths containing
// literal `$` or backticks are intentionally NOT tested here because
// QuotePath uses double-quotes (preserving shell variable expansion),
// so a `$` in the path will still be expanded by the shell. See the
// comment on QuotePath itself for this trade-off.
func TestQuotePathRoundTrip(t *testing.T) {
	if runtime.GOOS == "plan9" {
		t.Skip("plan9 shell semantics differ; skip")
	}
	// The round-trip uses RunShellScript, which on Windows invokes
	// cmd.exe rather than bash. cmd.exe's unquote rules differ from
	// bash (notably, cmd.exe drops the leading backslash from a
	// double-quoted path and treats `\"` as a literal `"` followed by
	// the next char). The test's purpose is to verify QuotePath's
	// bash-mode quoting, so it is not meaningful on Windows. The
	// table-driven TestQuotePath already covers the per-OS quoting
	// logic for both unix and Windows.
	if runtime.GOOS == "windows" {
		t.Skip("cmd.exe unquote rules differ from bash; TestQuotePath covers per-OS expected output")
	}
	paths := []string{
		"/Users/dipto/My App",
		"/Users/Double  Space/bin",
		"/tmp/oops\"quote/x",
	}
	for _, p := range paths {
		p := p
		t.Run(p, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			// `printf '%s'` echoes its second argument verbatim.
			script := "printf '%s' " + QuotePath(p)
			res, err := RunShellScript(ctx, script)
			if err != nil {
				t.Fatalf("RunShellScript(%q): %v", script, err)
			}
			if res.Stdout != p {
				t.Errorf("round-trip mismatch: in=%q quoted=%q got=%q",
					p, QuotePath(p), res.Stdout)
			}
		})
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
