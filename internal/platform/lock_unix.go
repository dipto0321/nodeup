//go:build !windows

package platform

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"syscall"
)

// LockFile is an advisory lock acquired by AcquireLock. Multiple processes
// (e.g., two `nodeup upgrade` invocations) are prevented from mutating
// the same Node installation concurrently.
//
// On linux/darwin we use the BSD flock(2) syscall: each process takes an
// exclusive, non-blocking lock on the lock file; subsequent attempts return
// ErrAlreadyLocked immediately.
//
// Lifecycle:
//
//	lock, err := platform.AcquireLock()
//	if err != nil { ... }
//	defer lock.Release()
type LockFile struct {
	path string
	f    *os.File
}

// ErrAlreadyLocked is returned when another nodeup process holds the lock.
var ErrAlreadyLocked = errors.New("nodeup is already running")

// AcquireLock takes the nodeup lock at LockPath(). If another instance
// already holds it, returns ErrAlreadyLocked wrapped with the PID of the
// holder (read from the lock file contents). Stale lock files from dead
// processes are removed first.
func AcquireLock() (*LockFile, error) {
	lockPath, err := LockPath()
	if err != nil {
		return nil, err
	}

	if existing, perr := readLockPID(lockPath); perr == nil {
		if !processAlive(existing) {
			_ = os.Remove(lockPath)
		} else {
			return nil, fmt.Errorf("%w: PID %d", ErrAlreadyLocked, existing)
		}
	}

	f, err := os.OpenFile(lockPath, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create lock file: %w", err)
	}

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("%w (flock failed: %v)", ErrAlreadyLocked, err)
	}

	if err := f.Truncate(0); err != nil {
		_ = f.Close()
		return nil, err
	}
	if _, err := f.WriteString(strconv.Itoa(os.Getpid())); err != nil {
		_ = f.Close()
		return nil, err
	}

	return &LockFile{path: lockPath, f: f}, nil
}

// Release unlocks and removes the lock file. Safe to call multiple times.
func (l *LockFile) Release() error {
	if l == nil || l.f == nil {
		return nil
	}
	_ = syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	cerr := l.f.Close()
	l.f = nil
	if rerr := os.Remove(l.path); rerr != nil && !errors.Is(rerr, os.ErrNotExist) {
		return rerr
	}
	return cerr
}

// readLockPID reads the PID stored inside an existing lock file. Used to
// detect and recover from locks left by crashed nodeup processes.
func readLockPID(path string) (int, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(string(bytesTrimSpace(b)))
	if err != nil {
		return 0, err
	}
	return pid, nil
}

// processAlive sends signal 0 to pid and returns whether the process
// accepted it. Signal 0 is the canonical "is this PID alive?" probe on
// POSIX systems — it doesn't deliver anything but errors on ESRCH.
func processAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	if err := proc.Signal(syscall.Signal(0)); err == nil {
		return true
	}
	return false
}
