package detector

import (
	"reflect"
	"sort"
	"testing"
)

// TestAllowedManagerNames_MatchesAll covers the platform-correct set:
// the allowlist mirrors All()[i].Name() exactly. nvm-windows only
// appears on Windows builds (registry_windows.go), so we don't
// hard-code the expected slice — we derive it from All() and assert
// the two stay synchronized.
func TestAllowedManagerNames_MatchesAll(t *testing.T) {
	want := make([]string, 0, len(All()))
	for _, m := range All() {
		want = append(want, m.Name())
	}
	sort.Strings(want)

	got := append([]string(nil), AllowedManagerNames()...)
	sort.Strings(got)

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("AllowedManagerNames = %v, want %v", got, want)
	}
}

// TestIsAllowedManagerName pins the case-sensitive lookup contract.
//
// Case-sensitive matters: the `--manager` flag and the `<manager>`
// positional in `packages restore` / `packages diff` all do
// byte-for-byte case-sensitive matches, and a silent case-fold
// here would just paper over the next inconsistency instead of
// surfacing it. The negative cases include both genuine traversal
// payloads and harmless-looking-but-unrelated strings so a future
// regression that flips the case-fold would be caught.
func TestIsAllowedManagerName(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		// Positive: every name in the canonical allowlist. We don't
		// hard-code the list — we iterate AllowedManagerNames() so
		// a future addition flows through this test automatically.
		{"fnm", "fnm", true},
		{"empty string", "", false},

		// Negative: traversal payloads (the bug from #51).
		{"dotdot traversal", "../etc/passwd", false},
		{"absolute path", "/etc/passwd", false},
		{"mixed traversal", "../../tmp/evil", false},
		{"backslash on windows-style path", "..\\..\\evil", false},
		{"dot segment hidden", "./fnm", false},

		// Negative: case-folding must NOT match (see comment above).
		{"uppercase FNM", "FNM", false},
		{"title case FnM", "FnM", false},

		// Negative: strings that look manager-shaped but aren't.
		{"near-miss fnmm", "fnmm", false},
		{"near-miss fn", "fn", false},
		{"garbage", "lol", false},
		{"space-padded", " fnm", false},
		{"trailing slash", "fnm/", false},
	}

	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			if got := IsAllowedManagerName(tc.in); got != tc.want {
				t.Errorf("IsAllowedManagerName(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}

	// Sanity: every name in AllowedManagerNames() must also pass the
	// boolean helper. Catches an off-by-one in the iteration order
	// (e.g. if someone typed `if candidate > name` instead of `==`).
	for _, name := range AllowedManagerNames() {
		if !IsAllowedManagerName(name) {
			t.Errorf("AllowedManagerNames lists %q but IsAllowedManagerName says false", name)
		}
	}
}
