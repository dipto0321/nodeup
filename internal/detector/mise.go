package detector

import "github.com/Masterminds/semver/v3"

// Mise (formerly rtx) is the asdf-successor implementation.
//
// Detection:
//   - binary on PATH (`mise`)
//   - mise plugins list | grep node
//
// Implementation status: stub. Real implementation lands in Phase 5.
type Mise struct{}

// NewMise constructs a fresh mise detector.
func NewMise() *Mise { return &Mise{} }

func (m *Mise) Name() string { return "mise" }

func (m *Mise) Detect() bool                             { return false }
func (m *Mise) Version() (string, error)                 { return "", nil }
func (m *Mise) ListInstalled() ([]semver.Version, error) { return nil, nil }
func (m *Mise) Install(ver semver.Version) error         { return nil }
func (m *Mise) Uninstall(ver semver.Version) error       { return nil }
func (m *Mise) Use(ver semver.Version) error             { return nil }
func (m *Mise) SetDefault(ver semver.Version) error      { return nil }
func (m *Mise) GlobalNpmPrefix(ver semver.Version) (string, error) {
	return "", nil
}
