//go:build windows

package detector

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
