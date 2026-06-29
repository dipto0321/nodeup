//go:build !windows

package detector

// All returns every manager implementation nodeup knows about, in a
// stable order. The full slice is constructed here so individual manager
// files don't have to be edited when a new one is added.
//
// Platform-specific managers are appended via build-tagged registry_*.go
// files (see registry_windows.go for nvm-windows).
//
// Returned slice order is also the "default priority" for ResolveManager
// when the user has multiple managers installed and no explicit preference.
func All() []Manager {
	return []Manager{
		NewFNM(),
		NewNVM(),
		NewVolta(),
		NewASDF(),
		NewMise(),
		NewN(),
		NewNodenv(),
	}
}

// ByName returns a freshly-constructed manager of the given name. Used
// when the user passes --manager <name> but that manager wasn't auto-
// detected (rare but possible if PATH is unusual).
func ByName(name string) (Manager, bool) {
	for _, m := range All() {
		if m.Name() == name {
			return m, true
		}
	}
	return nil, false
}

// Priority returns the index of the manager in the All() slice. Lower
// numbers are preferred. fnm wins over nvm by convention because it's
// faster and the project owner's primary tool.
func Priority(name string) int {
	all := All()
	for i, m := range all {
		if m.Name() == name {
			return i
		}
	}
	return len(all)
}