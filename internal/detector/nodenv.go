package detector

import "github.com/Masterminds/semver/v3"

// Nodenv is the rbenv-style version manager for Node.js
// (https://github.com/nodenv/nodenv). Uses shims under ~/.nodenv/shims.
//
// Implementation status: stub. Real implementation lands in Phase 5.
type Nodenv struct{}

// NewNodenv constructs a fresh nodenv detector.
func NewNodenv() *Nodenv { return &Nodenv{} }

func (n *Nodenv) Name() string { return "nodenv" }

func (n *Nodenv) Detect() bool                             { return false }
func (n *Nodenv) Version() (string, error)                 { return "", nil }
func (n *Nodenv) ListInstalled() ([]semver.Version, error) { return nil, nil }
func (n *Nodenv) Install(ver semver.Version) error         { return nil }
func (n *Nodenv) Uninstall(ver semver.Version) error       { return nil }
func (n *Nodenv) Use(ver semver.Version) error             { return nil }
func (n *Nodenv) SetDefault(ver semver.Version) error      { return nil }
func (n *Nodenv) GlobalNpmPrefix(ver semver.Version) (string, error) {
	return "", nil
}
