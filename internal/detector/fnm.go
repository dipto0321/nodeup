package detector

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"

	"github.com/dipto0321/nodeup/internal/platform"
)

// FNM is the Fast Node Manager implementation. See
// internal/detector/detector.go for the Manager interface contract.
//
// Phase 1 implements the detection surface only:
//   - Detect       : cheap PATH probe via exec.LookPath
//   - Version      : `fnm --version`, parsed to drop the leading "fnm "
//   - ListInstalled: `fnm list`, parsed into sorted []semver.Version
//
// Mutation methods (Install, Uninstall, Use, SetDefault, GlobalNpmPrefix)
// return an explicit "not implemented" error so callers can detect them
// at runtime instead of getting a silent zero-value result.
type FNM struct{}

// NewFNM constructs a fresh fnm detector. Returned by value so each
// detection cycle gets its own state.
func NewFNM() *FNM { return &FNM{} }

func (f *FNM) Name() string { return "fnm" }

// runShell is the package-level seam used by FNM to invoke fnm. Tests
// overwrite it to capture arguments and return canned output without
// spawning a subprocess. Production code never reassigns it.
//
// Signature matches platform.RunShell so a direct assignment works.
var runShell = platform.RunShell

// ErrFNMNotImplemented is returned by FNM mutation methods that have not
// yet been implemented in Phase 1 (Install, Uninstall, Use, SetDefault,
// GlobalNpmPrefix). Returning this error instead of a zero value lets
// callers distinguish "I haven't done it yet" from "user passed a bad
// version" via errors.Is.
var ErrFNMNotImplemented = errors.New("fnm mutation commands not yet implemented")

// Detect returns true when an fnm executable can be located on PATH.
// Per the Manager contract, it MUST be cheap — exec.LookPath does a
// directory walk but no subprocess spawn.
func (f *FNM) Detect() bool {
	return platform.LookupManagerBinary("fnm") != ""
}

// Version returns fnm's own version string, e.g. "1.39.0". The binary
// emits "fnm 1.39.0\n" so we drop the leading "fnm " prefix and trim
// surrounding whitespace.
func (f *FNM) Version() (string, error) {
	res, err := runShell(context.Background(), "fnm", "--version")
	if err != nil {
		return "", fmt.Errorf("fnm --version: %w", err)
	}
	return parseFNMVersion(res.Stdout)
}

// parseFNMVersion extracts the version token from `fnm --version` output.
// Real observed output:
//
//	fnm 1.39.0\n
//
// We accept either "fnm X.Y.Z" or bare "X.Y.Z" (some forks omit the
// program name).
func parseFNMVersion(stdout string) (string, error) {
	out := strings.TrimSpace(stdout)
	if out == "" {
		return "", errors.New("fnm --version returned empty output")
	}
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return "", errors.New("fnm --version returned no tokens")
	}
	// If the first token is literally "fnm" take the next one.
	if fields[0] == "fnm" && len(fields) >= 2 {
		return strings.TrimSpace(fields[1]), nil
	}
	// Otherwise assume the whole string is already a version.
	return strings.TrimSpace(fields[0]), nil
}

// ListInstalled returns every Node.js version fnm has installed, sorted
// ascending. Source: `fnm list` which prints lines like:
//
//   - v24.15.0 default
//   - v25.9.0
//   - system
//
// The leading "* " is a default-marker (not per-line current); we strip
// it. The literal "system" line (which represents the system Node, not
// an fnm-managed install) is excluded — nodeup only cares about versions
// fnm actually manages.
func (f *FNM) ListInstalled() ([]semver.Version, error) {
	res, err := runShell(context.Background(), "fnm", "list")
	if err != nil {
		return nil, fmt.Errorf("fnm list: %w", err)
	}
	return parseFNMInstalled(res.Stdout)
}

// parseFNMInstalled turns raw `fnm list` output into a sorted-ascending
// []semver.Version. Exported (lowercase) for direct unit testing.
//
// Returns a non-nil empty slice (never nil) when no parseable versions
// are present — callers rely on this for "no versions, but no error"
// semantics (e.g., "fnm installed, nothing managed yet").
func parseFNMInstalled(stdout string) ([]semver.Version, error) {
	versions := make([]semver.Version, 0)
	for _, raw := range strings.Split(stdout, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		// Strip the leading "* " default marker if present.
		line = strings.TrimPrefix(line, "* ")
		line = strings.TrimPrefix(line, "*")
		line = strings.TrimSpace(line)

		// The first whitespace-separated token is the version or the
		// literal "system". Anything beyond it (e.g. "default", an alias
		// name) is metadata we don't need.
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		first := fields[0]
		if first == "system" {
			continue
		}
		// fnm emits "v22.11.0" (with v prefix); semver.NewVersion handles
		// that, but we normalize to be safe across forks.
		v, err := semver.NewVersion(strings.TrimPrefix(first, "v"))
		if err != nil {
			// Skip unparseable lines rather than aborting the whole list.
			continue
		}
		versions = append(versions, *v)
	}
	// semver.Collection in v3.5.0 is []*Version (pointers), so a value
	// slice doesn't satisfy it. Use sort.Slice with semver.Compare.
	sort.Slice(versions, func(i, j int) bool {
		return versions[i].Compare(&versions[j]) < 0
	})
	return versions, nil
}

// --- Mutation stubs -----------------------------------------------------
//
// Install, Uninstall, Use, SetDefault, and GlobalNpmPrefix return
// ErrFNMNotImplemented. They will be filled in when the upgrade command
// (Phase 4) needs to mutate state. Returning an explicit sentinel error
// (rather than nil) makes "not implemented" provably distinguishable
// from "succeeded".

func (f *FNM) Install(v semver.Version) error    { return ErrFNMNotImplemented }
func (f *FNM) Uninstall(v semver.Version) error  { return ErrFNMNotImplemented }
func (f *FNM) Use(v semver.Version) error        { return ErrFNMNotImplemented }
func (f *FNM) SetDefault(v semver.Version) error { return ErrFNMNotImplemented }
func (f *FNM) GlobalNpmPrefix(v semver.Version) (string, error) {
	return "", ErrFNMNotImplemented
}
