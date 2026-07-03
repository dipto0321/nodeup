package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/dipto0321/nodeup/internal/detector"
)

// newBufCmd returns a *cobra.Command with its output redirected to an
// in-memory buffer. Cobra's Println et al. write to OutOrStdout(); by
// overriding it we capture output without touching the real terminal.
//
// We construct a bare Cobra command here rather than going through
// NewRootCmd so the list tests stay independent of the full command
// tree (and don't trigger PersistentPreRunE / orphan-sentinel probes).
func newBufCmd(t *testing.T) (*cobra.Command, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	c := &cobra.Command{Use: "test"}
	c.SetOut(&buf)
	return c, &buf
}

// listTestStubManager implements detector.Manager with controllable
// ListInstalled/Current outputs so list-rendering helpers can be
// exercised without touching the real registry.
//
// Mirrors the stubManager in cleanup_test.go but exposes the
// ListInstalled / Current fields the list code actually reads.
type listTestStubManager struct {
	name         string
	installed    []semver.Version
	installedErr error
	currentV     *semver.Version
	currentErr   error
}

func (s *listTestStubManager) Name() string                                   { return s.name }
func (s *listTestStubManager) Detect() bool                                   { return true }
func (s *listTestStubManager) Version() (string, error)                       { return "0.0.0-test", nil }
func (s *listTestStubManager) Install(semver.Version) error                   { return nil }
func (s *listTestStubManager) Use(semver.Version) error                       { return nil }
func (s *listTestStubManager) SetDefault(semver.Version) error                { return nil }
func (s *listTestStubManager) GlobalNpmPrefix(semver.Version) (string, error) { return "", nil }
func (s *listTestStubManager) Uninstall(semver.Version) error                 { return nil }

func (s *listTestStubManager) ListInstalled(_ context.Context) ([]semver.Version, error) {
	if s.installedErr != nil {
		return nil, s.installedErr
	}
	return s.installed, nil
}

func (s *listTestStubManager) Current(_ context.Context) (semver.Version, error) {
	if s.currentErr != nil {
		return semver.Version{}, s.currentErr
	}
	if s.currentV == nil {
		return semver.Version{}, errListCurrentUnknown
	}
	return *s.currentV, nil
}

// errListCurrentUnknown is a sentinel for "no active version known";
// kept local because nothing outside this file needs it and the
// cleanup tests already define their own Current stub semantics.
var errListCurrentUnknown = strErr("no active version")

type strErr string

func (e strErr) Error() string { return string(e) }

// --- formatListVersions ---------------------------------------------------

