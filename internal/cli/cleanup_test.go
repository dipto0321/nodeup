package cli

import (
	"bufio"
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/Masterminds/semver/v3"

	"github.com/dipto0321/nodeup/internal/detector"
)

// stubManager is a minimal Manager implementation that records
// Uninstall calls so tests can assert which versions were deleted.
// Every other method returns a zero value — the cleanup prompt only
// exercises Uninstall + (optionally) Current.
type stubManager struct {
	name       string
	uninstalls []semver.Version
	failOn     map[string]error // version string → error to return
	currentV   *semver.Version  // nil = Current() returns an error
	currentErr error
}

func (s *stubManager) Name() string                                   { return s.name }
func (s *stubManager) Detect() bool                                   { return true }
func (s *stubManager) Version() (string, error)                       { return "0.0.0-test", nil }
func (s *stubManager) ListInstalled() ([]semver.Version, error)       { return nil, nil }
func (s *stubManager) Install(semver.Version) error                   { return nil }
func (s *stubManager) Use(semver.Version) error                       { return nil }
func (s *stubManager) SetDefault(semver.Version) error                { return nil }
func (s *stubManager) GlobalNpmPrefix(semver.Version) (string, error) { return "", nil }
func (s *stubManager) Current() (semver.Version, error) {
	if s.currentErr != nil {
		return semver.Version{}, s.currentErr
	}
	if s.currentV == nil {
		return semver.Version{}, errors.New("no active version")
	}
	return *s.currentV, nil
}
func (s *stubManager) Uninstall(v semver.Version) error {
	if err, ok := s.failOn[v.String()]; ok {
		return err
	}
	s.uninstalls = append(s.uninstalls, v)
	return nil
}

// newCleanupIO bundles an in-memory stdin/stdout pair for prompt
// tests. Tests pipe user input through `in` and read the rendered
// prompt from `out` for assertions.
func newCleanupIO(input string) (cleanupIO, *bytes.Buffer) {
	var out bytes.Buffer
	return cleanupIO{
		in:  bufio.NewReader(strings.NewReader(input)),
		out: &out,
	}, &out
}

func mustVer(t *testing.T, s string) semver.Version {
	t.Helper()
	v, err := semver.NewVersion(s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return *v
}

// --- cleanupCandidates -------------------------------------------------

func TestCleanupCandidates_ExcludesNewAndActive(t *testing.T) {
	installed := []semver.Version{
		mustVer(t, "18.20.4"),
		mustVer(t, "20.18.0"),
		mustVer(t, "22.11.0"),
		mustVer(t, "24.15.0"),
	}
	toInstall := []semver.Version{
		mustVer(t, "22.11.0"), // new LTS — exclude
		mustVer(t, "24.15.0"), // new Current — exclude
	}
	active := mustVer(t, "20.18.0") // currently active — exclude

	got := cleanupCandidates(toInstall, installed, active)
	want := []string{"18.20.4"}
	if len(got) != len(want) {
		t.Fatalf("got %d candidates (%v), want %d (%v)", len(got), got, len(want), want)
	}
	for i, w := range want {
		if got[i].String() != w {
			t.Errorf("got[%d] = %s, want %s", i, got[i], w)
		}
	}
}

func TestCleanupCandidates_NoActive(t *testing.T) {
	// When m.Current() errors out (e.g., nvm-windows), we pass the
	// zero semver.Version — exclusion should still apply to
	// toInstall but not to any "active" version (since none was
	// detected).
	installed := []semver.Version{
		mustVer(t, "18.20.4"),
		mustVer(t, "22.11.0"),
	}
	toInstall := []semver.Version{mustVer(t, "22.11.0")}

	got := cleanupCandidates(toInstall, installed, semver.Version{})
	want := []string{"18.20.4"}
	if len(got) != len(want) {
		t.Fatalf("got %d, want %d", len(got), len(want))
	}
	if got[0].String() != want[0] {
		t.Errorf("got %s, want %s", got[0], want[0])
	}
}

func TestCleanupCandidates_AllExcluded(t *testing.T) {
	// Every installed version is either new or active → nothing left.
	installed := []semver.Version{
		mustVer(t, "22.11.0"),
		mustVer(t, "20.18.0"),
	}
	got := cleanupCandidates(installed, installed, mustVer(t, "20.18.0"))
	if len(got) != 0 {
		t.Errorf("expected empty candidates, got %v", got)
	}
}

func TestCleanupCandidates_EmptyInstalled(t *testing.T) {
	got := cleanupCandidates(nil, nil, semver.Version{})
	if len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}
}

