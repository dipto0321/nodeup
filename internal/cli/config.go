package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/dipto0321/nodeup/internal/config"
	"github.com/dipto0321/nodeup/internal/platform"
)

// newConfigCmd implements `nodeup config ...` subcommands.
//
// Subcommands:
//
//	nodeup config show          Print the merged effective config as YAML.
//	nodeup config get <key>     Print a single dotted key (e.g. packages.migrate).
//	nodeup config set <k> <v>   Update a key in the config file and save.
//	nodeup config init          Scaffold a fresh config file at the default
//	                            path (refuses to overwrite an existing one).
//
// The YAML output of `show` round-trips with the file format, so users
// can pipe `nodeup config show > config.yaml` as a starting point.
func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage nodeup configuration",
		Long: `Manage the nodeup configuration file (~/.nodeup/config.yaml).

The effective config is built from four layers, in increasing precedence:
  1. Built-in defaults
  2. The YAML file on disk
  3. Environment variables (NODEUP_*)
  4. CLI flags

Use the subcommands below to inspect or modify the file layer.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newConfigShowCmd())
	cmd.AddCommand(newConfigGetCmd())
	cmd.AddCommand(newConfigSetCmd())
	cmd.AddCommand(newConfigInitCmd())

	return cmd
}

// newConfigShowCmd prints the merged effective config as YAML. The output
// is intentionally identical in shape to the file format so users can
// edit it, save it back, and get the same result.
func newConfigShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Show the merged effective config as YAML",
		Long: `Print the fully-merged configuration: defaults overlaid with the
config file (if any) overlaid with environment variables.

The output is valid YAML matching the file schema, so you can redirect it
to a file as a starting point for a new config.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWithConfig(cmd, func(cfg *config.Config) error {
				// yaml.v3 marshals struct field tags — the Config struct
				// is annotated so this matches the documented schema.
				out, err := yaml.Marshal(cfg)
				if err != nil {
					return fmt.Errorf("marshal config: %w", err)
				}
				path, _ := config.DefaultPath()
				w := writerFromCmd(cmd)
				w.Println(fmt.Sprintf("# effective config (source: %s)\n%s", path, out))
				return nil
			})
		},
	}
}

// newConfigGetCmd prints a single dotted-key value from the merged config.
// Supported keys mirror config.SetByKey:
//
//	manager, track.lts, track.current, packages.migrate,
//	packages.strategy, packages.skip, cleanup.auto,
//	cleanup.prompt, cache.ttl
func newConfigGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <key>",
		Short: "Get a single config value (e.g. packages.migrate)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWithConfig(cmd, func(cfg *config.Config) error {
				v, err := configGet(cfg, args[0])
				if err != nil {
					return err
				}
				writerFromCmd(cmd).Println(v)
				return nil
			})
		},
	}
}

// newConfigSetCmd updates a key in the config file. It does NOT touch
// env vars or defaults — it edits the file layer specifically, so the
// change persists across invocations.
//
// Refuses to write if the resulting config would be invalid (Validate
// runs after merge).
func newConfigSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a config value and save it to the config file",
		Long: `Persist a config value to the YAML file. Examples:

  nodeup config set manager fnm
  nodeup config set track.lts true
  nodeup config set packages.skip npm,corepack
  nodeup config set cache.ttl 7200`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := config.DefaultPath()
			if err != nil {
				return err
			}

			// Acquire the nodeup lock so two `nodeup config set`
			// (or `config set` + `nodeup upgrade`) invocations can't
			// race on read-modify-write of the config file. See #44.
			configLock, err := platform.AcquireLock()
			if err != nil {
				if errors.Is(err, platform.ErrAlreadyLocked) {
					return fmt.Errorf("refusing to edit config: %w\n  (another nodeup process holds the lock)", err)
				}
				return fmt.Errorf("acquire config lock: %w", err)
			}
			defer func() {
				if rerr := configLock.Release(); rerr != nil {
					writerFromCmd(cmd).Warn(fmt.Sprintf("Warning: failed to release config lock: %v", rerr))
				}
			}()

			// Load the current file (or start from defaults if absent).
			current, _, err := config.Load(path)
			if err != nil {
				// A missing file is fine for `set`; we just create one
				// from defaults.
				if !isNotExist(err) {
					return fmt.Errorf("load existing config: %w", err)
				}
				current = config.Default()
			}

			// Apply the change in-memory using the dotted-key API.
			ok, err := current.SetByKey(args[0], args[1])
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("unknown key %q", args[0])
			}

			// Validate before touching disk so we don't corrupt the
			// file with a bad value.
			if err := current.Validate(); err != nil {
				return fmt.Errorf("refusing to save invalid config: %w", err)
			}

			if err := config.Save(path, current); err != nil {
				return fmt.Errorf("save config to %s: %w", path, err)
			}
			writerFromCmd(cmd).Success(fmt.Sprintf("set %s=%s (saved to %s)", args[0], args[1], path))
			return nil
		},
	}
}

