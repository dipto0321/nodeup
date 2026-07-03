package detector

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Masterminds/semver/v3"

	"github.com/dipto0321/nodeup/internal/platform"
)

// fakeEntry is a minimal os.DirEntry implementation for tests. The real
// os.ReadDir returns *os.DirEntry-of-fs.FileInfo, but constructing those
// by hand is verbose; this stub lets us hand a canned list of names +
// IsDir flag to parseNVMInstalledEntries.
type fakeEntry struct {
	name  string
	isDir bool
}

func (f fakeEntry) Name() string               { return f.name }
func (f fakeEntry) IsDir() bool                { return f.isDir }
func (f fakeEntry) Type() os.FileMode          { return modeFromIsDir(f.isDir) }
func (f fakeEntry) Info() (os.FileInfo, error) { return nil, os.ErrNotExist }

// modeFromIsDir turns a bool into a FileMode bitmask. Used for Type(),
// which some os.DirEntry consumers call instead of IsDir().
func modeFromIsDir(d bool) os.FileMode {
	if d {
		return os.ModeDir
	}
	return 0
}

// withStubListDir swaps the package-level listDir var for the duration
// of one test, returning a cleanup hook via t.Cleanup. The stub ignores
// the path argument and always returns the supplied entries; tests that
// care about the path should assert on the helper's own bookkeeping.
func withStubListDir(t *testing.T, entries []os.DirEntry, err error) {
	t.Helper()
	orig := listDir
	listDir = func(string) ([]os.DirEntry, error) {
		return entries, err
	}
	t.Cleanup(func() { listDir = orig })
}

// withStubScript swaps the package-level runScript var for the duration
// of one test. The stub returns the supplied stdout and nil error,
// unless err is non-nil. It also captures the script that was passed
// in, which lets tests assert it contains the expected invocation
// (e.g. "nvm --version").
func withStubScript(t *testing.T, stdout string, err error) *scriptRecorder {
	t.Helper()
	orig := runScript
	rec := &scriptRecorder{}
	runScript = func(_ context.Context, script string) (*platform.RunResult, error) {
		rec.script = script
		return &platform.RunResult{Stdout: stdout, Stderr: "", ExitCode: 0}, err
	}
	t.Cleanup(func() { runScript = orig })
	return rec
}

// scriptRecorder captures the most recent script passed to runScript.
// Used to assert that Version() builds the expected shell command.
type scriptRecorder struct {
	script string
}

// --- parseNVMVersion ----------------------------------------------------

func TestParseNVMVersion_BareVersion(t *testing.T) {
	got, err := parseNVMVersion("0.40.5\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "0.40.5" {
		t.Errorf("got %q, want %q", got, "0.40.5")
	}
}

func TestParseNVMVersion_WithPrefix(t *testing.T) {
	// Older nvm versions print "nvm 0.39.7".
	got, err := parseNVMVersion("nvm 0.39.7\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "0.39.7" {
		t.Errorf("got %q, want %q", got, "0.39.7")
	}
}

func TestParseNVMVersion_TrimsWhitespace(t *testing.T) {
	got, err := parseNVMVersion("   0.40.5   \n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "0.40.5" {
		t.Errorf("got %q, want %q", got, "0.40.5")
	}
}

func TestParseNVMVersion_Empty(t *testing.T) {
	_, err := parseNVMVersion("")
	if err == nil {
		t.Error("expected error on empty input")
	}
}

func TestParseNVMVersion_WhitespaceOnly(t *testing.T) {
	_, err := parseNVMVersion("   \n\t  ")
	if err == nil {
		t.Error("expected error on whitespace-only input")
	}
}

// --- parseNVMInstalledEntries -------------------------------------------

func TestParseNVMInstalledEntries_StandardLayout(t *testing.T) {
	// Real nvm layout: directory entries named "vX.Y.Z".
	entries := []os.DirEntry{
		fakeEntry{name: "v18.14.0", isDir: true},
		fakeEntry{name: "v16.19.0", isDir: true},
		fakeEntry{name: "v19.6.0", isDir: true},
	}
	got, err := parseNVMInstalledEntries(entries)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"16.19.0", "18.14.0", "19.6.0"}
	if len(got) != len(want) {
		t.Fatalf("got %d versions, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i].String() != w {
			t.Errorf("version[%d] = %q, want %q", i, got[i].String(), w)
		}
	}
}

