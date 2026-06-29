package detector

import "github.com/Masterminds/semver/v3"

// NVM is the Node Version Manager implementation. nvm is unusual because
// it is a SHELL FUNCTION, not a binary. See nodeup.md §5 "The nvm
// Special Case" for the three strategies we use.
//
// Strategy C is preferred for read operations (parse ~/.nvm/versions/node/*
// directly). For mutating operations (install, uninstall, use) we fall
// back to Strategy A: `bash -c "source ~/.nvm/nvm.sh && nvm <cmd>"`.
//
// Implementation status: stub. Real implementation lands in Phase 1.
type NVM struct{}

// NewNVM constructs a fresh nvm detector.
func NewNVM() *NVM { return &NVM{} }

func (n *NVM) Name() string { return "nvm" }

func (n *NVM) Detect() bool                              { return false }
func (n *NVM) Version() (string, error)                 { return "", nil }
func (n *NVM) ListInstalled() ([]semver.Version, error)  { return nil, nil }
func (n *NVM) Install(v semver.Version) error           { return nil }
func (n *NVM) Uninstall(v semver.Version) error         { return nil }
func (n *NVM) Use(v semver.Version) error               { return nil }
func (n *NVM) SetDefault(v semver.Version) error        { return nil }
func (n *NVM) GlobalNpmPrefix(v semver.Version) (string, error) {
	return "", nil
}