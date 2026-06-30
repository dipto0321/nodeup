package cli

import (
	"errors"
	"os"
)

// isNotExist returns true if err is a "file not found" error from the
// OS. Wrapping errors.Is/os.IsNotExist gives us correct behavior for
// both raw syscall errors and wrapped ones.
func isNotExist(err error) bool {
	return errors.Is(err, os.ErrNotExist)
}

// fileExists returns true if path resolves to an existing file. A nil
// error from os.Stat means the file is there. We don't distinguish
// "is a directory" vs "is a file" — both are non-zero existence signals;
// the callers (config init, etc.) make their own judgement.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