func TestParseNVMInstalledEntries_AcceptsBareVersions(t *testing.T) {
	// Some installs drop the v prefix.
	entries := []os.DirEntry{
		fakeEntry{name: "18.14.0", isDir: true},
		fakeEntry{name: "16.19.0", isDir: true},
	}
	got, err := parseNVMInstalledEntries(entries)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d versions, want 2", len(got))
	}
	if got[0].String() != "16.19.0" || got[1].String() != "18.14.0" {
		t.Errorf("sort order wrong: %v", got)
	}
}

func TestParseNVMInstalledEntries_SkipsSystem(t *testing.T) {
	entries := []os.DirEntry{
		fakeEntry{name: "system", isDir: true},
		fakeEntry{name: "v18.14.0", isDir: true},
	}
	got, err := parseNVMInstalledEntries(entries)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 version, got %d", len(got))
	}
	if got[0].String() != "18.14.0" {
		t.Errorf("got %q, want 18.14.0", got[0].String())
	}
}

func TestParseNVMInstalledEntries_SkipsNonDirs(t *testing.T) {
	// nvm sometimes leaves a "lts" symlink or other files in this dir.
	entries := []os.DirEntry{
		fakeEntry{name: "v18.14.0", isDir: true},
		fakeEntry{name: "lts", isDir: false},
		fakeEntry{name: ".DS_Store", isDir: false},
	}
	got, err := parseNVMInstalledEntries(entries)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 version, got %d", len(got))
	}
}

func TestParseNVMInstalledEntries_SkipsUnparseable(t *testing.T) {
	entries := []os.DirEntry{
		fakeEntry{name: "v18.14.0", isDir: true},
		fakeEntry{name: "weird-name", isDir: true},
	}
	got, err := parseNVMInstalledEntries(entries)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 version, got %d", len(got))
	}
}

func TestParseNVMInstalledEntries_Empty(t *testing.T) {
	got, err := parseNVMInstalledEntries(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Error("expected non-nil empty slice")
	}
	if len(got) != 0 {
		t.Errorf("expected 0 versions, got %d", len(got))
	}
}

func TestParseNVMInstalledEntries_OnlySystem(t *testing.T) {
	entries := []os.DirEntry{
		fakeEntry{name: "system", isDir: true},
	}
	got, err := parseNVMInstalledEntries(entries)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Error("expected non-nil empty slice (only-system case)")
	}
	if len(got) != 0 {
		t.Errorf("expected 0 versions, got %d", len(got))
	}
}

// --- NVM.Name -----------------------------------------------------------

func TestNVM_Name(t *testing.T) {
	if got := NewNVM().Name(); got != "nvm" {
		t.Errorf("got %q, want %q", got, "nvm")
	}
}

// --- NVM.Detect ---------------------------------------------------------

// withStubNVMScript creates a real on-disk nvm.sh file in a temp dir
// and points NVM_DIR at it. This is the cleanest way to test Detect:
// the production code reads nvm.sh via os.Stat, and stubbing os.Stat
// would be heavier than just writing a one-line file.
func withStubNVMScript(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	// nvm.sh is a real shell script; an empty file is enough to satisfy
	// the existence check. The contents are not sourced in Detect.
	if err := os.WriteFile(filepath.Join(dir, "nvm.sh"), []byte("# stub nvm\n"), 0o644); err != nil {
		t.Fatalf("write nvm.sh: %v", err)
	}
	t.Setenv("NVM_DIR", dir)
	t.Cleanup(func() { t.Setenv("NVM_DIR", "") })
}

func TestNVM_Detect_TrueWhenNVMScriptExists(t *testing.T) {
	withStubNVMScript(t)
	if !NewNVM().Detect() {
		t.Error("Detect returned false with nvm.sh present")
	}
}