// --- runCleanupPrompt --------------------------------------------------

func TestCleanupPrompt_EmptyStdinSkipsAll(t *testing.T) {
	// Empty input = no. Default N means skip everything.
	streams, out := newCleanupIO("")
	mgr := &stubManager{name: "fnm"}
	cfg := cleanupConfig{PerVersion: true}

	candidates := []semver.Version{mustVer(t, "18.20.4")}
	result, err := runCleanupPrompt(cfg, nil, candidates, semver.Version{}, mgr, streams)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Deleted) != 0 {
		t.Errorf("expected 0 deleted, got %v", result.Deleted)
	}
	if !strings.Contains(out.String(), "Cleanup skipped") {
		t.Errorf("expected skip message, got %q", out.String())
	}
}

func TestCleanupPrompt_YesDeletesAll(t *testing.T) {
	streams, _ := newCleanupIO("y\n")
	mgr := &stubManager{name: "fnm"}
	cfg := cleanupConfig{PerVersion: false} // no per-version confirm so "y\n" deletes everything

	candidates := []semver.Version{
		mustVer(t, "18.20.4"),
		mustVer(t, "20.18.0"),
	}
	result, err := runCleanupPrompt(cfg, nil, candidates, semver.Version{}, mgr, streams)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Deleted) != 2 {
		t.Errorf("expected 2 deleted, got %v", result.Deleted)
	}
	if len(mgr.uninstalls) != 2 {
		t.Errorf("expected 2 Uninstall calls, got %v", mgr.uninstalls)
	}
}

func TestCleanupPrompt_PerVersionConfirm(t *testing.T) {
	// With PerVersion=true, "y" to all-or-nothing still requires a
	// per-version "y" to actually delete. User says y, y, n — only
	// the first should be deleted.
	streams, _ := newCleanupIO("y\ny\nn\n")
	mgr := &stubManager{name: "fnm"}
	cfg := cleanupConfig{PerVersion: true}

	candidates := []semver.Version{
		mustVer(t, "18.20.4"),
		mustVer(t, "20.18.0"),
	}
	result, err := runCleanupPrompt(cfg, nil, candidates, semver.Version{}, mgr, streams)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Deleted) != 1 {
		t.Errorf("expected 1 deleted, got %v", result.Deleted)
	}
	if len(result.Skipped) != 1 {
		t.Errorf("expected 1 skipped, got %v", result.Skipped)
	}
	if result.Deleted[0].String() != "18.20.4" {
		t.Errorf("expected 18.20.4 deleted, got %s", result.Deleted[0])
	}
	if result.Skipped[0].String() != "20.18.0" {
		t.Errorf("expected 20.18.0 skipped, got %s", result.Skipped[0])
	}
}

func TestCleanupPrompt_NoSkips(t *testing.T) {
	streams, out := newCleanupIO("n\n")
	mgr := &stubManager{name: "fnm"}
	cfg := cleanupConfig{PerVersion: true}

	candidates := []semver.Version{mustVer(t, "18.20.4")}
	result, err := runCleanupPrompt(cfg, nil, candidates, semver.Version{}, mgr, streams)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Deleted) != 0 {
		t.Errorf("expected 0 deleted, got %v", result.Deleted)
	}
	if !strings.Contains(out.String(), "Cleanup skipped") {
		t.Errorf("expected skip message, got %q", out.String())
	}
}

