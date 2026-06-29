package platform

import (
	"bytes"
	"testing"
)

// TestBytesTrimSpace is a parametric test for the trim helper shared by
// the unix and windows lock implementations. Guards against accidental
// regressions in whitespace handling — critical because lockfile contents
// are integer-only and any leading newline would parse as zero.
func TestBytesTrimSpace(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want []byte
	}{
		{"nil", nil, nil},
		{"empty", []byte{}, []byte{}},
		{"plain", []byte("12345"), []byte("12345")},
		{"leading spaces", []byte("  \t12345"), []byte("12345")},
		{"trailing newline", []byte("12345\n\r\n"), []byte("12345")},
		{"mixed", []byte("\t 12345 \n"), []byte("12345")},
		{"only whitespace", []byte("   \t\n"), []byte{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := bytesTrimSpace(tc.in)
			if !bytes.Equal(got, tc.want) {
				t.Errorf("bytesTrimSpace(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