func TestNVM_Detect_FalseWhenNoNVMScript(t *testing.T) {
	// Point NVM_DIR at an empty temp dir — no nvm.sh inside.
	t.Setenv("NVM_DIR", t.TempDir())
	// And make sure HOME-based fallback doesn't accidentally find a
	// real install. t.TempDir() lives under os.TempDir() which is not
	// $HOME, so the ~/.nvm fallback will hit a path that doesn't
	// exist on the test machine (unless the developer happens to have
	// ~/.nvm — and on CI runners it won't). We accept that small risk
	// in exchange for test simplicity.
	if NewNVM().Detect() {
		// Only fail if HOME-based fallback would have found something.
		// Check by computing the fallback path and seeing if it exists.
		home, err := os.UserHomeDir()
		if err != nil {
			t.Fatalf("Detect returned true unexpectedly: %v", err)
		}
		if _, err := os.Stat(filepath.Join(home, ".nvm", "nvm.sh")); err == nil {
			t.Skip("developer has ~/.nvm/nvm.sh; cannot test the negative case")
		}
		t.Error("Detect returned true with no nvm.sh")
	}
}

// --- NVM.Version -------------------------------------------------------

func TestNVM_Version_BareOutput(t *testing.T) {
	withStubNVMScript(t)
	rec := withStubScript(t, "0.40.5\n", nil)

	got, err := NewNVM().Version()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "0.40.5" {
		t.Errorf("got %q, want %q", got, "0.40.5")
	}
	// Sanity: the script we built should source nvm.sh and call nvm --version.
	if !strings.Contains(rec.script, "nvm --version") {
		t.Errorf("script missing nvm --version: %q", rec.script)
	}
	if !strings.Contains(rec.script, "source ") {
		t.Errorf("script missing source command: %q", rec.script)
	}
}

func TestNVM_Version_PropagatesScriptError(t *testing.T) {
	withStubNVMScript(t)
	withStubScript(t, "", errSentinelForTest)

	_, err := NewNVM().Version()
	if err == nil {
		t.Fatal("expected error when runScript fails")
	}
}

func TestNVM_Version_NoNVMScript(t *testing.T) {
	// NVM_DIR pointing at an empty temp dir.
	t.Setenv("NVM_DIR", t.TempDir())
	if _, err := NewNVM().Version(); err == nil {
		t.Error("expected error when nvm cannot be located")
	}
}

