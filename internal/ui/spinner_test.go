package ui

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// TestNewSpinner_PlainModeHonored pins the decision in NewSpinner:
// only FancyMode gets the bubbletea-backed spinner; PlainMode gets
// the one-shot "label ..." line so log aggregators see one record
// per operation instead of a stream of cursor-positioned updates.
func TestNewSpinner_PlainModeHonored(t *testing.T) {
	var out bytes.Buffer
	s := NewSpinner(PlainMode, "Fetching manifest", &out)
	if s.Mode() != PlainMode {
		t.Fatalf("PlainMode → spinner.Mode() = %v, want PlainMode", s.Mode())
	}

	stop := s.Start()
	defer stop()

	got := out.String()
	if !strings.Contains(got, "Fetching manifest") {
		t.Errorf("plain spinner didn't print its label, got %q", got)
	}
	if !strings.Contains(got, "...") {
		t.Errorf("plain spinner missing the trailing '...' marker, got %q", got)
	}

	// Stop() in PlainMode must be a no-op (no second line). The
	// caller emits the success/failure line via Writer.Success /
	// Writer.Error — not via Stop. This keeps the log record count
	// predictable (one record per operation).
	before := out.Len()
	stop()
	stop() // idempotent
	if out.Len() != before {
		t.Errorf("plain spinner Stop() wrote extra bytes: before=%d after=%d", before, out.Len())
	}
}

// TestNewSpinner_FancyModeSelected pins that FancyMode + a non-nil
// writer hands back a FancyMode spinner. The actual bubbletea program
// is hard to assert against without a TTY (under `go test` stdin/out
// are pipes and bubbletea's renderer needs a real terminal to
// produce frames), so we just verify the decision-point behavior
// here. The end-to-end happy path is covered by manual smoke tests
// documented in PR2's body.
func TestNewSpinner_FancyModeSelected(t *testing.T) {
	var out bytes.Buffer
	s := NewSpinner(FancyMode, "Installing v22", &out)
	if s.Mode() != FancyMode {
		t.Fatalf("FancyMode + non-nil writer → spinner.Mode() = %v, want FancyMode", s.Mode())
	}

	// Stop() on a fancy spinner must be safe to call even when bubbletea
	// can't initialize a real program (which is the case under `go test`).
	// We don't assert on the bytes bubbletea produced — that's
	// environment-dependent — only that the lifecycle doesn't panic
	// or deadlock. 2s is generous; the in-process Quit+Settle dance
	// normally completes in well under 250ms.
	done := make(chan struct{})
	go func() {
		defer close(done)
		stop := s.Start()
		stop()
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Errorf("fancy spinner Start().Stop() did not return within 2s")
	}
}

// TestNewSpinner_FancyNilWriterDegradesToPlain covers the
// defensive branch: a caller passes FancyMode but with nil out
// (e.g. unit tests that don't care about output). We MUST NOT
// pass nil into bubbletea — that would NPE inside the program.
// Instead we hand back a no-op. The Mode() still reports FancyMode
// (caller's intent) so behavior is consistent at the decision point.
func TestNewSpinner_FancyNilWriterDegradesToPlain(t *testing.T) {
	s := NewSpinner(FancyMode, "noop", nil)
	if s == nil {
		t.Fatalf("NewSpinner returned nil for (FancyMode, nil)")
	}
	stop := s.Start()
	if stop == nil {
		t.Fatalf("Start() returned nil stop func")
	}
	// Stop() must be safe even though bubbletea never ran.
	stop()
}

// TestWaitWithSpinner_NilSpinnerPassesThrough pins that a nil
// spinner is treated as "no spinner" — useful for tests that don't
// want to set up a Spinner at all but still want to use the same
// control flow as production code.
func TestWaitWithSpinner_NilSpinnerPassesThrough(t *testing.T) {
	called := false
	work := func() error {
		called = true
		return nil
	}
	if err := WaitWithSpinner(context.Background(), nil, work); err != nil {
		t.Errorf("nil spinner → WaitWithSpinner err = %v, want nil", err)
	}
	if !called {
		t.Errorf("work function was not invoked")
	}
}

// TestWaitWithSpinner_HappyPath pins the basic plumbing: when work
// returns nil, WaitWithSpinner returns nil. We use the plain
// spinner so this test exercises the actual production path on
// non-interactive stdio (which is what `go test` sees).
func TestWaitWithSpinner_HappyPath(t *testing.T) {
	var out bytes.Buffer
	s := NewSpinner(PlainMode, "happy", &out)

	err := WaitWithSpinner(context.Background(), s, func() error {
		return nil
	})
	if err != nil {
		t.Errorf("happy path: WaitWithSpinner err = %v, want nil", err)
	}
	if !strings.Contains(out.String(), "happy") {
		t.Errorf("happy path: spinner label not printed, got %q", out.String())
	}
}

// TestWaitWithSpinner_WorkErrorReturned pins that errors from
// `work` are surfaced to the caller (not swallowed by the spinner).
// The spinner itself does NOT print the error — the caller is
// responsible for rendering it via Writer.Error.
func TestWaitWithSpinner_WorkErrorReturned(t *testing.T) {
	var out bytes.Buffer
	s := NewSpinner(PlainMode, "failing", &out)

	wantErr := errors.New("boom")
	got := WaitWithSpinner(context.Background(), s, func() error {
		return wantErr
	})
	if !errors.Is(got, wantErr) {
		t.Errorf("WaitWithSpinner err = %v, want %v", got, wantErr)
	}
}

// TestWaitWithSpinner_ContextCancelReturns pins the cancel contract:
// when ctx is canceled while work is running, WaitWithSpinner
// returns ctx.Err() WITHOUT preempting the work goroutine (callers
// who need preemptible work must thread ctx into work themselves).
//
// Run the helper in a goroutine and observe its return value via a
// channel. The work function blocks on its own channel so we can
// prove work did NOT finish before the helper returned.
func TestWaitWithSpinner_ContextCancelReturns(t *testing.T) {
	var out bytes.Buffer
	s := NewSpinner(PlainMode, "canceling", &out)

	ctx, cancel := context.WithCancel(context.Background())
	releaseWork := make(chan struct{}) // test → work: unblock parked work
	defer close(releaseWork)           // at test end, wake parked work

	resultCh := make(chan error, 1)
	go func() {
		resultCh <- WaitWithSpinner(ctx, s, func() error {
			// Block until the test releases us. If the helper had
			// preempted us, we'd never see this; instead the helper
			// just returns ctx.Err() while we sit parked here.
			<-releaseWork
			return nil
		})
	}()

	// Cancel from the test goroutine and assert the helper saw it.
	cancel()

	select {
	case got := <-resultCh:
		if !errors.Is(got, context.Canceled) {
			t.Errorf("WaitWithSpinner err = %v, want context.Canceled", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("WaitWithSpinner did not return within 2s after ctx cancel")
	}
	// The defer above closes releaseWork, which wakes the parked
	// work goroutine; it then returns nil on `done`, but the helper
	// already returned — so that send goes to an unread buffered
	// channel and the goroutine exits cleanly. No leak.
}
