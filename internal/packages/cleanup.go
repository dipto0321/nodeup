package packages

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/dipto0321/nodeup/internal/platform"
)

// SnapshotFile describes one snapshot file on disk.
//
// Unlike SnapshotData (the parsed JSON body), SnapshotFile describes the
// FILE — which is what a cleanup command needs to act on. The two are
// deliberately separate types: a snapshot file can be unparseable (a
// crash mid-write left a truncated JSON body) and we still need to be
// able to list and delete it. ListSnapshots() silently skips such files,
// so it cannot drive a deletion flow.
type SnapshotFile struct {
	// Path is the absolute path of the snapshot file.
	Path string

	// Name is the file's basename, e.g. "fnm-20.10.0.json".
	Name string

	// Manager is the manager name parsed out of the filename, e.g. "fnm".
	// Empty when the filename doesn't follow the <manager>-<version>.json
	// convention.
	Manager string

	// Version is the Node version parsed out of the filename, e.g. "20.10.0".
	// Empty when the filename doesn't follow the convention.
	Version string

	// Size is the file size in bytes.
	Size int64

	// ModTime is the file's last-modification time.
	ModTime time.Time

	// ParseError is non-empty when the file's JSON body could not be
	// parsed. The file is still listed (and can still be deleted) — we
	// just can't tell the user what's inside it.
	ParseError string
}

// ListSnapshotFiles returns every snapshot file present in the snapshots
// directory, including files whose JSON body is corrupt.
//
// This is the enumeration a cleanup command needs, and it is deliberately
// NOT built on top of ListSnapshots():
//
//   - ListSnapshots() parses each file and skips the ones that fail to
//     parse. A snapshot truncated by a crash mid-write is therefore
//     invisible to it — and a cleanup command that can't see a file can't
//     delete it. That's exactly the class of file we're trying to
//     reclaim.
//   - ListSnapshots() returns parsed SnapshotData, which loses the
//     file's size and modification time — the two things a user wants
//     to see before deciding what to delete.
//
// Results are sorted by modification time (oldest first) so the most
// stale files — the ones most likely to be dead weight — appear first.
//
// The directory is created if it doesn't exist (platform.SnapshotsDir
// does an MkdirAll), so a first run on a clean machine returns an empty
// slice rather than an error.
func ListSnapshotFiles() ([]SnapshotFile, error) {
	dir, err := platform.SnapshotsDir()
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	out := make([]SnapshotFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		// Accept ".json" (a real snapshot) and ".json.tmp" (the
		// half-written sibling an interrupted atomic write leaves
		// behind). The .tmp files are otherwise invisible — nothing
		// else in nodeup ever looks at them — so without this they
		// would accumulate unnoticed.
		isTmp := strings.HasSuffix(name, ".json.tmp")
		if !strings.HasSuffix(name, ".json") && !isTmp {
			continue
		}

		path := filepath.Join(dir, name)
		info, err := entry.Info()
		if err != nil {
			// The file vanished between ReadDir and Info (another
			// process deleted it, or it's a broken symlink). Skip it
			// rather than failing the whole listing.
			continue
		}

		sf := SnapshotFile{
			Path:    path,
			Name:    name,
			Size:    info.Size(),
			ModTime: info.ModTime(),
		}
		sf.Manager, sf.Version = parseSnapshotFilename(name)

		// Try to parse the body so we can tell the user what's inside.
		// A parse failure is recorded, not fatal — the file is still
		// listed and can still be deleted.
		data, err := os.ReadFile(path)
		if err != nil {
			sf.ParseError = err.Error()
		} else if s := parseSnapshotBody(data); s != nil {
			if sf.Manager == "" {
				sf.Manager = s.Manager
			}
			if sf.Version == "" {
				sf.Version = s.NodeVersion
			}
		} else {
			sf.ParseError = "snapshot file is not valid JSON"
		}

		out = append(out, sf)
	}

	// Oldest first — the files most likely to be stale come first.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ModTime.Equal(out[j].ModTime) {
			return out[i].Name < out[j].Name
		}
		return out[i].ModTime.Before(out[j].ModTime)
	})

	return out, nil
}

// parseSnapshotBody unmarshals snapshot JSON, returning nil when the
// body isn't a valid snapshot.
func parseSnapshotBody(data []byte) *SnapshotData {
	var s SnapshotData
	if err := json.Unmarshal(data, &s); err != nil {
		return nil
	}
	return &s
}