// TestNVM_Version_NVMDirShellInjectionIsNeutralized is the regression
// test for #43. Even when NVM_DIR contains shell metacharacters like
// `$(touch …)` or backticks, the script that runs through runScript
// must keep those characters LITERAL — bash must not perform command
// substitution inside the single-quoted region that QuotePath emits.
//
// The test sets NVM_DIR to a string that contains a command-substitution
// payload but does NOT require a real directory to exist (nvm.sh is
// stubbed via runScript), captures the script that NVM.Version() builds,
// and asserts that the payload characters appear inside a single-quoted
// span rather than as bare command substitution.
func TestNVM_Version_NVMDirShellInjectionIsNeutralized(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("cmd.exe unquote rules differ; QuotePath's Windows branch is covered in internal/platform tests")
	}

	// Pick a payload directory NAME that QuotePath must absorb
	// verbatim — we never create it on disk, but nvmDir()/nvmScriptPath()
	// only return the strings, never validate them, and the runScript
	// stub returns canned stdout without touching the filesystem.
	//
	// The bytes `$(touch /tmp/PWND_BY_NVM_DIR)` are just path bytes,
	// not actual shell substitution. NVM_DIR is set to a path that
	// looks like `/tmp/<tmpdir>/$(touch /tmp/PWND_BY_NVM_DIR)`.
	maliciousDirName := "$(touch /tmp/PWND_BY_NVM_DIR)"
	t.Setenv("NVM_DIR", "/tmp/anything-at-all/"+maliciousDirName)
	// Make sure no leftover sentinel from a previous run.
	_ = os.Remove("/tmp/PWND_BY_NVM_DIR")
	defer os.Remove("/tmp/PWND_BY_NVM_DIR")

	rec := withStubScript(t, "0.40.5\n", nil)
	if _, err := NewNVM().Version(); err != nil {
		t.Fatalf("Version(): %v", err)
	}

	// The script MUST contain the payload inside single quotes, not
	// as bare `$(touch …)`. Otherwise bash would expand it the moment
	// the script runs in production.
	script := rec.script

	// 1. The malicious substring must be inside a single-quoted span.
	//    nvmScriptPath() appends `/nvm.sh`, so the full quoted form
	//    is `…/$(touch /tmp/PWND_BY_NVM_DIR)/nvm.sh`. We find the
	//    payload's byte offset and walk backwards/forward to the
	//    nearest `'` on each side; both must surround the payload,
	//    proving it lives inside a single-quoted region.
	idx := strings.Index(script, "$(touch")
	if idx < 0 {
		t.Fatalf("payload substring %q not present in script — the malicious NVM_DIR was not used. script=%q",
			"$(touch", script)
	}
	leftQuote := strings.LastIndex(script[:idx], "'")
	rightQuote := strings.Index(script[idx:], "'")
	if leftQuote < 0 || rightQuote < 0 {
		t.Fatalf("payload not surrounded by single quotes on both sides. leftQuote=%d rightQuote=%d script=%q",
			leftQuote, rightQuote, script)
	}
	// If the script also contains double-quotes around the payload,
	// bash would still perform command-substitution inside them.
	// We tolerate overall double-quotes elsewhere in the script;
	// the regression check is specifically that the payload bytes are
	// not in an unquoted region.

	// 2. The payload substring `$(touch` (if present in the script
	//    as bare bytes) must appear only inside a single-quoted span.
	//    If QuotePath passed it through bare, that's a regression.
	if idx := strings.Index(script, "$(touch"); idx >= 0 {
		// Count single quotes BEFORE the payload occurrence: odd
		// count means we are inside a single-quoted span.
		upTo := script[:idx]
		singleQuoteCount := strings.Count(upTo, "'")
		if singleQuoteCount%2 == 0 {
			t.Errorf("payload substring %q at offset %d is OUTSIDE single quotes — bash would expand it. script=%q",
				"$(touch", idx, script)
		}
	}
}

// --- NVM.ListInstalled -------------------------------------------------

func TestNVM_ListInstalled_HappyPath(t *testing.T) {
	withStubNVMScript(t)
	withStubListDir(t, []os.DirEntry{
		fakeEntry{name: "v18.14.0", isDir: true},
		fakeEntry{name: "v16.19.0", isDir: true},
		fakeEntry{name: "v19.6.0", isDir: true},
	}, nil)

	got, err := NewNVM().ListInstalled()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"16.19.0", "18.14.0", "19.6.0"}
	if len(got) != len(want) {
		t.Fatalf("got %d versions, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i].String() != w {
			t.Errorf("version[%d] = %q, want %q", i, got[i].String(), w)
		}
	}
}

func TestNVM_ListInstalled_NotFoundReturnsEmpty(t *testing.T) {
	withStubNVMScript(t)
	// Simulate "nvm installed, never installed a node" by returning
	// os.ErrNotExist from the stub.
	withStubListDir(t, nil, os.ErrNotExist)

	got, err := NewNVM().ListInstalled()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Error("expected non-nil empty slice")
	}
	if len(got) != 0 {
		t.Errorf("expected 0 versions, got %d", len(got))
	}
}

func TestNVM_ListInstalled_PropagatesOtherError(t *testing.T) {
	withStubNVMScript(t)
	withStubListDir(t, nil, errSentinelForTest)

	_, err := NewNVM().ListInstalled()
	if err == nil {
		t.Fatal("expected error when listDir fails with non-NotExist")
	}
}

