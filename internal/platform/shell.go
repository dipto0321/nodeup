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
// IMPORTANT: any filesystem path embedded in `script` MUST be passed
// through QuotePath first. We do not re-tokenize or auto-quote the
// script string — what you give us is what the shell sees. Forgetting
// QuotePath for a path that contains spaces (very common on Windows,
// e.g. `C:\Program Files\...`) will produce a confusing parse error
// or, worse, a silent substring split that runs the wrong command.
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
// Quoting rules by platform:
//
//   - On unix (bash): we wrap in single quotes, escaping any embedded
//     single quote as `'\”` (close-quote, literal-quote, reopen-quote).
//     Inside single quotes, bash performs NO expansion — `$`, “ ` “,
//     `\\`, `"`, `;`, `|` all become literal. This is the only quoting
//     mode that fully neuters command injection, including for values
//     like NVM_DIR that are read from the environment without further
//     validation. See #43.
//   - On Windows (cmd.exe): we wrap in double quotes. cmd.exe's unquote
//     rules differ from bash (notably, `\` and `"` have special
//     handling); the goal here is just to keep paths-with-spaces
//     together, not to defeat injection (the nvm path on Windows goes
//     through the nvm-windows manager, which has its own quoting story).
func QuotePath(p string) string {
	if p == "" {
		return `""`
	}
	if runtime.GOOS == "windows" {
		return quotePathWindows(p)
	}
	return quotePathPOSIX(p)
}

// quotePathPOSIX wraps p in single quotes, escaping any embedded `'` as
// `'\”`. The result is safe to embed verbatim in a bash script — bash
// will not expand any variable, command substitution, or backtick
// inside the quoted region.
func quotePathPOSIX(p string) string {
	// The set of characters that are unconditionally safe to leave
	// inside a single-quoted string WITHOUT triggering any quoting
	// requirement: every byte except `'` itself. We still always wrap
	// (rather than only-on-demand) to keep the function's contract
	// "result is always a shell-safe literal of the input" — callers
	// can rely on the output being safe regardless of the input.
	//
	// Always wrap, then handle the single-quote escape inside.
	return `'` + strings.ReplaceAll(p, `'`, `'\''`) + `'`
}

// quotePathWindows wraps p for cmd.exe. On Windows, only `"` forces
// quoting (cmd.exe requires the path to be wrapped in double quotes if
// it contains spaces or tabs, and `"` inside the path must be escaped).
// Other characters — including `\\`, `%`, `&`, `|`, `<`, `>` — have
// cmd.exe-specific handling that we do NOT attempt to fully neutralize
// here; the use case is filesystem paths that may have spaces
// ("C:\Program Files\…"), not arbitrary user-controlled strings.
func quotePathWindows(p string) string {
	safe := true
	unsafeChars := " \""
	for _, c := range p {
		if strings.ContainsRune(unsafeChars, c) {
			safe = false
			break
		}
	}
	if safe {
		return p
	}
	escaped := strings.ReplaceAll(p, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	return `"` + escaped + `"`
}

// DefaultCommandTimeout is the default upper bound for any single command
// invocation. Long-running commands (e.g., `fnm install`) should be
// opted in to a longer timeout by the caller.
const DefaultCommandTimeout = 60 * time.Second