func TestCleanupPrompt_SpecificVersion(t *testing.T) {
	// User picks "20.18.0" by typing it. We should delete only that one.
	streams, _ := newCleanupIO("20.18.0\n")
	mgr := &stubManager{name: "fnm"}
	cfg := cleanupConfig{PerVersion: false} // no per-version confirm

	candidates := []semver.Version{
		mustVer(t, "18.20.4"),
		mustVer(t, "20.18.0"),
		mustVer(t, "22.11.0"),
	}
	result, err := runCleanupPrompt(cfg, nil, candidates, semver.Version{}, mgr, streams)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Deleted) != 1 {
		t.Errorf("expected 1 deleted, got %v", result.Deleted)
	}
	if result.Deleted[0].String() != "20.18.0" {
		t.Errorf("expected to delete 20.18.0, got %s", result.Deleted[0])
	}
}

func TestCleanupPrompt_SpecificVersionInvalidSkips(t *testing.T) {
	// User types something that's neither y/N nor a version — treat as skip.
	streams, out := newCleanupIO("bogus\n")
	mgr := &stubManager{name: "fnm"}
	cfg := cleanupConfig{PerVersion: false}

	candidates := []semver.Version{mustVer(t, "18.20.4")}
	result, err := runCleanupPrompt(cfg, nil, candidates, semver.Version{}, mgr, streams)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Deleted) != 0 {
		t.Errorf("expected 0 deleted, got %v", result.Deleted)
	}
	if !strings.Contains(out.String(), "Cleanup skipped") {
		t.Errorf("expected skip message, got %q", out.String())
	}
}

func TestCleanupPrompt_AutoDeleteAll(t *testing.T) {
	// --cleanup: no all-or-nothing prompt; straight to deletion.
	streams, _ := newCleanupIO("")
	mgr := &stubManager{name: "fnm"}
	cfg := cleanupConfig{AutoDeleteAll: true, PerVersion: true}

	candidates := []semver.Version{
		mustVer(t, "18.20.4"),
		mustVer(t, "20.18.0"),
	}
	// Per-version prompts come AFTER AutoDeleteAll skips the first
	// prompt. We send two "y\n" responses (one per candidate).
	// To do that, build a multi-line input.
	var inBuf bytes.Buffer
	inBuf.WriteString("y\n")
	inBuf.WriteString("y\n")
	streams = cleanupIO{
		in:  bufio.NewReader(&inBuf),
		out: &bytes.Buffer{},
	}

	result, err := runCleanupPrompt(cfg, nil, candidates, semver.Version{}, mgr, streams)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Deleted) != 2 {
		t.Errorf("expected 2 deleted, got %v", result.Deleted)
	}
}

func TestCleanupPrompt_NonInteractiveNoOp(t *testing.T) {
	// --no-cleanup: no input read, no output written, no deletes.
	streams, out := newCleanupIO("")
	mgr := &stubManager{name: "fnm"}
	cfg := cleanupConfig{NonInteractive: true}

	candidates := []semver.Version{mustVer(t, "18.20.4")}
	result, err := runCleanupPrompt(cfg, nil, candidates, semver.Version{}, mgr, streams)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Deleted) != 0 {
		t.Errorf("expected 0 deleted, got %v", result.Deleted)
	}
	if out.Len() != 0 {
		t.Errorf("expected no output for non-interactive, got %q", out.String())
	}
}

func TestCleanupPrompt_NoCandidatesNoPrompt(t *testing.T) {
	// When every installed version is excluded, no prompt appears.
	streams, out := newCleanupIO("")
	mgr := &stubManager{name: "fnm"}
	cfg := cleanupConfig{PerVersion: true}

	installed := []semver.Version{mustVer(t, "22.11.0")}
	result, err := runCleanupPrompt(cfg, installed, installed, mustVer(t, "22.11.0"), mgr, streams)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Deleted) != 0 {
		t.Errorf("expected 0 deleted, got %v", result.Deleted)
	}
	if !strings.Contains(out.String(), "No old versions to clean up") {
		t.Errorf("expected 'no old versions' message, got %q", out.String())
	}
}

