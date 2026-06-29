package detector

import "github.com/Masterminds/semver/v3"

// Volta is the Volta implementation (https://volta.sh). Volta is a
// binary at ~/.volta/bin/volta with VOLTA_HOME env var.
//
// Implementation status: stub. Real implementation lands in Phase 5.
type Volta struct{}

// NewVolta constructs a fresh Volta detector.
func NewVolta() *Volta { return &Volta{} }

func (v *Volta) Name() string { return "volta" }

func (v *Volta) Detect() bool                             { return false }
func (v *Volta) Version() (string, error)                { return "", nil }
func (v *Volta) ListInstalled() ([]semver.Version, error) { return nil, nil }
func (v *Volta) Install(ver semver.Version) error        { return nil }
func (v *Volta) Uninstall(ver semver.Version) error      { return nil }
func (v *Volta) Use(ver semver.Version) error            { return nil }
func (v *Volta) SetDefault(ver semver.Version) error     { return nil }
func (v *Volta) GlobalNpmPrefix(ver semver.Version) (string, error) {
	return "", nil
}