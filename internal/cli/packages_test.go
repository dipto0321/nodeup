package cli

import (
	"bytes"
	"strings"
	"testing"
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
