package detector

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Masterminds/semver/v3"

	"github.com/dipto0321/nodeup/internal/ui"
)

// stubMgr is a tiny Manager implementation for tests that
// exercise the registry / picker. Methods return zero values
// except Name and Detect, which the picker relies on.
type stubMgr struct {
	name string
	pri  int
}

func (s *stubMgr) Name() string             { return s.name }
func (s *stubMgr) Detect() bool             { return true }
func (s *stubMgr) Version() (string, error) { return "0.0.0", nil }
func (s *stubMgr) ListInstalled(context.Context) ([]semver.Version, error) {
	return nil, nil
}
func (s *stubMgr) Install(semver.Version) error                    { return nil }
func (s *stubMgr) Uninstall(semver.Version) error                  { return nil }
func (s *stubMgr) Use(semver.Version) error                        { return nil }
func (s *stubMgr) SetDefault(semver.Version) error                 { return nil }
func (s *stubMgr) GlobalNpmPrefix(semver.Version) (string, error)  { return "", nil }
func (s *stubMgr) Current(context.Context) (semver.Version, error) { return semver.Version{}, nil }

// stubPrompt implements ui.Prompt with hand-coded answers. The
// "answers" channel is fed by the test before each call.
type stubPrompt struct {
	answers []string
	calls   int
}

func (s *stubPrompt) Mode() ui.Mode { return ui.PlainMode }
func (s *stubPrompt) Confirm(string, bool) (bool, error) {
	return false, errors.New("Confirm not expected in ResolveInteractive tests")
}
func (s *stubPrompt) Select(_ string, options []string, _ string) (string, error) {
	if s.calls >= len(s.answers) {
		return "", fmt.Errorf("stubPrompt: out of answers (call %d)", s.calls)
	}
	got := s.answers[s.calls]
	s.calls++
	for _, o := range options {
		if o == got {
			return got, nil
		}
	}
	return "", fmt.Errorf("stubPrompt: answer %q not in options %v", got, options)
}

// --- ResolveInteractive -------------------------------------------------

func TestResolveInteractive_NoManagers(t *testing.T) {
	got, err := ResolveInteractive(Registry{}, nil, false, nil, nil)
	if !errors.Is(err, ErrNoManager) {
		t.Errorf("err = %v, want ErrNoManager", err)
	}
	if got != nil {
		t.Errorf("got %v, want nil", got)
	}
}

func TestResolveInteractive_SingleManagerSkipsPrompt(t *testing.T) {
	p := &stubPrompt{answers: []string{"unused"}}
	reg := Registry{Found: []Manager{&stubMgr{name: "fnm", pri: 0}}}
	got, err := ResolveInteractive(reg, p, false, nil, nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got == nil || got.Name() != "fnm" {
		t.Errorf("got %v, want fnm", got)
	}
	if p.calls != 0 {
		t.Errorf("single-manager path called prompt %d times, want 0", p.calls)
	}
}

func TestResolveInteractive_NonInteractiveErrors(t *testing.T) {
	reg := Registry{Found: []Manager{
		&stubMgr{name: "fnm", pri: 0},
		&stubMgr{name: "nvm", pri: 1},
	}}
	got, err := ResolveInteractive(reg, nil, true, nil, nil)
	if !errors.Is(err, ErrInteractiveRequired) {
		t.Errorf("err = %v, want ErrInteractiveRequired", err)
	}
	if got != nil {
		t.Errorf("got %v, want nil", got)
	}
}

func TestResolveInteractive_PicksFromPrompt(t *testing.T) {
	reg := Registry{Found: []Manager{
		&stubMgr{name: "fnm", pri: 0},
		&stubMgr{name: "nvm", pri: 1},
		&stubMgr{name: "volta", pri: 2},
	}}
	// User picks "nvm" (second option).
	p := &stubPrompt{answers: []string{"nvm"}}
	got, err := ResolveInteractive(reg, p, false, nil, nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got == nil || got.Name() != "nvm" {
		t.Errorf("got %v, want nvm", got)
	}
}

// TestResolveInteractive_FallbackPlainPrompt pins the "caller
// passed nil for p" path: we build a fresh plain ui.Prompt
// against the supplied in/out streams and use that. This is what
// upgrade.go / check.go actually do — they don't carry a
// long-lived ui.Prompt around, they pass the cmd streams
// directly. The fallback exists for tests that want to drive
// the picker without constructing a stub.
func TestResolveInteractive_FallbackPlainPrompt(t *testing.T) {
	reg := Registry{Found: []Manager{
		&stubMgr{name: "fnm", pri: 0},
		&stubMgr{name: "nvm", pri: 1},
	}}
	// User types "2" for nvm.
	var out bytes.Buffer
	got, err := ResolveInteractive(reg, nil, false, strings.NewReader("2\n"), &out)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got == nil || got.Name() != "nvm" {
		t.Errorf("got %v, want nvm (out: %s)", got, out.String())
	}
}

// TestResolveInteractive_FallbackEOF pins the EOF behavior of
// the fallback path: closed stdin falls back to options[0]
// (per ui.PlainPrompt.Select contract) — so ResolveInteractive
// returns the first manager rather than erroring out. This is
// the same "piped script ran out of input" path Confirm covers.
func TestResolveInteractive_FallbackEOF(t *testing.T) {
	reg := Registry{Found: []Manager{
		&stubMgr{name: "fnm", pri: 0},
		&stubMgr{name: "nvm", pri: 1},
	}}
	got, err := ResolveInteractive(reg, nil, false, strings.NewReader(""), &bytes.Buffer{})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got == nil || got.Name() != "fnm" {
		t.Errorf("got %v, want fnm (EOF → first)", got)
	}
}

// --- ResolveManagerAuto -------------------------------------------------

func TestResolveManagerAuto_PreferredWins(t *testing.T) {
	reg := Registry{Found: []Manager{
		&stubMgr{name: "fnm", pri: 0},
		&stubMgr{name: "nvm", pri: 1},
	}}
	got, err := ResolveManagerAuto(reg, "nvm")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got.Name() != "nvm" {
		t.Errorf("got %v, want nvm", got)
	}
}

func TestResolveManagerAuto_MultiWithoutPreferenceReturnsErrInteractiveRequired(t *testing.T) {
	reg := Registry{Found: []Manager{
		&stubMgr{name: "fnm", pri: 0},
		&stubMgr{name: "nvm", pri: 1},
	}}
	got, err := ResolveManagerAuto(reg, "")
	if !errors.Is(err, ErrInteractiveRequired) {
		t.Errorf("err = %v, want ErrInteractiveRequired", err)
	}
	if got != nil {
		t.Errorf("got %v, want nil", got)
	}
}

func TestResolveManagerAuto_NoManagersReturnsErrNoManager(t *testing.T) {
	got, err := ResolveManagerAuto(Registry{}, "")
	if !errors.Is(err, ErrNoManager) {
		t.Errorf("err = %v, want ErrNoManager", err)
	}
	if got != nil {
		t.Errorf("got %v, want nil", got)
	}
}

func TestResolveManagerAuto_SingleManagerNoPrompt(t *testing.T) {
	reg := Registry{Found: []Manager{&stubMgr{name: "fnm", pri: 0}}}
	got, err := ResolveManagerAuto(reg, "")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got.Name() != "fnm" {
		t.Errorf("got %v, want fnm", got)
	}
}
