package detector

import (
	"testing"
)

// TestAllReturnsManagers verifies that All() returns a non-empty list of
// managers in a stable order, and that fnm comes first (it's the
// project owner's primary tool and the priority order is a public
// contract — call-sites sort by Priority()).
func TestAllReturnsManagers(t *testing.T) {
	all := All()
	if len(all) == 0 {
		t.Fatal("All() returned no managers")
	}

	if all[0].Name() != "fnm" {
		t.Errorf("expected first manager to be fnm, got %q", all[0].Name())
	}

	// Names must be unique.
	seen := map[string]bool{}
	for _, m := range all {
		if seen[m.Name()] {
			t.Errorf("duplicate manager name: %q", m.Name())
		}
		seen[m.Name()] = true
	}
}

// TestByNameRoundTrip verifies that ByName returns the same singleton
// description as All() for every manager in the registry.
func TestByNameRoundTrip(t *testing.T) {
	for _, m := range All() {
		got, ok := ByName(m.Name())
		if !ok {
			t.Errorf("ByName(%q) returned not-found", m.Name())
			continue
		}
		if got.Name() != m.Name() {
			t.Errorf("ByName round-trip mismatch: got %q want %q", got.Name(), m.Name())
		}
	}
}

// TestByNameUnknownReturnsFalse ensures we don't accidentally return a
// manager for a typo like "nvmm".
func TestByNameUnknownReturnsFalse(t *testing.T) {
	_, ok := ByName("definitely-not-a-real-manager")
	if ok {
		t.Error("ByName returned ok=true for unknown manager")
	}
}

// TestPriorityIsTotalOrder verifies the priority function is a total
// order (no two distinct names return the same priority).
func TestPriorityIsTotalOrder(t *testing.T) {
	seen := map[int]string{}
	for _, m := range All() {
		p := Priority(m.Name())
		if other, dup := seen[p]; dup && other != m.Name() {
			t.Errorf("priority collision: %q and %q both = %d", other, m.Name(), p)
		}
		seen[p] = m.Name()
	}
}

// TestErrNoManagerIsExported verifies the sentinel is exported so callers
// can use errors.Is(err, detector.ErrNoManager).
func TestErrNoManagerIsExported(t *testing.T) {
	if ErrNoManager == nil {
		t.Error("ErrNoManager should not be nil")
	}
	if ErrNoManager.Error() == "" {
		t.Error("ErrNoManager should have a non-empty message")
	}
}
