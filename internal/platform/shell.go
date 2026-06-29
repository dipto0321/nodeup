package platform

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// RunResult captures the outcome of a shell command. It is the single
// return type for the helpers in this file so call-sites can handle
// errors uniformly.
type RunResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// ErrNotFound is returned by RunShell when the executable cannot be located
// on PATH. Callers can use errors.Is to detect this specifically.
var ErrNotFound = errors.New("executable not found")

// RunShell runs an executable with arguments, capturing stdout and stderr.
//
// Cross-platform rules enforced here:
//   - On Windows we look up the binary with LookPath which already checks
//     PATHEXT (.exe, .cmd, .bat).
//   - We do NOT go through a shell (/bin/sh -c) on the host — instead we
//     exec the binary directly to avoid quoting nightmares. Callers that
//     need shell features (pipelines, redirects, source'ing .nvm.sh)
//     should use RunShellScript which is platform-aware.
//   - ctx is honored for cancellation.
func RunShell(ctx context.Context, name string, args ...string) (*RunResult, error) {
	if _, err := exec.LookPath(name); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, name)
	}

	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	res := &RunResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: 0,
	}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			res.ExitCode = exitErr.ExitCode()
			return res, fmt.Errorf("command %q exited with code %d: %s", name, res.ExitCode, strings.TrimSpace(stderr.String()))
		}
		return res, err
	}

	return res, nil
}

// RunShellScript executes a shell command string via the platform's shell.
//
//   - On unix we use `bash -c <cmd>`. We force bash (not sh) because nvm
//     users commonly `source ~/.nvm/nvm.sh` and nvm.sh is bash-only.
//   - On Windows we use `cmd.exe /c <cmd>` (cmd.exe is guaranteed present).
//
// Returns ErrNotFound if the shell itself is missing, which should be
// effectively impossible on a normal install.
func RunShellScript(ctx context.Context, script string) (*RunResult, error) {
	if runtime.GOOS == "windows" {
		return RunShell(ctx, "cmd.exe", "/c", script)
	}

	// Try bash first (nvm-friendly), fall back to sh.
	if _, err := exec.LookPath("bash"); err == nil {
		return RunShell(ctx, "bash", "-c", script)
	}
	return RunShell(ctx, "sh", "-c", script)
}

// LookupManagerBinary finds a manager executable on PATH and returns its
// absolute path. Returns an empty string and a nil error if not found —
// callers can use this for "soft" detection where they want to print a
// "not installed" hint rather than an error.
func LookupManagerBinary(name string) string {
	p, err := exec.LookPath(name)
	if err != nil {
		return ""
	}
	return p
}

// EnvWithSource returns an os/exec environment pre-loaded with common
// Node version-manager environment variables so that spawned child
// processes can find their tools even when the parent was launched
// from a non-interactive context (e.g., CI).
//
// We deliberately set NVM_DIR / FNM_DIR / VOLTA_HOME if those env vars
// are present in our own environment — we don't *create* them, since
// each manager does that itself.
func EnvWithSource() []string {
	env := os.Environ()

	// Some managers (notably fnm) need HOME / USERPROFILE to be set so
	// they can locate their data dir. These are virtually always present
	// in os.Environ() but we guard anyway.
	hasHome := false
	for _, v := range env {
		if strings.HasPrefix(v, "HOME=") || strings.HasPrefix(v, "USERPROFILE=") {
			hasHome = true
			break
		}
	}
	if !hasHome {
		if h, err := os.UserHomeDir(); err == nil {
			if runtime.GOOS == "windows" {
				env = append(env, "USERPROFILE="+h)
			} else {
				env = append(env, "HOME="+h)
			}
		}
	}

	return env
}

// QuotePath wraps a filesystem path in the appropriate shell quotes for
// embedding in a shell script. Used when we build a `bash -c` script that
// contains paths from the user (e.g., ~/.nvm/nvm.sh). Spaces are common
// on Windows ("C:\Program Files\...").
//
// We use double-quotes on unix (preserving $variables) and double-quotes
// on Windows inside cmd.exe — which won't expand %FOO% inside double
// quotes the same way, but it's still safer than leaving the path bare.
func QuotePath(p string) string {
	if p == "" {
		return `""`
	}
	// If the path contains no characters that the shell would interpret,
	// return it as-is. Otherwise wrap in double quotes and escape embedded
	// double quotes / backslashes appropriately per platform.
	safe := true
	unsafeChars := " \"$`<>|'"
	if runtime.GOOS == "windows" {
		unsafeChars = " \""
	}
	for _, c := range p {
		if strings.ContainsRune(unsafeChars, c) {
			safe = false
			break
		}
	}
	if safe {
		return p
	}
	// Escape backslashes and double quotes for double-quoted string.
	escaped := strings.ReplaceAll(p, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	return `"` + escaped + `"`
}

// DefaultCommandTimeout is the default upper bound for any single command
// invocation. Long-running commands (e.g., `fnm install`) should be
// opted in to a longer timeout by the caller.
const DefaultCommandTimeout = 60 * time.Second
