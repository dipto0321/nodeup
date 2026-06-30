package packages

import (
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
}

// NewMigrationReport creates a fresh report for a migration.
func NewMigrationReport(manager, fromVersion, toVersion string) *MigrationReport {
	return &MigrationReport{
		Timestamp:   time.Now(),
		Manager:     manager,
		FromVersion: fromVersion,
		ToVersion:   toVersion,
	}
}

// Save writes the report to ~/.nodeup/reports/<timestamp>.json.
func (r *MigrationReport) Save() error {
	dir, err := platform.ReportsDir()
	if err != nil {
		return err
	}

	filename := fmt.Sprintf("migration-%s.json", r.Timestamp.Format("20060102-150405"))
	path := filepath.Join(dir, filename)

	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal report: %w", err)
	}

	return os.WriteFile(path, data, 0o644)
}

// AddResult appends a package result to the report.
func (r *MigrationReport) AddResult(pr PackageResult) {
	r.Results = append(r.Results, pr)
}
