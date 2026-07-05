package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Masterminds/semver/v3"

	"github.com/dipto0321/nodeup/internal/detector"
	"github.com/dipto0321/nodeup/internal/ui"
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

func (s *stubManager) Name() string                                            { return s.name }
func (s *stubManager) Detect() bool                                            { return true }
func (s *stubManager) Version() (string, error)                                { return "0.0.0-test", nil }
func (s *stubManager) ListInstalled(context.Context) ([]semver.Version, error) { return nil, nil }
func (s *stubManager) Install(semver.Version) error                            { return nil }
func (s *stubManager) Use(semver.Version) error                                { return nil }
func (s *stubManager) SetDefault(semver.Version) error                         { return nil }
func (s *stubManager) GlobalNpmPrefix(semver.Version) (string, error)          { return "", nil }
func (s *stubManager) Current(context.Context) (semver.Version, error) {
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

// newTestPrompt builds a (ui.Prompt, ui.Writer) pair backed by the
// input string + a captured output buffer. Callers wire the writer
// into runCleanupPrompt so the "Cleanup skipped" / "No old versions"
// messages get captured — previously tests passed nil for the
// writer and never asserted on those messages.
func newTestPrompt(t *testing.T, input string) (ui.Prompt, ui.Writer, *bytes.Buffer) {
	t.Helper()
	var out bytes.Buffer
	// ui.NewPrompt accepts (mode, in, out). For tests we always
	// use PlainMode — FancyMode requires a real TTY that go test
	// can't provide, and the unit-test surface is the line-reading
	// fallback anyway. We share the same *bytes.Buffer between the
	// prompt's "out" and the writer's "out" so all captured bytes
	// land in one buffer for assertion.
	prompt := ui.NewPrompt(ui.PlainMode, strings.NewReader(input), &out)
	writer := ui.NewWriter(ui.PlainMode, &out, &out)
	return prompt, writer, &out
}

// promptWithInput is a variant of newTestPrompt that lets callers
// build the input stream programmatically (e.g., concatenate
// multiple "y\n" lines for a per-version prompt sequence).
func promptWithInput(in *bytes.Buffer) (ui.Prompt, ui.Writer, *bytes.Buffer) {
	var out bytes.Buffer
	prompt := ui.NewPrompt(ui.PlainMode, in, &out)
	writer := ui.NewWriter(ui.PlainMode, &out, &out)
	return prompt, writer, &out
}

func mustVer(t *testing.T, s string) semver.Version {
	t.Helper()
	v, err := semver.NewVersion(s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return *v
}

func ptrVer(s string) *semver.Version {
	return semver.MustParse(s)
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
	prompt, writer, out := newTestPrompt(t, "")
	mgr := &stubManager{name: "fnm"}
	cfg := cleanupConfig{PerVersion: true}

	candidates := []semver.Version{mustVer(t, "18.20.4")}
	result, err := runCleanupPrompt(prompt, writer, cfg, nil, candidates, semver.Version{}, mgr)
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
	// In the new prompt flow, "yes" = pick the "Delete all of the
	// above" option in the Select list. That's option index 1 in
	// the options slice (index 0 = "Delete all of the above", index
	// 1 = "Skip cleanup", then per-version). Plain-mode Select
	// accepts either numeric ("1") or label match
	// ("Delete all of the above") — we use "1" to keep the test
	// focused on the cleanup decision, not label matching.
	prompt, writer, _ := newTestPrompt(t, "1\n")
	mgr := &stubManager{name: "fnm"}
	cfg := cleanupConfig{PerVersion: false} // no per-version confirm so "1\n" deletes everything

	candidates := []semver.Version{
		mustVer(t, "18.20.4"),
		mustVer(t, "20.18.0"),
	}
	result, err := runCleanupPrompt(prompt, writer, cfg, nil, candidates, semver.Version{}, mgr)
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
	// Once the user has explicitly opted into mass-delete at the
	// all-or-nothing prompt ("1" for "Delete all"), that
	// confirmation is sticky: no per-version re-prompt, no chance
	// for a non-`y` default to silently override the explicit `y`
	// from the previous step. This is the regression test for #76
	// — the pre-fix behavior was: "y" at all-or-nothing, then
	// "Delete v18.20.4? [y/N]", then "Delete v20.18.0? [y/N]",
	// with empty/non-y input silently skipping each version.
	prompt, writer, _ := newTestPrompt(t, "1\n")
	mgr := &stubManager{name: "fnm"}
	cfg := cleanupConfig{PerVersion: true}

	candidates := []semver.Version{
		mustVer(t, "18.20.4"),
		mustVer(t, "20.18.0"),
	}
	result, err := runCleanupPrompt(prompt, writer, cfg, nil, candidates, semver.Version{}, mgr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Deleted) != 2 {
		t.Errorf("expected 2 deleted (sticky confirm), got %v", result.Deleted)
	}
	if len(result.Skipped) != 0 {
		t.Errorf("expected 0 skipped, got %v", result.Skipped)
	}
	if len(mgr.uninstalls) != 2 {
		t.Errorf("expected 2 Uninstall calls, got %v", mgr.uninstalls)
	}
}

// TestCleanupPrompt_DeleteAllSkipsPerVersionPrompt is the explicit
// regression test for issue #76. It pins that when the user
// selects "Delete all" at the all-or-nothing prompt, NO per-version
// re-prompt appears — and crucially, that a short input stream
// (just "1\n" with no further answers) still deletes every
// candidate.
func TestCleanupPrompt_DeleteAllSkipsPerVersionPrompt(t *testing.T) {
	prompt, writer, out := newTestPrompt(t, "1\n")
	mgr := &stubManager{name: "fnm"}
	cfg := cleanupConfig{PerVersion: true}

	candidates := []semver.Version{
		mustVer(t, "18.20.4"),
		mustVer(t, "20.18.0"),
	}
	result, err := runCleanupPrompt(prompt, writer, cfg, nil, candidates, semver.Version{}, mgr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Deleted) != 2 {
		t.Fatalf("deleteAll must skip per-version prompt: expected 2 deleted, got %d (%v)", len(result.Deleted), result.Deleted)
	}
	if len(result.Skipped) != 0 {
		t.Errorf("deleteAll must skip per-version prompt: expected 0 skipped, got %d (%v)", len(result.Skipped), result.Skipped)
	}
	// The output must NOT contain a per-version `Delete vX.Y.Z? [y/N]`
	// confirmation question — that's the visible signal of the
	// old regression. Note: the Select option list DOES contain
	// "Delete vX.Y.Z" entries; those are intentional (they're how
	// the user picks which version to delete in the new prompt
	// flow). The marker we're guarding against is the per-version
	// `[y/N]` re-confirmation question that the bug would emit.
	if strings.Contains(out.String(), "[y/N]") {
		t.Errorf("deleteAll must skip per-version prompt; output contains a [y/N] re-confirmation:\n%s", out.String())
	}
}

func TestCleanupPrompt_NoSkips(t *testing.T) {
	// "2" = second option = "Skip cleanup".
	prompt, writer, out := newTestPrompt(t, "2\n")
	mgr := &stubManager{name: "fnm"}
	cfg := cleanupConfig{PerVersion: true}

	candidates := []semver.Version{mustVer(t, "18.20.4")}
	result, err := runCleanupPrompt(prompt, writer, cfg, nil, candidates, semver.Version{}, mgr)
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
	// With one candidate, the options list is:
	//   1: Delete all of the above
	//   2: Skip cleanup
	//   3: Delete v18.20.4
	//   4: Delete v20.18.0
	//   5: Delete v22.11.0
	// User picks "4" for v20.18.0.
	prompt, writer, _ := newTestPrompt(t, "4\n")
	mgr := &stubManager{name: "fnm"}
	cfg := cleanupConfig{PerVersion: true} // explicit pick is sticky

	candidates := []semver.Version{
		mustVer(t, "18.20.4"),
		mustVer(t, "20.18.0"),
		mustVer(t, "22.11.0"),
	}
	result, err := runCleanupPrompt(prompt, writer, cfg, nil, candidates, semver.Version{}, mgr)
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
	// Garbage input → fall back to default ("Skip cleanup" = option 2).
	prompt, writer, out := newTestPrompt(t, "bogus\n")
	mgr := &stubManager{name: "fnm"}
	cfg := cleanupConfig{PerVersion: false}

	candidates := []semver.Version{mustVer(t, "18.20.4")}
	result, err := runCleanupPrompt(prompt, writer, cfg, nil, candidates, semver.Version{}, mgr)
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
	mgr := &stubManager{name: "fnm"}
	cfg := cleanupConfig{AutoDeleteAll: true, PerVersion: true}

	candidates := []semver.Version{
		mustVer(t, "18.20.4"),
		mustVer(t, "20.18.0"),
	}
	// Per-version prompts come AFTER AutoDeleteAll skips the first
	// prompt. We send two "y\n" responses (one per candidate).
	var inBuf bytes.Buffer
	inBuf.WriteString("y\n")
	inBuf.WriteString("y\n")
	prompt, writer, _ := promptWithInput(&inBuf)

	result, err := runCleanupPrompt(prompt, writer, cfg, nil, candidates, semver.Version{}, mgr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Deleted) != 2 {
		t.Errorf("expected 2 deleted, got %v", result.Deleted)
	}
}

func TestCleanupPrompt_NonInteractiveNoOp(t *testing.T) {
	// --no-cleanup: no input read, no output written, no deletes.
	prompt, writer, out := newTestPrompt(t, "")
	mgr := &stubManager{name: "fnm"}
	cfg := cleanupConfig{NonInteractive: true}

	candidates := []semver.Version{mustVer(t, "18.20.4")}
	result, err := runCleanupPrompt(prompt, writer, cfg, nil, candidates, semver.Version{}, mgr)
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
	prompt, writer, out := newTestPrompt(t, "")
	mgr := &stubManager{name: "fnm"}
	cfg := cleanupConfig{PerVersion: true}

	installed := []semver.Version{mustVer(t, "22.11.0")}
	result, err := runCleanupPrompt(prompt, writer, cfg, installed, installed, mustVer(t, "22.11.0"), mgr)
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
	var inBuf bytes.Buffer
	inBuf.WriteString("y\n")
	prompt, writer, _ := promptWithInput(&inBuf)

	result, err := runCleanupPrompt(prompt, writer, cfg, nil, candidates, semver.Version{}, mgr)
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
	prompt, writer, out := newTestPrompt(t, "")
	mgr := &stubManager{name: "fnm"}
	cfg := cleanupConfig{
		Prefiltered: []semver.Version{mustVer(t, "99.0.0")},
	}

	candidates := []semver.Version{mustVer(t, "18.20.4")}
	result, err := runCleanupPrompt(prompt, writer, cfg, nil, candidates, semver.Version{}, mgr)
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
	// Two candidates; both answered "y\n"; first fails.
	prompt, writer, _ := newTestPrompt(t, "1\n")
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
	result, err := runCleanupPrompt(prompt, writer, cfg, nil, candidates, semver.Version{}, mgr)
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
	prompt, writer, _ := newTestPrompt(t, "1\n")
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
	result, err := runCleanupPrompt(prompt, writer, cfg, nil, installed, *mgr.currentV, mgr)
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
	prompt, writer, _ := newTestPrompt(t, "1\n")
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
	result, err := runCleanupPrompt(prompt, writer, cfg, toInstall, installed, semver.Version{}, mgr)
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

// TestCleanupPrompt_ForcePerVersionDowngradesAutoDeleteAll pins
// the fail-closed behavior added for #58. When the upgrade flow
// can't determine the active Node version (Manager.Current()
// errors), the active version is no longer in the exclusion set
// — so the cleanup candidates contain the version that's currently
// powering the user's shell. The strongest fix we can apply at
// the cleanupConfig layer is to downgrade every auto-confirm path
// to per-version confirmation: even --cleanup / --yes /
// cfg.Cleanup.Auto must NOT mass-delete.
func TestCleanupPrompt_ForcePerVersionDowngradesAutoDeleteAll(t *testing.T) {
	// With ForcePerVersion=true, AutoDeleteAll gets downgraded.
	// The flow becomes: all-or-nothing prompt → per-version prompts.
	// We answer:
	//   1. all-or-nothing: "1" (Delete all)
	//   2. per-version v18.20.4: "y" (delete)
	//   3. per-version v20.18.0: "n" (skip)
	var inBuf bytes.Buffer
	inBuf.WriteString("1\ny\nn\n")
	prompt, writer, _ := promptWithInput(&inBuf)

	mgr := &stubManager{name: "fnm"}
	cfg := cleanupConfig{
		AutoDeleteAll:   true, // would normally mass-delete
		ForcePerVersion: true, // but #58 forces per-version
	}

	candidates := []semver.Version{
		mustVer(t, "18.20.4"),
		mustVer(t, "20.18.0"),
	}
	result, err := runCleanupPrompt(prompt, writer, cfg, nil, candidates, semver.Version{}, mgr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Deleted) != 1 {
		t.Fatalf("ForcePerVersion + AutoDeleteAll: expected 1 deleted (after per-version y/n), got %d (%v)", len(result.Deleted), result.Deleted)
	}
	if result.Deleted[0].String() != "18.20.4" {
		t.Errorf("ForcePerVersion + AutoDeleteAll: expected 18.20.4 deleted (the y), got %s", result.Deleted[0])
	}
	if len(result.Skipped) != 1 {
		t.Fatalf("ForcePerVersion + AutoDeleteAll: expected 1 skipped (the n), got %d (%v)", len(result.Skipped), result.Skipped)
	}
	if result.Skipped[0].String() != "20.18.0" {
		t.Errorf("ForcePerVersion + AutoDeleteAll: expected 20.18.0 skipped (the n), got %s", result.Skipped[0])
	}
	if len(mgr.uninstalls) != 1 {
		t.Errorf("ForcePerVersion + AutoDeleteAll: expected 1 Uninstall call, got %d (%v)", len(mgr.uninstalls), mgr.uninstalls)
	}
}

// --- detect integration ------------------------------------------------

// TestStubManager_ImplementsDetectorManager pins that the stub
// satisfies the detector.Manager interface. Without this we
// wouldn't catch drift between the interface and our test
// doubles (a renamed method would compile here but break in
// production code).
func TestStubManager_ImplementsDetectorManager(t *testing.T) {
	var _ detector.Manager = (*stubManager)(nil)
}
