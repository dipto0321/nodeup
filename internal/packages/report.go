package packages

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/dipto0321/nodeup/internal/platform"
)

// PackageResult tracks the outcome of a single package install operation.
type PackageResult struct {
	Name             string
	Status           string // ok, failed, skipped, version_mismatch
	AttemptedVersion string
	InstalledVersion string
	Error            string
}

// MigrationReport records the results of a package migration.
type MigrationReport struct {
	Timestamp   time.Time       `json:"timestamp"`
	Manager     string          `json:"manager"`
	FromVersion string          `json:"from_version"`
	ToVersion   string          `json:"to_version"`
	Interrupted bool            `json:"interrupted"`
	Results     []PackageResult `json:"results"`

	// tag is the on-disk-filename discriminator; assigned lazily on
	// the first Path()/Save() call. Not serialized — it's purely a
	// filename concern, not part of the report's content.
	tag string
}

// NewMigrationReport creates a fresh report for a migration. The
// returned report has a unique 4-byte hex tag in its Timestamp-derived
// filename (see Path/Save) so two upgrades running within the same
// wall-clock second can't accidentally clobber each other's report.
func NewMigrationReport(manager, fromVersion, toVersion string) *MigrationReport {
	return &MigrationReport{
		Timestamp:   time.Now(),
		Manager:     manager,
		FromVersion: fromVersion,
		ToVersion:   toVersion,
	}
}

// Save writes the report to <DataDir>/reports/migration-<ts>-<tag>.json.
// The first call computes a stable 4-byte hex tag and caches it on the
// report; subsequent calls (Path and Save) reuse the same tag, so the
// CLI can call Path() before Save() to print the path and have it
// match the file that Save() actually writes.
//
// The tag uses crypto/rand entropy, so two reports produced within the
// same wall-clock second get distinct filenames. 32 bits of entropy is
// plenty to make accidental collisions vanishingly unlikely across the
// lifetime of a single machine's DataDir (~1 report per upgrade,
// upgrades are minutes-to-hours apart for a real user).
//
// Save is NOT safe to call concurrently on the same report instance:
// the tag is assigned once and shared across Path/Save, and two
// parallel Save calls would race to write the same file. Callers
// don't actually need concurrent saves — a single upgrade writes at
// most one report.
func (r *MigrationReport) Save() error {
	path, err := r.Path()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal report: %w", err)
	}
	return os.WriteFile(path, data, 0o644)
}

// Path returns the on-disk path Save() would write to. The first call
// assigns a stable random suffix; subsequent calls return the same
// filename. Exposed so the CLI can print the path to the user without
// re-deriving the timestamp-formatting rule.
//
// Two different MigrationReport instances (i.e., two upgrades) get
// independent random suffixes — Path() called on each one resolves to
// a unique filename even within the same second.
func (r *MigrationReport) Path() (string, error) {
	if r.tag == "" {
		tag, err := reportTag()
		if err != nil {
			return "", fmt.Errorf("generate report tag: %w", err)
		}
		r.tag = tag
	}
	dir, err := platform.ReportsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, r.filename()), nil
}

// filename is the basename portion of the on-disk path. Centralized
// so Path() and any future tests stay in lockstep with the format.
func (r *MigrationReport) filename() string {
	return fmt.Sprintf("migration-%s-%s.json", r.Timestamp.Format("20060102-150405"), r.tag)
}

// reportTag returns 4 hex bytes (8 chars) of crypto-random entropy
// sourced from crypto/rand. 32 bits of entropy is plenty to make
// accidental collisions vanishingly unlikely across the lifetime of a
// single machine's DataDir (~1 report per upgrade, upgrades are
// minutes-to-hours apart for a real user). We deliberately do NOT use
// math/rand here — that would couple report-naming to Go's seeded PRNG
// and could produce identical tags in two different processes
// initialized identically.
func reportTag() (string, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// AddResult appends a package result to the report.
func (r *MigrationReport) AddResult(pr PackageResult) {
	r.Results = append(r.Results, pr)
}