func TestCleanupPrompt_PrefilteredOnly(t *testing.T) {
	// --cleanup-version 20.18.0: only that version should be offered,
	// and the all-or-nothing prompt should be skipped.
	streams, _ := newCleanupIO("")
	mgr := &stubManager{name: "fnm"}
	cfg := cleanupConfig{
		PerVersion:  true, // per-version prompt still happens
		Prefiltered: []semver.Version{mustVer(t, "20.18.0")},
	}

	candidates := []semver.Version{
		mustVer(t, "18.20.4"),
		mustVer(t, "20.18.0"),
		mustVer(t, "22.11.0"),
	}
	// Per-version "y" for the one prefiltered version.
	var inBuf bytes.Buffer
	inBuf.WriteString("y\n")
	streams = cleanupIO{
		in:  bufio.NewReader(&inBuf),
		out: &bytes.Buffer{},
	}

	result, err := runCleanupPrompt(cfg, nil, candidates, semver.Version{}, mgr, streams)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Deleted) != 1 {
		t.Errorf("expected 1 deleted, got %v", result.Deleted)
	}
	if result.Deleted[0].String() != "20.18.0" {
		t.Errorf("expected 20.18.0, got %s", result.Deleted[0])
	}
}

func TestCleanupPrompt_PrefilteredNoMatch(t *testing.T) {
	// --cleanup-version 99.0.0 (not installed) → friendly note,
	// no deletes.
	streams, out := newCleanupIO("")
	mgr := &stubManager{name: "fnm"}
	cfg := cleanupConfig{
		Prefiltered: []semver.Version{mustVer(t, "99.0.0")},
	}

	candidates := []semver.Version{mustVer(t, "18.20.4")}
	result, err := runCleanupPrompt(cfg, nil, candidates, semver.Version{}, mgr, streams)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Deleted) != 0 {
		t.Errorf("expected 0 deleted, got %v", result.Deleted)
	}
	if !strings.Contains(out.String(), "No matching versions") {
		t.Errorf("expected 'no matching versions' message, got %q", out.String())
	}
}

func TestCleanupPrompt_UninstallErrorCollected(t *testing.T) {
	// If Uninstall fails on one version, we still try the next.
	streams, _ := newCleanupIO("y\ny\n")
	mgr := &stubManager{
		name: "fnm",
		failOn: map[string]error{
			"18.20.4": errors.New("permission denied"),
		},
	}
	cfg := cleanupConfig{PerVersion: false}

	candidates := []semver.Version{
		mustVer(t, "18.20.4"),
		mustVer(t, "20.18.0"),
	}
	result, err := runCleanupPrompt(cfg, nil, candidates, semver.Version{}, mgr, streams)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Deleted) != 1 {
		t.Errorf("expected 1 deleted, got %v", result.Deleted)
	}
	if result.Deleted[0].String() != "20.18.0" {
		t.Errorf("expected 20.18.0 deleted, got %s", result.Deleted[0])
	}
	if len(result.Failed) != 1 {
		t.Errorf("expected 1 failure, got %v", result.Failed)
	}
	if result.Failed[0].Version.String() != "18.20.4" {
		t.Errorf("expected failed version 18.20.4, got %s", result.Failed[0].Version)
	}
}

func TestCleanupPrompt_ExcludesActive(t *testing.T) {
	// Active version (per m.Current) must NOT appear in candidates.
	streams, _ := newCleanupIO("y\n")
	mgr := &stubManager{
		name:     "fnm",
		currentV: ptrVer("20.18.0"),
	}
	cfg := cleanupConfig{PerVersion: false}

	installed := []semver.Version{
		mustVer(t, "18.20.4"),
		mustVer(t, "20.18.0"), // active — should be excluded
		mustVer(t, "22.11.0"),
	}
	result, err := runCleanupPrompt(cfg, nil, installed, *mgr.currentV, mgr, streams)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Deleted) != 2 {
		t.Fatalf("expected 2 deleted (active excluded), got %v", result.Deleted)
	}
	for _, v := range result.Deleted {
		if v.String() == "20.18.0" {
			t.Errorf("active version 20.18.0 should NOT have been deleted")
		}
	}
}