// parseSnapshotFilename splits a snapshot filename into its manager and
// version components.
//
// The convention is "<manager>-<version>.json" (see snapshotPath), e.g.
// "fnm-20.10.0.json" → ("fnm", "20.10.0"). Manager names may themselves
// contain hyphens ("vite-plus", "nvm-windows"), so we split on the LAST
// hyphen — the version never contains one, but the manager might.
//
// A ".tmp" suffix (the sibling left behind by an interrupted atomic
// write) is stripped first, so "fnm-20.10.0.json.tmp" parses the same
// way as its completed counterpart.
//
// Returns empty strings when the filename doesn't follow the convention.
func parseSnapshotFilename(name string) (manager, version string) {
	base := strings.TrimSuffix(name, ".tmp")
	base = strings.TrimSuffix(base, ".json")
	idx := strings.LastIndex(base, "-")
	if idx <= 0 || idx == len(base)-1 {
		return "", ""
	}
	return base[:idx], base[idx+1:]
}

// DeleteSnapshotFile removes a single snapshot file from disk.
//
// It refuses to delete anything that isn't a regular file inside the
// snapshots directory — a symlink to somewhere else, or a path that
// escaped the directory via "..", is rejected rather than followed.
// This matters because the path can come from user input (the CLI
// accepts snapshot names to delete) and we don't want a malformed
// argument to turn into an arbitrary-file-unlink.
//
// A file that is already gone is not an error — the desired end state
// (the file doesn't exist) is already true.
func DeleteSnapshotFile(path string) error {
	dir, err := platform.SnapshotsDir()
	if err != nil {
		return err
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}

	// Resolve symlinks on both sides so a symlink pointing outside the
	// snapshots dir is caught. EvalSymlinks fails on a non-existent
	// file, which we handle below (already-gone is not an error).
	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		realDir = dir
	}
	realPath, err := filepath.EvalSymlinks(abs)
	if err != nil {
		// The file may simply not exist. Check that first before
		// treating this as a hard error.
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("resolve snapshot path: %w", err)
	}

	if !isInsideDir(realDir, realPath) {
		return fmt.Errorf("refusing to delete %s: not inside the snapshots directory", path)
	}

	info, err := os.Lstat(realPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat snapshot: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("refusing to delete %s: not a regular file", path)
	}

	if err := os.Remove(realPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("delete snapshot: %w", err)
	}
	return nil
}

// DeleteSnapshotFiles removes several snapshot files, returning the
// names that were deleted and the failures that occurred.
//
// A failure on one file does not abort the rest — the caller gets a
// complete picture of what happened and can report it. This mirrors
// the cleanup loop in the upgrade command, which also collects failures
// instead of stopping at the first one.
func DeleteSnapshotFiles(paths []string) (deleted []string, failures map[string]error) {
	failures = map[string]error{}
	for _, p := range paths {
		if err := DeleteSnapshotFile(p); err != nil {
			failures[p] = err
			continue
		}
		deleted = append(deleted, p)
	}
	return deleted, failures
}

// isInsideDir reports whether path resolves to a location strictly
// inside dir (or equal to it).
//
// This is the same guard the tar extraction path uses (see #65): a
// naive strings.HasPrefix check would treat "/tmp/ab" as being inside
// "/tmp/a". path.relative handles that correctly.
func isInsideDir(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// ListReportFiles returns every migration report file present in the
// reports directory.
//
// Migration reports accumulate one-per-migration with a random-suffixed
// filename, so they are never overwritten and never reused. Like
// snapshots, they can leak; the cleanup command surfaces them alongside
// the snapshots so the user can reclaim that space too.
func ListReportFiles() ([]SnapshotFile, error) {
	dir, err := platform.ReportsDir()
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	out := make([]SnapshotFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		out = append(out, SnapshotFile{
			Path:    filepath.Join(dir, name),
			Name:    name,
			Size:    info.Size(),
			ModTime: info.ModTime(),
		})
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ModTime.Equal(out[j].ModTime) {
			return out[i].Name < out[j].Name
		}
		return out[i].ModTime.Before(out[j].ModTime)
	})

	return out, nil
}

// DeleteReportFile removes a single migration report file.
//
// Mirrors DeleteSnapshotFile's safety rules: the target must be a
// regular file inside the reports directory.
func DeleteReportFile(path string) error {
	dir, err := platform.ReportsDir()
	if err != nil {
		return err
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}

	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		realDir = dir
	}
	realPath, err := filepath.EvalSymlinks(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("resolve report path: %w", err)
	}

	if !isInsideDir(realDir, realPath) {
		return fmt.Errorf("refusing to delete %s: not inside the reports directory", path)
	}

	info, err := os.Lstat(realPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat report: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("refusing to delete %s: not a regular file", path)
	}

	if err := os.Remove(realPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("delete report: %w", err)
	}
	return nil
}
