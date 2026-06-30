// Package sentinel implements the upgrade-in-progress sentinel file.
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