func TestNVM_ListInstalled_NoNVMDir(t *testing.T) {
	// NVM_DIR empty AND home is unresolvable. We force the latter by
	// setting HOME="" on unix. On Windows the equivalent is USERPROFILE.
	t.Setenv("NVM_DIR", "")
	if platform.IsWindows() {
		t.Setenv("USERPROFILE", "")
	} else {
		t.Setenv("HOME", "")
	}
	_, err := NewNVM().ListInstalled()
	if err == nil {
		t.Error("expected error when NVM_DIR and HOME both empty")
	}
}

// --- Mutation methods -------------------------------------------------

func TestNVM_Install_SourcesAndInvokes(t *testing.T) {
	// The Install path must (a) source nvm.sh, (b) call `nvm install -s <v>`,
	// and (c) return nil on a clean exit. We assert all three.
	withStubNVMScript(t)
	rec := withStubScript(t, "", nil)

	v := semver.MustParse("22.11.0")
	if err := NewNVM().Install(*v); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(rec.script, "source ") {
		t.Errorf("script missing source: %q", rec.script)
	}
	if !strings.Contains(rec.script, "nvm install -s 22.11.0") {
		t.Errorf("script missing `nvm install -s 22.11.0`: %q", rec.script)
	}
}

func TestNVM_Uninstall_SourcesAndInvokes(t *testing.T) {
	withStubNVMScript(t)
	rec := withStubScript(t, "", nil)

	v := semver.MustParse("20.0.0")
	if err := NewNVM().Uninstall(*v); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(rec.script, "nvm uninstall 20.0.0") {
		t.Errorf("script missing `nvm uninstall 20.0.0`: %q", rec.script)
	}
}

func TestNVM_SetDefault_SourcesAndInvokes(t *testing.T) {
	withStubNVMScript(t)
	rec := withStubScript(t, "", nil)

	v := semver.MustParse("22.11.0")
	if err := NewNVM().SetDefault(*v); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(rec.script, "nvm alias default 22.11.0") {
		t.Errorf("script missing `nvm alias default 22.11.0`: %q", rec.script)
	}
}

func TestNVM_Uninstall_PropagatesError(t *testing.T) {
	// nvm refuses to uninstall the active version — that refusal
	// must propagate wrapped, so the CLI can show a useful message.
	withStubNVMScript(t)
	withStubScript(t, "", errSentinelForTest)

	v := semver.MustParse("20.0.0")
	err := NewNVM().Uninstall(*v)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, errSentinelForTest) {
		t.Errorf("error %v should wrap %v", err, errSentinelForTest)
	}
}

func TestNVM_Install_NoNVMScript(t *testing.T) {
	// NVM_DIR pointing at an empty temp dir — no nvm.sh inside.
	t.Setenv("NVM_DIR", t.TempDir())
	v := semver.MustParse("22.11.0")
	if err := NewNVM().Install(*v); err == nil {
		t.Error("expected error when nvm cannot be located")
	}
}

// --- parseNVMCurrent ----------------------------------------------------

func TestParseNVMCurrent_WithPrefix(t *testing.T) {
	v, err := parseNVMCurrent("v22.11.0\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.String() != "22.11.0" {
		t.Errorf("got %q, want %q", v.String(), "22.11.0")
	}
}

func TestParseNVMCurrent_BareVersion(t *testing.T) {
	v, err := parseNVMCurrent("20.18.0\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.String() != "20.18.0" {
		t.Errorf("got %q, want %q", v.String(), "20.18.0")
	}
}

func TestParseNVMCurrent_SystemIsError(t *testing.T) {
	// "system" is NOT a managed version; we must surface an error so
	// the cleanup prompt doesn't try to exclude it.
	_, err := parseNVMCurrent("system\n")
	if err == nil {
		t.Error("expected error for 'system' (not a managed version)")
	}
}

func TestParseNVMCurrent_Empty(t *testing.T) {
	_, err := parseNVMCurrent("")
	if err == nil {
		t.Error("expected error on empty input")
	}
}

// --- helpers ------------------------------------------------------------

// errSentinelForTest is the error returned by stubs to verify error
// propagation. It is a plain errors.New sentinel.
var errSentinelForTest = errors.New("simulated shell failure")
