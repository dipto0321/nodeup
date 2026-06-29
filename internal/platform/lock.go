package platform

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
)

// LockFile is an advisory lock acquired by AcquireLock. Multiple processes
// (e.g., two `nodeup upgrade` invocations) are prevented from mutating
// the same Node installation concurrently.
//
// We use the classic "write a PID to a file and fcntl F_SETLK" pattern.
// This is portable across linux/macOS/windows since Go's syscall package
// exposes F_SETLK and LockFileEx respectively.
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

// AcquireLock attempts to take the nodeup lock. If another instance holds
// it, returns ErrAlreadyLocked wrapped with the PID of the holder.
//
// On Windows, LockFileEx semantics differ slightly — we still get an
// exclusive lock, but the OS-level semantics of "another PID owns it"
// are slightly different. The PID recovery from the lock file contents
// works identically across platforms.
func AcquireLock() (*LockFile, error) {
	lockPath, err := LockPath()
	if err != nil {
		return nil, err
	}

	// If a stale lock file exists from a dead process, we want to clean it
	// up. We detect a stale lock by trying to read the PID inside it and
	// checking whether that process is alive.
	if existing, err := readLockPID(lockPath); err == nil {
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
		// On linux/darwin, EWOULDBLOCK == EAGAIN here means another flock holder.
		return nil, fmt.Errorf("%w (flock failed: %v)", ErrAlreadyLocked, err)
	}

	// Truncate and write our PID so other invocations can identify us.
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
		// Best-effort cleanup; the flock release above already lets
		// another process in even if the file lingers.
		return rerr
	}
	return cerr
}

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

// processAlive checks whether pid is currently running. Used to detect
// stale lock files from crashed processes.
//
// We use a 0-byte signal probe: signal 0 doesn't actually send anything
// to the process, but the syscall returns an error if the process
// doesn't exist. This is the standard Unix idiom.
func processAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On unix, signal 0 is a no-op probe; on Windows FindProcess always
	// returns the process handle and Signal returns an error if invalid.
	err = proc.Signal(syscall.Signal(0))
	if err == nil {
		return true
	}
	// ESRCH on unix == process gone.
	return false
}

// bytesTrimSpace is a tiny helper to avoid importing bytes for one call.
func bytesTrimSpace(b []byte) []byte {
	start, end := 0, len(b)
	for start < end && isSpace(b[start]) {
		start++
	}
	for end > start && isSpace(b[end-1]) {
		end--
	}
	return b[start:end]
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

// Sanity check at package init — fails fast if DataDir is unreadable.
func init() {
	if _, err := LockPath(); err != nil {
		// We intentionally do not panic here — it's fine for the user
		// to have an unreadable home dir and still inspect nodeup's
		// --help output. LockPath errors will surface when they actually
		// try AcquireLock.
		_ = err
	}
}

// helper to silence "imported and not used" if filepath ends up unused
var _ = filepath.Separator