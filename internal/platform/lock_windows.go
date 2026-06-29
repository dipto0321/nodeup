//go:build windows

package platform

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"syscall"
	"unsafe"
)

// LockFile is an advisory lock acquired by AcquireLock. Multiple processes
// (e.g., two `nodeup upgrade` invocations) are prevented from mutating
// the same Node installation concurrently.
//
// On Windows we use LockFileEx from kernel32. There is no portable
// "is another process holding this?" probe like flock(LOCK_NB); a
// ERROR_LOCK_VIOLATION return is our EWOULDBLOCK equivalent.
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

// LockFileEx flags. Mirrors win32 LOCKFILE_* constants.
const (
	lockFlagExclusive       = 0x00000002
	lockFlagFailImmediately = 0x00000001
)

// Windows system error codes we map. ERROR_LOCK_VIOLATION = 33,
// ERROR_IO_PENDING = 997 (we use neither).
const errnoERROR_LOCK_VIOLATION syscall.Errno = 33

// AcquireLock takes the nodeup lock at LockPath(). If another instance
// already holds it, returns ErrAlreadyLocked wrapped with the PID of the
// holder. Stale lock files from dead processes are removed first.
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

	overlapped := &syscall.Overlapped{}
	if err := lockFileEx(syscall.Handle(f.Fd()), lockFlagExclusive|lockFlagFailImmediately, 0, 1, 0, overlapped); err != nil {
		_ = f.Close()
		if errors.Is(err, errnoERROR_LOCK_VIOLATION) {
			return nil, ErrAlreadyLocked
		}
		return nil, fmt.Errorf("%w (LockFileEx failed: %v)", ErrAlreadyLocked, err)
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
	overlapped := &syscall.Overlapped{}
	_ = unlockFileEx(syscall.Handle(l.f.Fd()), 1, 0, overlapped)
	cerr := l.f.Close()
	l.f = nil
	if rerr := os.Remove(l.path); rerr != nil && !errors.Is(rerr, os.ErrNotExist) {
		return rerr
	}
	return cerr
}

// readLockPID reads the PID stored inside an existing lock file. Identical
// to the unix implementation; lives here so the file compiles on its own.
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

// processAlive checks whether pid is currently running. On Windows the
// canonical probe is os.FindProcess + Signal, which Go translates to
// OpenProcess(SYNCHRONIZE) + WaitForSingleObject(0).
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

// lockFileEx is a thin Go binding for Win32 LockFileEx.
//
// BOOL LockFileEx(
//   HANDLE        hFile,             // a1
//   DWORD         dwFlags,           // a2
//   DWORD         dwReserved,        // a3
//   DWORD         nNumberOfBytesToLockLow,   // a4
//   DWORD         nNumberOfBytesToLockHigh,  // a5
//   LPOVERLAPPED  lpOverlapped       // a6
// );
//
// We use Syscall6 with arg count 6; the last slot is the overlapped pointer.
func lockFileEx(h syscall.Handle, flags, reserved, nLow, nHigh uint32, overlapped *syscall.Overlapped) error {
	_, _, e1 := syscall.Syscall6(
		procLockFileEx.Addr(),
		6,
		uintptr(h),
		uintptr(flags),
		uintptr(reserved),
		uintptr(nLow),
		uintptr(nHigh),
		uintptr(unsafe.Pointer(overlapped)),
	)
	if e1 != 0 {
		return error(e1)
	}
	return nil
}

// unlockFileEx mirrors UnlockFileEx. The function has 5 real parameters
// (hFile, dwReserved=0, nLow, nHigh, lpOverlapped). We pad the Syscall6
// payload to 6 by appending a trailing zero — the same pattern used by
// syscall.SetFilePointerEx in the standard library.
//
// BOOL UnlockFileEx(
//   HANDLE        hFile,             // a1
//   DWORD         dwReserved,        // a2  (== 0)
//   DWORD         nNumberOfBytesToUnlockLow,   // a3
//   DWORD         nNumberOfBytesToUnlockHigh,  // a4
//   LPOVERLAPPED  lpOverlapped       // a5
// );
func unlockFileEx(h syscall.Handle, nLow, nHigh uint32, overlapped *syscall.Overlapped) error {
	_, _, e1 := syscall.Syscall6(
		procUnlockFileEx.Addr(),
		6,
		uintptr(h),
		0,
		uintptr(nLow),
		uintptr(nHigh),
		uintptr(unsafe.Pointer(overlapped)),
		0, // padding slot — required by Syscall6's 6-arg layout
	)
	if e1 != 0 {
		return error(e1)
	}
	return nil
}

var (
	modkernel32     = syscall.NewLazyDLL("kernel32.dll")
	procLockFileEx  = modkernel32.NewProc("LockFileEx")
	procUnlockFileEx = modkernel32.NewProc("UnlockFileEx")
)
