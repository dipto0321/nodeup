package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/dipto0321/nodeup/internal/config"
)

// loadConfigOrDefault resolves the effective Config the way the docs promise:
//
//	defaults < config file < environment variables
//
// CLI flag overrides happen in the individual RunE handlers (they're the top
// layer in the precedence chain). Errors reading or parsing the config file
// are returned verbatim so the user sees the path + reason. A missing file
// is not an error — it just means "use the defaults".
//
// This is the single chokepoint for every subcommand that needs config so
// the behavior is consistent.
func loadConfigOrDefault() (*config.Config, error) {
	path, err := config.DefaultPath()
	if err != nil {
		return nil, fmt.Errorf("resolve config path: %w", err)
	}

	// If the file exists, load it. If it doesn't, that's fine — defaults
	// are still a valid starting point. Anything else (permission denied,
	// malformed YAML, invalid value) is an error.
	cfg, _, err := config.Load(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("load config from %s: %w", path, err)
	}

	// Overlay env vars on top of (defaults | file).
	envLayer, err := config.FromEnv()
	if err != nil {
		return nil, err
	}
	final := config.Resolve(cfg, envLayer)
	if err := final.Validate(); err != nil {
		return nil, fmt.Errorf("validate merged config: %w", err)
	}
	return final, nil
}

// runWithConfig is a convenience wrapper for subcommand RunE handlers
// that just want the merged Config. They don't need to know how it's
// loaded.
func runWithConfig(cmd *cobra.Command, fn func(*config.Config) error) error {
	cfg, err := loadConfigOrDefault()
	if err != nil {
		return err
	}
	return fn(cfg)
}
