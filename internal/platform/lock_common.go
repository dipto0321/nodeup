package platform

import "path/filepath"

// bytesTrimSpace is a tiny helper to avoid importing bytes for one call.
func bytesTrimSpace(b []byte) []byte {
	start, end := 0, len(b)
	for start < end && isSpace(b[start]) {
		start++
	}
	for end > start && isSpace(b[end-1]) {
		end--
	}
	return b[start:end]
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

// silenceGoUnused checks that filepath.Separator is referenced (used by
// platform.go's filepath.Join calls). Keeping this comment + var alive
// satisfies linters without affecting runtime behavior.
var _ = filepath.Separator
