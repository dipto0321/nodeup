package detector

// AllowedManagerNames returns the canonical set of manager names
// nodeup accepts from user input — `--manager`, the `<manager>` slot
// in `nodeup packages restore <manager> <version>`, and the
// `<manager>` slot in `nodeup packages diff <manager> ...`.
//
// This is the *allowlist* the cli uses to block path-traversal
// attempts: a manager name like `../../tmp/evil` would otherwise be
// interpolated into a snapshot filename via `fmt.Sprintf` and then
// `filepath.Join`'d into the snapshots dir — and `filepath.Join`
// collapses `..` segments, taking the result outside the snapshots
// dir. Validating the name against this list before any path is
// constructed closes that confused-deputy surface. See issue #51.
//
// Built dynamically from All() so the set stays in sync with
// per-platform registry_*.go build files (e.g. nvm-windows on
// Windows only). Kept in its own file (no build tag) so the
// allowlist works identically on every platform — Windows's
// nvm-windows stays in the list on Linux builds where it would
// never install, but the cost of an extra name in a 9-element list
// is nil and the consistency helps audit.
func AllowedManagerNames() []string {
	all := All()
	out := make([]string, 0, len(all))
	for _, m := range all {
		out = append(out, m.Name())
	}
	return out
}

// IsAllowedManagerName reports whether `name` is one of the canonical
// manager-name strings nodeup accepts from user input. The match is
// case-sensitive (matches All()[i].Name() byte-for-byte) — the
// `--manager` flag uses the same case-sensitive lookup, and silently
// accepting "FNM" / "FnM" would just create another inconsistency
// to debug later. See #51.
func IsAllowedManagerName(name string) bool {
	for _, candidate := range AllowedManagerNames() {
		if candidate == name {
			return true
		}
	}
	return false
}
