package config

import (
	"errors"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Load reads a config file from path, applies defaults to any missing
// fields, and validates the result. If the file does not exist, the
// returned Config is the pure defaults (and fromFile is false, so
// callers know the config was not actually persisted).
//
// A malformed YAML file returns an error that wraps the underlying
// yaml.TypeError or yaml.UnmarshalTypeError so callers can show line/
// column info if they want.
//
// Missing-key behavior: yaml.v3 unmarshalling cannot distinguish
// "key absent" from "key present with zero value" for non-pointer
// fields. We walk the YAML document tree (yaml.Node) to record which
// top-level and nested keys were actually present, then build a
// FileOverlay that only marks present keys as set. Keys the user
// omitted keep their default value.
func Load(path string) (cfg *Config, fromFile bool, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Default(), false, nil
		}
		return nil, false, fmt.Errorf("read %s: %w", path, err)
	}

	// First pass: decode into a yaml.Node so we can see which keys
	// are present, even if their values happen to be zero.
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, false, fmt.Errorf("parse %s: %w", path, err)
	}

	// Second pass: decode into the actual struct.
	loaded := &Config{}
	if err := yaml.Unmarshal(data, loaded); err != nil {
		return nil, false, fmt.Errorf("parse %s: %w", path, err)
	}

	cfg = Resolve(Default(), FileOverlayFromNode(loaded, &root))
	if err := cfg.Validate(); err != nil {
		return nil, false, fmt.Errorf("invalid %s: %w", path, err)
	}
	return cfg, true, nil
}

// Save writes cfg to path as YAML, atomically replacing any existing
// file. The directory is created with mode 0o755 if it does not exist.
// The file is written with mode 0o600 because it may grow to contain
// sensitive settings in future releases (e.g. private registry tokens).
func Save(path string, cfg *Config) error {
	if cfg == nil {
		return errors.New("Save: cfg is nil")
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("refusing to save invalid config: %w", err)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	if err := os.MkdirAll(dirOf(path), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	// Write to a sibling temp file, then rename — readers never see a
	// half-written config.
	tmp, err := os.CreateTemp(dirOf(path), ".config-*.yaml.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		// Best-effort cleanup if we never rename.
		_ = os.Remove(tmpName)
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename temp file -> %s: %w", path, err)
	}
	return nil
}

// Scaffold returns a Config suitable for writing into a fresh config
// file via `nodeup config init`. It is currently identical to Default()
// but is a separate function so we can grow it (e.g. add comments per
// field) without breaking callers that depend on Default being
// value-only.
func Scaffold() *Config {
	return Default()
}

func dirOf(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == os.PathSeparator {
			return path[:i]
		}
	}
	return "."
}
