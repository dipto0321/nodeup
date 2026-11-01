package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Masterminds/semver/v3"
)

// TestRunRestore_RejectsPathTraversal exercises the manager-name
// allowlist added for #51. Calling runRestore with a positional
// manager arg like `../../tmp/evil` must fail BEFORE any
// snapshotPath is computed — the traversal closes the
// confused-deputy surface where the user controls the manager name
// and the result string is used to build a filesystem path.
func TestRunRestore_RejectsPathTraversal(t *testing.T) {
	cmd := newRestoreCmd()

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"../../tmp/evil", "1.0.0"})

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("runRestore(%q): expected error, got nil", "../../tmp/evil")
	}
	if !strings.Contains(err.Error(), "invalid manager name") {
		t.Fatalf("runRestore: err = %v, want message containing 'invalid manager name'", err)
	}
	if !strings.Contains(err.Error(), "../../tmp/evil") {
		t.Fatalf("runRestore: err = %v, want the offender name in the message", err)
	}
	// The allowlist must be surfaced verbatim so the user can fix
	// the typo without grepping our docs.
	if !strings.Contains(err.Error(), "fnm") {
		t.Errorf("runRestore: err = %v, want allowlist (e.g. 'fnm') in message", err)
	}
}

// TestRunDiff_RejectsPathTraversal mirrors the restore test for the
// diff subcommand. The diff codepath also passes the user-supplied
// manager name straight into snapshotPath via LoadSnapshot, with
// the same path-traversal exposure.
func TestRunDiff_RejectsPathTraversal(t *testing.T) {
	cmd := newDiffCmd()

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"../../tmp/evil", "1.0.0", "2.0.0"})

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("runDiff(%q): expected error, got nil", "../../tmp/evil")
	}
	if !strings.Contains(err.Error(), "invalid manager name") {
		t.Fatalf("runDiff: err = %v, want message containing 'invalid manager name'", err)
	}
}

// TestRunRestore_AcceptsCanonicalName verifies the validator doesn't
// reject every input — a regression that flipped the boolean would
// be caught by the negative cases above, but a regression that
// rejected all names would slip past them.
//
// We give cobra a single positional arg per the leaf's ExactArgs(2)
// validator (matches the real CLI shape), and assert: the error path
// must NOT come from the allowlist. Whether the command succeeds,
// fails at the snapshot read, or fails at the npm install, doesn't
// matter — only that the validator let the canonical name through.
func TestRunRestore_AcceptsCanonicalName(t *testing.T) {
	// Build the leaf directly so we don't trigger the parent's
	// Args validator via SetArgs.
	cmd := newRestoreCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"fnm", "20.0.0"})

	err := cmd.Execute()
	if err == nil {
		// On a machine with fnm installed and a real snapshot
		// file at <DataDir>/snapshots/fnm-20.0.0.json, the
		// command could in principle succeed. We don't depend on
		// that — we only care that the error (if any) is not the
		// allowlist message.
		return
	}
	if strings.Contains(err.Error(), "invalid manager name") {
		t.Errorf("runRestore(\"fnm\"): allowlist wrongly rejected canonical name: %v", err)
	}
}

// stubCurrentManager is a detector.Manager whose Current() answer is
// configurable, used to pin getCurrentVersion's behavior.
type stubCurrentManager struct {
	name       string
	current    semver.Version
	currentErr error
	installed  []semver.Version
}

func (s *stubCurrentManager) Name() string             { return s.name }
func (s *stubCurrentManager) Detect() bool             { return true }
func (s *stubCurrentManager) Version() (string, error) { return "0.0.0-test", nil }
func (s *stubCurrentManager) ListInstalled(context.Context) ([]semver.Version, error) {
	return s.installed, nil
}
func (s *stubCurrentManager) Install(semver.Version) error                   { return nil }
func (s *stubCurrentManager) Uninstall(semver.Version) error                 { return nil }
func (s *stubCurrentManager) Use(semver.Version) error                       { return nil }
func (s *stubCurrentManager) SetDefault(semver.Version) error                { return nil }
func (s *stubCurrentManager) GlobalNpmPrefix(semver.Version) (string, error) { return "", nil }
func (s *stubCurrentManager) Current(context.Context) (semver.Version, error) {
	if s.currentErr != nil {
		return semver.Version{}, s.currentErr
	}
	return s.current, nil
}

// TestGetCurrentVersion_PrefersManagerCurrent pins that
// `nodeup packages snapshot` captures the ACTIVE version's globals.
//
// The previous implementation returned versions[0] — the LOWEST
// installed version, because ListInstalled sorts ascending. On a
// machine with Node 18 and Node 20 that captured the (usually empty)
// 18 install under the 20.x filename, producing a snapshot that was
// useless as a restore source. See #128.
func TestGetCurrentVersion_PrefersManagerCurrent(t *testing.T) {
	cur := *semver.MustParse("20.10.0")
	m := &stubCurrentManager{
		name:      "fnm",
		current:   cur,
		installed: []semver.Version{*semver.MustParse("18.0.0"), cur},
	}

	got, err := getCurrentVersion(context.Background(), m)
	if err != nil {
		t.Fatalf("getCurrentVersion: %v", err)
	}
	if got.String() != "20.10.0" {
		t.Errorf("getCurrentVersion = %s, want 20.10.0 (the active version)", got)
	}
}

// TestGetCurrentVersion_FallsBackToHighestInstalled covers the
// managers that cannot report an active version (nvm-windows returns
// "not implemented"). The fallback must be the HIGHEST installed
// version, not the lowest.
func TestGetCurrentVersion_FallsBackToHighestInstalled(t *testing.T) {
	m := &stubCurrentManager{
		name:       "nvm-windows",
		currentErr: errors.New("not implemented"),
		installed:  []semver.Version{*semver.MustParse("18.0.0"), *semver.MustParse("20.10.0"), *semver.MustParse("22.0.0")},
	}

	got, err := getCurrentVersion(context.Background(), m)
	if err != nil {
		t.Fatalf("getCurrentVersion: %v", err)
	}
	if got.String() != "22.0.0" {
		t.Errorf("getCurrentVersion = %s, want 22.0.0 (the highest installed version)", got)
	}
}

// TestGetCurrentVersion_NoVersionsErrors verifies the empty case.
func TestGetCurrentVersion_NoVersionsErrors(t *testing.T) {
	m := &stubCurrentManager{
		name:       "fnm",
		currentErr: errors.New("no active version"),
	}
	if _, err := getCurrentVersion(context.Background(), m); err == nil {
		t.Error("getCurrentVersion with no versions = nil error, want an error")
	}
}
