// Sentinel file handling: tracks when `nodeup upgrade` is mid-migration.
//
// When `nodeup upgrade` starts a migration that involves snapshotting global
// npm packages, installing new Node versions, and restoring packages onto
// the new versions, an interrupted run (Ctrl-C, power loss, OOM kill) leaves
// the user with installed Node versions but no migrated packages. The
// sentinel is the "we were in the middle of an upgrade" marker that lets
// the next `nodeup` invocation detect the interruption and prompt for replay.
//
// File layout:
//
//	<DataDir>/upgrade-in-progress.json
//
// The file is small (a few hundred bytes) and only ever present while an
// upgrade is running or after one was interrupted. Successful upgrades
// always remove it via the deferred cleanup in internal/cli/upgrade.go.
package packages

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/dipto0321/nodeup/internal/platform"
)

// SentinelFile is the canonical filename for the in-progress marker.
// Exported so tests and CLI helpers can refer to it without a magic string.
const SentinelFile = "upgrade-in-progress.json"

// UpgradeSentinel is the on-disk schema for upgrade-in-progress.json.
//
// It records enough context for the next `nodeup` invocation to print a
// useful "looks like your last upgrade was interrupted, here's how to
// resume it" message and to drive `nodeup packages restore` without
// forcing the user to look anything up.
type UpgradeSentinel struct {
	// StartedAt is when the upgrade began. Rendered as RFC3339 in the
	// warning message so users can correlate it with their shell history.
	StartedAt time.Time `json:"started_at"`

	// Manager is the Node version manager that was driving the upgrade
	// (fnm, nvm, volta, ...). Needed because the restore CLI needs it.
	Manager string `json:"manager"`

	// OldVersion is the Node version packages were snapshotted from. May
	// be empty if the upgrade started on a clean install.
	OldVersion string `json:"old_version,omitempty"`

	// NewVersion is the version being installed (or already installed,
	// if we crashed mid-install). What `packages restore` targets.
	NewVersion string `json:"new_version,omitempty"`

	// SnapshotPath is the absolute path of the snapshot that `packages
	// restore --from` should replay against. We record it explicitly so
	// the user does not have to know the <manager>-<version>.json naming
	// convention — we just print the path in the warning message.
	SnapshotPath string `json:"snapshot_path,omitempty"`
}

// ErrNoSentinel is returned by LoadSentinel when no sentinel file exists.
// Distinct from a parse error so callers can treat "no upgrade was in
// progress" as a normal state, not an error condition.
var ErrNoSentinel = errors.New("no upgrade-in-progress sentinel")

// SentinelPath returns the absolute path of the sentinel file. The
// parent directory is created on demand (matching the pattern of the
// other DataDir helpers in internal/platform).
func SentinelPath() (string, error) {
	d, err := platform.DataDir()
	if err != nil {
		return "", err
	}
	// MkdirAll is a no-op if the directory already exists. We do not
	// pre-create the file — WriteSentinel handles that.
	if err := os.MkdirAll(d, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(d, SentinelFile), nil
}

// WriteSentinel atomically replaces any existing sentinel file with a
// fresh one describing the upgrade that is about to begin.
//
// We write to a tempfile in the same directory and os.Rename into place
// so a concurrent reader (e.g., a second nodeup invocation that lost the
// race to acquire the lock) never sees a half-written file. On POSIX,
// rename within a directory is atomic. On Windows, os.Rename replaces
// the destination if it exists — also fine for our use case.
func WriteSentinel(s UpgradeSentinel) error {
	path, err := SentinelPath()
	if err != nil {
		return err
	}

	// Default StartedAt to now if the caller did not set it, so the
	// schema is always populated with a timestamp.
	if s.StartedAt.IsZero() {
		s.StartedAt = time.Now()
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal sentinel: %w", err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write sentinel tempfile: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		// Best-effort cleanup; the tmp file in DataDir is harmless
		// but we don't want to leak it across many interrupted runs.
		_ = os.Remove(tmp)
		return fmt.Errorf("rename sentinel into place: %w", err)
	}
	return nil
}

// LoadSentinel reads and parses the sentinel file. Returns ErrNoSentinel
// (wrapped) when the file does not exist — callers should treat this as
// "no interrupted upgrade" and proceed silently.
func LoadSentinel() (*UpgradeSentinel, error) {
	path, err := SentinelPath()
	if err != nil {
		return nil, err
	}
	return loadSentinelFrom(path)
}

// loadSentinelFrom reads and parses a sentinel file at an explicit
// path.
//
// Split out from LoadSentinel so the claim-by-rename path in
// RemoveSentinelIfUnchanged can parse the claimed file without going
// back through SentinelPath (which always resolves to the canonical
// location).
func loadSentinelFrom(path string) (*UpgradeSentinel, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrNoSentinel, path)
		}
		return nil, fmt.Errorf("read sentinel: %w", err)
	}

	var s UpgradeSentinel
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse sentinel: %w", err)
	}
	return &s, nil
}

