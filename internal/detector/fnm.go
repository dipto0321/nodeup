package detector

import "github.com/Masterminds/semver/v3"

// FNM is the Fast Node Manager implementation. See nodeup.md §5 for the
// detection strategy and the supported command surface.
//
// Detection:
//   - binary on PATH (`fnm`)
//   - FNM_DIR env var
//   - ~/.local/share/fnm (Linux), ~/Library/Application Support/fnm (macOS),
//     %AppData%\fnm (Windows)
//
// Implementation status: stub. Real implementation lands in Phase 1.
type FNM struct{}

// NewFNM constructs a fresh fnm detector. Returned by value so each
// detection cycle gets its own state.
func NewFNM() *FNM { return &FNM{} }

func (f *FNM) Name() string { return "fnm" }

// Stub methods. Phase 1 fills these in.
func (f *FNM) Detect() bool                          { return false }
func (f *FNM) Version() (string, error)             { return "", nil }
func (f *FNM) ListInstalled() ([]semver.Version, error) { return nil, nil }
func (f *FNM) Install(v semver.Version) error       { return nil }
func (f *FNM) Uninstall(v semver.Version) error     { return nil }
func (f *FNM) Use(v semver.Version) error           { return nil }
func (f *FNM) SetDefault(v semver.Version) error    { return nil }
func (f *FNM) GlobalNpmPrefix(v semver.Version) (string, error) {
	return "", nil
}