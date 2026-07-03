package platform

import (
	"errors"
	"path/filepath"
	"runtime"
	"testing"
)

// redirectHomeToTmp points platform.DataDir() at a per-test tempdir by
// setting HOME (and XDG_DATA_HOME / APPDATA — whichever is checked first
// on this OS) to the tempdir. This lets AcquireLock/LockPath work in a
// hermetic per-test directory and never touch the developer's real
// ~/.nodeup.
//
// We also clear HOME's parent cousins on each platform so they don't
// override HOME on Linux (XDG_DATA_HOME) or Windows (APPDATA).
func redirectHomeToTmp(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("APPDATA", "")
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", tmp)
		t.Setenv("HOME", tmp) // belt + braces on Windows shells
	} else {
		t.Setenv("HOME", tmp)
	}
	return tmp
}

// TestAcquireLock_RoundTrip verifies the basic happy path: acquire,
// then release, then re-acquire. Without release in between, the second
// AcquireLock would return ErrAlreadyLocked (covered by
// TestAcquireLock_TwiceReturnsAlreadyLocked).
func TestAcquireLock_RoundTrip(t *testing.T) {
	redirectHomeToTmp(t)
	l1, err := AcquireLock()
	if err != nil {
		t.Fatalf("AcquireLock (1st): %v", err)
	}
	if l1 == nil {
		t.Fatal("AcquireLock returned nil LockFile")
	}
	if err := l1.Release(); err != nil {
		t.Errorf("Release: %v", err)
	}

	// A second acquire after a clean release must succeed.
	l2, err := AcquireLock()
	if err != nil {
		t.Fatalf("AcquireLock (2nd, after Release): %v", err)
	}
	if err := l2.Release(); err != nil {
		t.Errorf("Release (2nd): %v", err)
	}
}

// TestAcquireLock_TwiceReturnsAlreadyLocked is the regression test for
// #44: the lock subsystem exists and is correct, but it had no call
// sites. While the CLI wire-up is now in place (internal/cli/{upgrade,
// config}.go), this unit test pins down the contract that the wire-up
// relies on — a held lock must surface ErrAlreadyLocked, not a generic
// I/O error.
func TestAcquireLock_TwiceReturnsAlreadyLocked(t *testing.T) {
	redirectHomeToTmp(t)
	first, err := AcquireLock()
	if err != nil {
		t.Fatalf("AcquireLock (1st): %v", err)
	}
	t.Cleanup(func() { _ = first.Release() })

	_, err2 := AcquireLock()
	if err2 == nil {
		t.Fatal("second AcquireLock succeeded while first holds the lock — flock is broken")
	}
	if !errors.Is(err2, ErrAlreadyLocked) {
		t.Errorf("second AcquireLock error %q is not ErrAlreadyLocked — CLI callers cannot branch on it", err2)
	}
}

// TestLockPath_UnderDataDir verifies that LockPath() returns a path
// inside the same DataDir the rest of nodeup's files live under. On
// each platform DataDir's terminal path component is "nodeup" (Linux,
// Windows) or a prefix that ends in "/nodeup" (macOS, where
// ~/Library/Application Support/nodeup is the layout). We assert
// the lock file lives under that directory by cross-checking via
// DataDir() — the lock MUST be on the same DataDir as the snapshots/
// reports/ cache/ subdirs, otherwise filesystem-permission enforcement
// of the "concurrent runs blocked" claim would not survive a refactor.
func TestLockPath_UnderDataDir(t *testing.T) {
	redirectHomeToTmp(t)

	dataDir, err := DataDir()
	if err != nil {
		t.Fatalf("DataDir: %v", err)
	}
	lock, err := LockPath()
	if err != nil {
		t.Fatalf("LockPath: %v", err)
	}
	if filepath.Dir(lock) != dataDir {
		t.Errorf("LockPath() directory %q is not the resolved DataDir %q — lock would not be enforceable next to snapshots/cache/reports",
			filepath.Dir(lock), dataDir)
	}
	if filepath.Base(lock) != "nodeup.lock" {
		t.Errorf("LockPath() file name = %q, want %q", filepath.Base(lock), "nodeup.lock")
	}
}
