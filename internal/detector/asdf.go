package detector

import "github.com/Masterminds/semver/v3"

// ASDF is the asdf-vm implementation with the nodejs plugin installed.
//
// Detection:
//   - binary on PATH (`asdf`)
//   - ASDF_DIR env var OR ~/.asdf directory
//   - asdf plugin list | grep nodejs
//
// Implementation status: stub. Real implementation lands in Phase 5.
type ASDF struct{}

// NewASDF constructs a fresh asdf detector.
func NewASDF() *ASDF { return &ASDF{} }

func (a *ASDF) Name() string { return "asdf" }

func (a *ASDF) Detect() bool                             { return false }
func (a *ASDF) Version() (string, error)                { return "", nil }
func (a *ASDF) ListInstalled() ([]semver.Version, error) { return nil, nil }
func (a *ASDF) Install(ver semver.Version) error        { return nil }
func (a *ASDF) Uninstall(ver semver.Version) error      { return nil }
func (a *ASDF) Use(ver semver.Version) error            { return nil }
func (a *ASDF) SetDefault(ver semver.Version) error     { return nil }
func (a *ASDF) GlobalNpmPrefix(ver semver.Version) (string, error) {
	return "", nil
}