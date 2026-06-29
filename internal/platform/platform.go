// Package platform provides OS, shell, path, and env helpers used across
// the codebase.
//
// Design goals:
//   - All filesystem paths flow through path/filepath helpers — never
//     hardcoded "/" or "\\".
//   - All shell commands go through RunShell so we can centralize quoting,
//     environment setup (NVM_DIR, FNM_DIR, ...), and Windows quirks.
//   - Platform-specific behavior is gated by Go build tags in *_windows.go
//     and *_unix.go files. Files in this file are platform-agnostic.
//
// Cross-platform rules we follow:
//   - Use filepath.Join for any path concatenation.
//   - Use os.UserHomeDir() to find the home directory — it respects
//     USERPROFILE on Windows and HOME on unix.
//   - Quote shell args that may contain spaces (esp. Windows profiles).
package platform

import (
	"os"
	"path/filepath"
	"runtime"
)

// DataDir returns the per-user data directory where nodeup stores snapshots,
// cached API responses, migration reports, and the lock file.
//
//   - On Linux/macOS: $XDG_DATA_HOME/nodeup or $HOME/.local/share/nodeup
//   - On Windows:     %AppData%\nodeup
//
// The directory is created (with parents) if it does not already exist.
func DataDir() (string, error) {
	base, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	var dir string
	switch runtime.GOOS {
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData == "" {
			appData = filepath.Join(base, "AppData", "Roaming")
		}
		dir = filepath.Join(appData, "nodeup")
	case "darwin":
		// macOS convention is $HOME/Library/Application Support/<app>
		dir = filepath.Join(base, "Library", "Application Support", "nodeup")
	default: // linux + others
		xdg := os.Getenv("XDG_DATA_HOME")
		if xdg != "" {
			dir = filepath.Join(xdg, "nodeup")
		} else {
			dir = filepath.Join(base, ".local", "share", "nodeup")
		}
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// ConfigPath returns the absolute path to the config file.
// Resolved as <DataDir>/config.yaml.
func ConfigPath() (string, error) {
	d, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "config.yaml"), nil
}

// SnapshotsDir returns the directory holding per-version package snapshots.
// Resolved as <DataDir>/snapshots and created on demand.
func SnapshotsDir() (string, error) {
	d, err := DataDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(d, "snapshots")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// ReportsDir returns the directory holding migration reports.
// Resolved as <DataDir>/reports and created on demand.
func ReportsDir() (string, error) {
	d, err := DataDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(d, "reports")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// CacheDir returns the directory holding cached API responses.
// Resolved as <DataDir>/cache and created on demand.
func CacheDir() (string, error) {
	d, err := DataDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(d, "cache")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// LockPath returns the path to the concurrency lock file.
func LockPath() (string, error) {
	d, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "nodeup.lock"), nil
}

// IsWindows is a tiny helper to keep call-sites readable. We avoid sprinkling
// runtime.GOOS == "windows" throughout the codebase.
func IsWindows() bool { return runtime.GOOS == "windows" }

// IsMacOS is a tiny helper — used to gate macOS-specific UX hints
// (e.g., "do you have Rosetta installed for an x86_64 binary?").
func IsMacOS() bool { return runtime.GOOS == "darwin" }

// IsLinux is a tiny helper.
func IsLinux() bool { return runtime.GOOS == "linux" }

// IsARM64 reports whether the running binary is ARM64. We use this to
// decide whether to print a Rosetta hint on Apple Silicon when an
// x86_64-only manager binary is on PATH.
func IsARM64() bool { return runtime.GOARCH == "arm64" }