// RemoveSentinel deletes the sentinel file. Safe to call when the file
// does not exist — we treat ErrNotExist as success so the deferred
// cleanup in runUpgrade never panics on the happy path.
//
// We deliberately swallow other errors and return them rather than
// panicking: a stale sentinel left on disk is harmless (the next run
// will warn and offer to remove it), whereas a panic during deferred
// cleanup would mask the original error.
func RemoveSentinel() error {
	path, err := SentinelPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// claimSentinelFile is the package-level seam used by
// RemoveSentinelIfUnchanged to atomically claim the sentinel file.
//
// It renames `path` to `claimPath` — an atomic operation on POSIX and
// on Windows (os.Rename replaces the destination). Tests overwrite it
// to simulate a concurrent `nodeup upgrade` arming a fresh sentinel in
// the window between our read and our claim. Production code never
// reassigns it.
//
// This mirrors the `whichNode` seam in internal/detector/system_node.go
// — the project's established way of making a filesystem-timing
// dependency deterministically testable.
var claimSentinelFile = os.Rename

// RemoveSentinelIfUnchanged deletes the sentinel ONLY if it still
// describes the same upgrade the caller inspected.
//
// This is the compare-and-delete primitive the stale-sentinel cleanup
// needs. Without it there is a race: the warning fires in
// PersistentPreRunE on EVERY nodeup invocation, so between reading the
// sentinel and deleting it, a concurrent `nodeup upgrade` could arm a
// FRESH sentinel whose snapshot is alive. An unconditional delete would
// clobber that live breadcrumb, and the user would lose the resume
// hint for a migration that genuinely needs replaying.
//
// # WHY CLAIM-BY-RENAME, NOT COMPARE-THEN-DELETE
//
// The obvious implementation — read the sentinel, compare it, then
// os.Remove it — has a TOCTOU window: a concurrent writer can replace
// the file between the compare and the delete, and we would then delete
// the NEW file, not the one we judged. Re-reading immediately before the
// delete narrows the window but does not close it.
//
// Instead we CLAIM the file first: os.Rename(path, claimPath) atomically
// moves whatever is currently at `path` to a unique name. Whatever we
// claimed, we now own exclusively — a concurrent writer can no longer
// touch it. We then read the claimed file:
//
//   - It matches the sentinel we judged stale → delete the claim. Done.
//   - It does NOT match → a concurrent upgrade armed a fresh sentinel
//     after we read ours, and we accidentally claimed THAT one. Put it
//     back and report that we removed nothing.
//
// The compare now happens on a file we exclusively own, so there is no
// window between "decide" and "act".
func RemoveSentinelIfUnchanged(target *UpgradeSentinel) (removed bool, err error) {
	if target == nil {
		return false, nil
	}

	path, err := SentinelPath()
	if err != nil {
		return false, err
	}

	// Claim the file. The claim path is unique per process so two
	// concurrent cleanups can't collide on it.
	claimPath := fmt.Sprintf("%s.claim-%d-%d", path, os.Getpid(), time.Now().UnixNano())
	if err := claimSentinelFile(path, claimPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// Already gone — someone else cleaned it up. The desired
			// end state holds, so report success without an error.
			return false, nil
		}
		return false, fmt.Errorf("claim sentinel: %w", err)
	}

	// From here on the claimed file is ours. Every early return must
	// either delete it or put it back — never leave it stranded under
	// the claim name, or the sentinel would vanish from the user's
	// view entirely.
	claimed, lerr := loadSentinelFrom(claimPath)
	if lerr != nil {
		// We can't tell what we claimed. Put it back rather than
		// destroying an upgrade breadcrumb we don't understand.
		_ = os.Rename(claimPath, path)
		if errors.Is(lerr, ErrNoSentinel) {
			return false, nil
		}
		return false, fmt.Errorf("read claimed sentinel: %w", lerr)
	}

	if !sameSentinel(claimed, target) {
		// The file we claimed is NOT the one the caller inspected — a
		// concurrent upgrade armed a fresh sentinel in the meantime.
		// Put it back exactly where it came from and leave it alone.
		if rerr := os.Rename(claimPath, path); rerr != nil {
			return false, fmt.Errorf("restore concurrently-written sentinel: %w", rerr)
		}
		return false, nil
	}

	// It's the sentinel we judged stale. Delete the claim.
	if err := os.Remove(claimPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		// We own the file but couldn't delete it. Put it back so the
		// user still sees their marker and can retry.
		_ = os.Rename(claimPath, path)
		return false, fmt.Errorf("delete stale sentinel: %w", err)
	}
	return true, nil
}

