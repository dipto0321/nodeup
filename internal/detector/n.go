package detector

import "github.com/Masterminds/semver/v3"

// N is the npm-based version manager (https://github.com/tj/n). Simple
// binary installed via npm, prefix configurable via N_PREFIX.
//
// Implementation status: stub. Real implementation lands in Phase 5.
type N struct{}

// NewN constructs a fresh n detector.
func NewN() *N { return &N{} }

func (n *N) Name() string { return "n" }

func (n *N) Detect() bool                             { return false }
func (n *N) Version() (string, error)                { return "", nil }
func (n *N) ListInstalled() ([]semver.Version, error) { return nil, nil }
func (n *N) Install(ver semver.Version) error        { return nil }
func (n *N) Uninstall(ver semver.Version) error      { return nil }
func (n *N) Use(ver semver.Version) error            { return nil }
func (n *N) SetDefault(ver semver.Version) error     { return nil }
func (n *N) GlobalNpmPrefix(ver semver.Version) (string, error) {
	return "", nil
}