func TestCleanupPrompt_ExcludesNewVersions(t *testing.T) {
	// Newly-installed versions must NOT appear in candidates.
	streams, _ := newCleanupIO("y\n")
	mgr := &stubManager{name: "fnm"}
	cfg := cleanupConfig{PerVersion: false}

	installed := []semver.Version{
		mustVer(t, "18.20.4"),
		mustVer(t, "22.11.0"), // new LTS — exclude
		mustVer(t, "24.15.0"), // new Current — exclude
	}
	toInstall := []semver.Version{
		mustVer(t, "22.11.0"),
		mustVer(t, "24.15.0"),
	}
	result, err := runCleanupPrompt(cfg, toInstall, installed, semver.Version{}, mgr, streams)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Deleted) != 1 {
		t.Fatalf("expected 1 deleted (new versions excluded), got %v", result.Deleted)
	}
	if result.Deleted[0].String() != "18.20.4" {
		t.Errorf("expected 18.20.4 deleted, got %s", result.Deleted[0])
	}
}

// --- intersectCandidates -----------------------------------------------

func TestIntersectCandidates_OrderPreserved(t *testing.T) {
	candidates := []semver.Version{
		mustVer(t, "18.20.4"),
		mustVer(t, "20.18.0"),
		mustVer(t, "22.11.0"),
	}
	want := []semver.Version{
		mustVer(t, "22.11.0"), // user asked in this order
		mustVer(t, "18.20.4"),
	}
	got := intersectCandidates(candidates, want)
	if len(got) != 2 {
		t.Fatalf("got %d, want 2", len(got))
	}
	if got[0].String() != "22.11.0" || got[1].String() != "18.20.4" {
		t.Errorf("order not preserved: got %v", got)
	}
}

func TestIntersectCandidates_FiltersMissing(t *testing.T) {
	candidates := []semver.Version{mustVer(t, "18.20.4")}
	want := []semver.Version{
		mustVer(t, "18.20.4"),
		mustVer(t, "99.0.0"), // not installed
	}
	got := intersectCandidates(candidates, want)
	if len(got) != 1 {
		t.Fatalf("got %d, want 1", len(got))
	}
	if got[0].String() != "18.20.4" {
		t.Errorf("got %s, want 18.20.4", got[0])
	}
}

// --- formatCleanupResult -----------------------------------------------

func TestFormatCleanupResult_Empty(t *testing.T) {
	if got := formatCleanupResult(cleanupResult{}); got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestFormatCleanupResult_DeletedOnly(t *testing.T) {
	r := cleanupResult{
		Deleted: []semver.Version{mustVer(t, "18.20.4"), mustVer(t, "20.18.0")},
	}
	got := formatCleanupResult(r)
	if !strings.Contains(got, "v18.20.4") || !strings.Contains(got, "v20.18.0") {
		t.Errorf("got %q, expected both versions listed", got)
	}
	if !strings.HasPrefix(got, "Deleted:") {
		t.Errorf("got %q, expected leading 'Deleted:'", got)
	}
}

func TestFormatCleanupResult_WithSkippedAndFailed(t *testing.T) {
	r := cleanupResult{
		Deleted: []semver.Version{mustVer(t, "18.20.4")},
		Skipped: []semver.Version{mustVer(t, "20.18.0")},
		Failed:  []cleanupFailure{{Version: mustVer(t, "22.11.0"), Err: errors.New("nope")}},
	}
	got := formatCleanupResult(r)
	for _, want := range []string{"v18.20.4", "v20.18.0", "1 failed"} {
		if !strings.Contains(got, want) {
			t.Errorf("got %q, expected to contain %q", got, want)
		}
	}
}

// --- helpers ------------------------------------------------------------

func ptrVer(s string) *semver.Version {
	v, err := semver.NewVersion(s)
	if err != nil {
		panic(err)
	}
	return v
}

// Compile-time check that stubManager implements detector.Manager.
var _ detector.Manager = (*stubManager)(nil)