// newConfigInitCmd scaffolds a fresh config file at the default path.
// It refuses to overwrite an existing file — use `set` to mutate
// individual keys, or delete the file first.
func newConfigInitCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Write a scaffolded config file to the default path",
		Long: `Create a new config file containing the documented defaults. Refuses
to overwrite an existing file unless --force is passed.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := config.DefaultPath()
			if err != nil {
				return err
			}

			// Acquire the nodeup lock so two `nodeup config init`
			// invocations can't race on the existence-check /
			// overwrite dance. See #44.
			configLock, err := platform.AcquireLock()
			if err != nil {
				if errors.Is(err, platform.ErrAlreadyLocked) {
					return fmt.Errorf("refusing to init config: %w\n  (another nodeup process holds the lock)", err)
				}
				return fmt.Errorf("acquire config lock: %w", err)
			}
			defer func() {
				if rerr := configLock.Release(); rerr != nil {
					writerFromCmd(cmd).Warn(fmt.Sprintf("Warning: failed to release config lock: %v", rerr))
				}
			}()

			// Existence check is best-effort; we let Save do the real
			// atomic-create dance.
			if !force && fileExists(path) {
				return fmt.Errorf("config already exists at %s (use --force to overwrite)", path)
			}
			cfg := config.Scaffold()
			if err := config.Save(path, cfg); err != nil {
				return fmt.Errorf("save config to %s: %w", path, err)
			}
			writerFromCmd(cmd).Success(fmt.Sprintf("wrote scaffolded config to %s", path))
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing config file")
	return cmd
}

// configGet is the read counterpart to Config.SetByKey. Both accept the
// same dotted-key vocabulary so users only have to learn one set of
// key names.
//
// Returns an error for unknown keys rather than empty string so typos
// surface immediately.
func configGet(c *config.Config, key string) (string, error) {
	switch strings.ToLower(key) {
	case "manager":
		return c.Manager, nil
	case "schema_version", "schemaversion":
		return fmt.Sprintf("%d", c.SchemaVersion), nil
	case "track.lts":
		return boolStr(c.Track.LTS), nil
	case "track.current":
		return boolStr(c.Track.Current), nil
	case "packages.migrate":
		return boolStr(c.Packages.Migrate), nil
	case "packages.strategy":
		return string(c.Packages.Strategy), nil
	case "packages.skip":
		return strings.Join(c.Packages.Skip, ","), nil
	case "cleanup.auto":
		return boolStr(c.Cleanup.Auto), nil
	case "cleanup.prompt":
		return boolStr(c.Cleanup.Prompt), nil
	case "cache.ttl":
		return fmt.Sprintf("%d", c.Cache.TTL), nil
	default:
		return "", fmt.Errorf("unknown key %q (supported: manager, track.lts, track.current, packages.migrate, packages.strategy, packages.skip, cleanup.auto, cleanup.prompt, cache.ttl)", key)
	}
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