// sameSentinel reports whether two sentinels describe the same upgrade.
//
// We compare the fields that identify the upgrade rather than the whole
// struct: StartedAt (when it began), Manager, and SnapshotPath (the
// payload it points at). NewVersion and OldVersion are included for
// completeness — a sentinel that differs in any of these is a
// different upgrade.
//
// StartedAt is compared with Equal rather than == because it round-trips
// through JSON, which preserves the instant but not the monotonic clock
// reading and location pointer.
func sameSentinel(a, b *UpgradeSentinel) bool {
	if a == nil || b == nil {
		return false
	}
	return a.StartedAt.Equal(b.StartedAt) &&
		a.Manager == b.Manager &&
		a.OldVersion == b.OldVersion &&
		a.NewVersion == b.NewVersion &&
		a.SnapshotPath == b.SnapshotPath
}

// OrphanedSentinel returns the parsed sentinel if one exists, or nil if
// not. This is the "did the last run get interrupted?" check that
// PersistentPreRunE uses on every `nodeup` invocation. Errors other than
// "file missing" are returned so the caller can decide whether to log
// them (e.g., a parse error means the file is corrupted — we should at
// least surface that rather than silently swallowing it).
func OrphanedSentinel() (*UpgradeSentinel, error) {
	s, err := LoadSentinel()
	if err != nil {
		if errors.Is(err, ErrNoSentinel) {
			return nil, nil
		}
		return nil, err
	}
	return s, nil
}

// SentinelSnapshotExists reports whether the snapshot a sentinel points
// at is still on disk.
//
// This is what makes the interrupted-upgrade warning self-validating.
//
// The sentinel's contract is "there is a migration you can replay;
// the payload for it lives at SnapshotPath". That contract only holds
// while the snapshot survives. Two things can break it:
//
//   - The upgrade flow's own auto-reclaim, which deletes the snapshot
//     files once the migration has replayed them (see #128).
//   - `nodeup packages cleanup`, which lets the user delete any
//     snapshot by hand.
//
// When either happens, the sentinel outlives its payload: the warning
// keeps firing on every invocation, it points at a file that no longer
// exists, and the recovery command it suggests is guaranteed to fail
// with "read snapshot ...: no such file or directory". The user is
// left with a warning they cannot act on and no way to dismiss it.
//
// A sentinel with an EMPTY SnapshotPath is treated as stale: there is
// nothing to replay, so there is nothing to warn about. (The sentinel
// is only armed when there is something to migrate — see the
// `len(toInstall) > 0` gate in internal/cli/upgrade.go — so an empty
// path means the file was written by something else, or hand-edited.)
//
// The check is a plain os.Stat. A snapshot that exists but is
// unparseable is still "present" as far as this function is concerned
// — the restore path will report the corruption, and a corruption is a
// different problem from a missing file.
func SentinelSnapshotExists(s *UpgradeSentinel) bool {
	if s == nil || s.SnapshotPath == "" {
		return false
	}
	info, err := os.Stat(s.SnapshotPath)
	if err != nil {
		return false
	}
	// A directory at the snapshot path is not a snapshot. Treat it as
	// missing so the warning doesn't point the user at something the
	// restore command cannot read.
	return !info.IsDir()
}