func TestFormatListVersions_SingleAndMulti(t *testing.T) {
	// Empty case is unreachable through runList (the caller prints
	// "(no versions installed)" first) so we don't test it here —
	// matches the doc comment on formatListVersions.
	cases := []struct {
		name string
		in   []string
		want string
	}{
		{"one", []string{"20.0.0"}, "20.0.0"},
		{"two ordered", []string{"20.0.0", "22.0.0"}, "20.0.0, 22.0.0"},
		{"three ordered", []string{"18.20.4", "20.18.0", "22.11.0"}, "18.20.4, 20.18.0, 22.11.0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := formatListVersions(tc.in)
			if got != tc.want {
				t.Fatalf("formatListVersions(%v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// --- lower / findManagerByName --------------------------------------------

func TestLower(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"abc", "abc"},
		{"ABC", "abc"},
		{"FnM", "fnm"},
		{"Nvm-Windows", "nvm-windows"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			if got := lower(tc.in); got != tc.want {
				t.Fatalf("lower(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestFindManagerByName_Exact(t *testing.T) {
	reg := detector.Registry{Found: []detector.Manager{
		&listTestStubManager{name: "fnm"},
		&listTestStubManager{name: "volta"},
	}}
	got, ok := findManagerByName(reg, "fnm")
	if !ok {
		t.Fatalf("expected fnm match, got ok=false")
	}
	if got.Name() != "fnm" {
		t.Fatalf("got %q, want fnm", got.Name())
	}
}

func TestFindManagerByName_CaseInsensitive(t *testing.T) {
	reg := detector.Registry{Found: []detector.Manager{
		&listTestStubManager{name: "nvm-windows"},
	}}
	for _, in := range []string{"nvm-windows", "NVM-Windows", "Nvm-Windows"} {
		got, ok := findManagerByName(reg, in)
		if !ok {
			t.Fatalf("expected match for %q, got ok=false", in)
		}
		if got.Name() != "nvm-windows" {
			t.Fatalf("got %q, want nvm-windows", got.Name())
		}
	}
}

func TestFindManagerByName_NotFound(t *testing.T) {
	reg := detector.Registry{Found: []detector.Manager{
		&listTestStubManager{name: "fnm"},
	}}
	got, ok := findManagerByName(reg, "volta")
	if ok {
		t.Fatalf("expected ok=false, got match on %q", got.Name())
	}
	if got != nil {
		t.Fatalf("expected nil Manager, got %T", got)
	}
}

// --- registryNames -------------------------------------------------------

func TestRegistryNames_Empty(t *testing.T) {
	if got := registryNames(detector.Registry{}); got != "" {
		t.Fatalf("empty registry should yield empty string, got %q", got)
	}
}

func TestRegistryNames_OrderingPreserved(t *testing.T) {
	reg := detector.Registry{Found: []detector.Manager{
		&listTestStubManager{name: "fnm"},
		&listTestStubManager{name: "volta"},
		&listTestStubManager{name: "asdf"},
	}}
	if got, want := registryNames(reg), "fnm, volta, asdf"; got != want {
		t.Fatalf("registryNames = %q, want %q", got, want)
	}
}

// --- outputListJSON ------------------------------------------------------

// TestOutputListJSON_HappyPath exercises outputListJSON's envelope
// shape directly. outputListJSON calls cmd.Println, which writes to
// the cobra Out stream — newBufCmd wires that to a buffer.
func TestOutputListJSON_HappyPath(t *testing.T) {
	cmd, buf := newBufCmd(t)
	cur := "20.18.0"
	if err := outputListJSON(cmd, listOutputJSON{
		Installed: []installedEntryJSON{
			{Manager: "fnm", Versions: []string{"18.20.4", "20.18.0", "22.11.0"}},
			{Manager: "volta", Versions: []string{"20.11.0"}},
		},
		Current: &cur,
	}); err != nil {
		t.Fatalf("outputListJSON: %v", err)
	}

	var got listOutputJSON
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output not valid JSON: %v\n%s", err, buf.String())
	}
	if len(got.Installed) != 2 {
		t.Fatalf("expected 2 installed entries, got %d", len(got.Installed))
	}
	if got.Installed[0].Manager != "fnm" {
		t.Fatalf("first entry manager = %q, want fnm", got.Installed[0].Manager)
	}
	if got.Installed[0].Versions[0] != "18.20.4" || got.Installed[0].Versions[2] != "22.11.0" {
		t.Fatalf("fnm versions malformed: %v", got.Installed[0].Versions)
	}
	if got.Current == nil || *got.Current != "20.18.0" {
		t.Fatalf("current = %v, want 20.18.0", got.Current)
	}
}

func TestOutputListJSON_OmitEmptyCurrent(t *testing.T) {
	cmd, buf := newBufCmd(t)
	if err := outputListJSON(cmd, listOutputJSON{
		Installed: []installedEntryJSON{{Manager: "fnm"}},
	}); err != nil {
		t.Fatalf("outputListJSON: %v", err)
	}
	// Marshal and ensure no "current" key was emitted.
	if strings.Contains(buf.String(), `"current"`) {
		t.Fatalf("expected current to be omitted, got: %s", buf.String())
	}
}

func TestOutputListJSON_OmitEmptyError(t *testing.T) {
	cmd, buf := newBufCmd(t)
	if err := outputListJSON(cmd, listOutputJSON{
		Installed: []installedEntryJSON{{Manager: "fnm", Versions: []string{"20.0.0"}}},
	}); err != nil {
		t.Fatalf("outputListJSON: %v", err)
	}
	if strings.Contains(buf.String(), `"error"`) {
		t.Fatalf("expected error to be omitted, got: %s", buf.String())
	}
}

// --- outputListTable -----------------------------------------------------

func TestOutputListTable_Empty(t *testing.T) {
	cmd, buf := newBufCmd(t)
	if err := outputListTable(cmd, nil); err != nil {
		t.Fatalf("outputListTable: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "No Node.js version manager detected.") {
		t.Fatalf("missing empty-state message in:\n%s", out)
	}
	if !strings.Contains(out, "fnm") {
		// We want the install-one-of list rendered so users have a path forward.
		t.Fatalf("expected manager list in empty-state output, got:\n%s", out)
	}
}

func TestOutputListTable_MixedStates(t *testing.T) {
	cmd, buf := newBufCmd(t)
	entries := []installedEntryJSON{
		{Manager: "fnm", Versions: []string{"20.18.0", "22.11.0"}},
		{Manager: "volta"}, // neither versions nor error — falls through with "(no versions installed)"
		{Manager: "asdf", Error: "boom"},
	}
	if err := outputListTable(cmd, entries); err != nil {
		t.Fatalf("outputListTable: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "fnm: 20.18.0, 22.11.0") {
		t.Fatalf("expected fnm line in:\n%s", out)
	}
	if !strings.Contains(out, "volta: (no versions installed)") {
		t.Fatalf("expected empty-volta line in:\n%s", out)
	}
	if !strings.Contains(out, "asdf: [error listing versions: boom]") {
		t.Fatalf("expected error-asdf line in:\n%s", out)
	}
}
