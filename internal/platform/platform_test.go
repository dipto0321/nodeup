package platform

import (
	"context"
	"os"
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

// TestQuotePathNoSpecials documents that all paths now go through
// single-quote wrapping (POSIX) or selective double-quote wrapping
// (Windows). The output is always a shell-safe literal of the input.
func TestQuotePathNoSpecials(t *testing.T) {
	in := "/usr/local/bin"
	got := QuotePath(in)
	if runtime.GOOS == "windows" {
		if got != in {
			t.Errorf("QuotePath(%q) = %q, want unchanged on Windows", in, got)
		}
		return
	}
	want := "'/usr/local/bin'"
	if got != want {
		t.Errorf("QuotePath(%q) = %q, want %q", in, got, want)
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
	// POSIX: must start with a single quote (and end with one).
	// Windows: must start with a double quote.
	if runtime.GOOS == "windows" {
		if got[0] != '"' {
			t.Errorf("QuotePath did not wrap in double quotes (Windows): %q", got)
		}
	} else {
		if got[0] != '\'' {
			t.Errorf("QuotePath did not wrap in single quotes (POSIX): %q", got)
		}
	}
}

// TestQuotePathTable exercises QuotePath across the cases that matter for
// cross-platform support: spaces, embedded quotes, backslashes (Windows
// path separators), shell metacharacters on unix ($, `, |, '), and edges
// (empty, single space, trailing slash).
//
// Expected output is computed by branching on runtime.GOOS because
// QuotePath uses a different quoting strategy on Windows vs unix: on
// Windows we wrap paths-with-spaces in double quotes (the cmd.exe
// unquote rules); on unix we ALWAYS wrap in single quotes so that `$`,
// backticks, and `\\` are not interpreted by bash. See #43.
//
// We run the same table on every OS so CI catches regressions on all
// three platforms (ubuntu, macos, windows in our matrix).
func TestQuotePathTable(t *testing.T) {
	type tc struct {
		name string
		in   string
		want string
	}

	// Unix-style cases. On POSIX we always single-quote; embedded `'`
	// becomes `'\''`. We deliberately do NOT preserve `$USER` or
	// backticks verbatim — that was the command-injection bug.
	unixCases := []tc{
		{"empty", "", `""`},
		{"plain_unix_path", "/usr/local/bin", "'/usr/local/bin'"},
		{"single_space", " ", `' '`},
		{"trailing_space", "/Users/dipto/My App ", "'/Users/dipto/My App '"},
		{"embedded_space", "/Users/dipto/My App/bin", "'/Users/dipto/My App/bin'"},
		{"multiple_spaces", "/Users/My  App/bin", "'/Users/My  App/bin'"},
		// Regression cases for #43 — these previously passed through
		// unchanged (or with bare double quotes that bash would expand).
		{"dollar_sign", "/home/$USER/foo", "'/home/$USER/foo'"},
		{"backtick", "/tmp/`whoami`/x", "'/tmp/`whoami`/x'"},
		{"command_subst", "/tmp/$(id)/x", "'/tmp/$(id)/x'"},
		{"pipe", "/tmp/a|b/c", "'/tmp/a|b/c'"},
		{"redirect", "/tmp/a>b/c", "'/tmp/a>b/c'"},
		// Single quote inside the path becomes the canonical
		// close/reopen/escape dance.
		{"single_quote", "/tmp/it's/x", `'/tmp/it'\''s/x'`},
		{"embedded_dquote", `/tmp/oops"quote/x`, `'/tmp/oops"quote/x'`},
		{"trailing_slash", "/var/log/", "'/var/log/'"},
	}
	// Windows: same logic as before — only `"` (and spaces) trigger
	// double-quote wrapping; `\\` inside the path gets escaped for
	// cmd.exe double-quote semantics.
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

// TestQuotePathNeutralizesShellInjection is the regression test for #43:
// a path containing `$(touch /tmp/PWNED)` or `\`whoami\“ must NOT be
// expanded when fed back through bash. We embed the quoted form in a
// shell script that touches a side-channel file if (and only if)
// expansion happens.
func TestQuotePathNeutralizesShellInjection(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("cmd.exe unquote rules differ; Windows path is covered by TestQuotePathTable")
	}
	// A path that, if expanded, would `touch` a sentinel file. After
	// running the test, the sentinel MUST NOT exist.
	malicious := "/tmp/nodeup-quote-path-test-$$(touch /tmp/nodeup-pwned-$$)"
	sentinel := "/tmp/nodeup-pwned-$$"
	// Clean up any leftover from a previous failed run.
	_ = os.Remove(sentinel)
	defer func() {
		// `$$` is process-id; multiple test runs may target the same
		// sentinel if the test reuses one. Clean using the literal
		// basename pattern.
		_ = os.Remove("/tmp/nodeup-pwned-$$")
	}()

	// Compose a shell script that runs `id -un` after sourcing a
	// non-existent file (a no-op for our purposes) but that, if the
	// path were expanded by bash, would touch the sentinel. We then
	// compare the printed value to the literal input — they must match.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// `printf '%s'` echoes the literal value of the quoted form; if
	// bash expanded `$(touch …)`, the touch side-effect would run AND
	// the printed value would be the empty string (touch produces no
	// stdout).
	script := "printf '%s' " + QuotePath(malicious)
	res, err := RunShellScript(ctx, script)
	if err != nil {
		t.Fatalf("RunShellScript: %v", err)
	}
	if res.Stdout != malicious {
		t.Errorf("round-trip mismatch: in=%q quoted=%q got=%q (bash expanded the quoted path)",
			malicious, QuotePath(malicious), res.Stdout)
	}
	if _, err := os.Stat("/tmp/nodeup-pwned-$$"); err == nil {
		t.Errorf("sentinel file /tmp/nodeup-pwned-$$ exists — bash expanded a metacharacter from the path, command injection is NOT neutralized")
	}
}

// TestQuotePathRoundTrip confirms that the output of QuotePath, when
// embedded in a shell script, re-evaluates to the original path. We
// shell out via RunShellScript and `printf '%s'` the quoted form, then
// compare to the original input.
//
// On POSIX we now expect round-trip success for the full set of
// metacharacters ($, `, |) because QuotePath uses single quotes that
// disable shell expansion entirely. On Windows, the round-trip is
// not meaningful for cmd.exe (different unquote rules) — TestQuotePath
// covers per-OS expected output instead.
func TestQuotePathRoundTrip(t *testing.T) {
	if runtime.GOOS == "plan9" {
		t.Skip("plan9 shell semantics differ; skip")
	}
	if runtime.GOOS == "windows" {
		t.Skip("cmd.exe unquote rules differ from bash; TestQuotePath covers per-OS expected output")
	}
	paths := []string{
		"/Users/dipto/My App",
		"/Users/Double  Space/bin",
		"/tmp/oops\"quote/x",
		// Regression cases for #43: these now round-trip correctly
		// because single-quote wrapping disables expansion.
		"/home/$USER/foo",
		"/tmp/`whoami`/x",
		"/tmp/it's/a/path",
		"/tmp/$(id -u)/x",
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
