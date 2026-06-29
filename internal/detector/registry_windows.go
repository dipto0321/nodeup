//go:build windows

package detector

import "github.com/Masterminds/semver/v3"

// NVMWindows is the Windows-only nvm-windows implementation
// (https://github.com/coreybutler/nvm-windows). Unlike nvm on unix,
// nvm-windows ships a real binary (nvm.exe) so we can call it like any
// other command — no shell-sourcing tricks.
//
// Implementation status: stub. Real implementation lands in Phase 5.
type NVMWindows struct{}

// NewNVMWindows constructs a fresh nvm-windows detector.
func NewNVMWindows() *NVMWindows { return &NVMWindows{} }

func (n *NVMWindows) Name() string { return "nvm-windows" }

func (n *NVMWindows) Detect() bool                             { return false }
func (n *NVMWindows) Version() (string, error)                 { return "", nil }
func (n *NVMWindows) ListInstalled() ([]semver.Version, error) { return nil, nil }
func (n *NVMWindows) Install(v semver.Version) error           { return nil }
func (n *NVMWindows) Uninstall(v semver.Version) error         { return nil }
func (n *NVMWindows) Use(v semver.Version) error               { return nil }
func (n *NVMWindows) SetDefault(v semver.Version) error        { return nil }
func (n *NVMWindows) GlobalNpmPrefix(v semver.Version) (string, error) {
	return "", nil
}

// All returns every manager implementation nodeup knows about on Windows.
// Identical to the unix All() but appends NewNVMWindows at the end so
// ResolveManager prefers fnm/nvm first.
func All() []Manager {
	return []Manager{
		NewFNM(),
		NewNVM(),
		NewVolta(),
		NewASDF(),
		NewMise(),
		NewN(),
		NewNodenv(),
		NewNVMWindows(),
	}
}

// ByName returns a freshly-constructed manager of the given name.
func ByName(name string) (Manager, bool) {
	for _, m := range All() {
		if m.Name() == name {
			return m, true
		}
	}
	return nil, false
}

// Priority returns the index of the manager in the All() slice.
func Priority(name string) int {
	all := All()
	for i, m := range all {
		if m.Name() == name {
			return i
		}
	}
	return len(all)
}
