# nodeup Configuration

nodeup's behavior can be tuned in three layers: a YAML config file,
environment variables, and CLI flags. The schema and precedence rules
below are the source of truth for all subsystems.

The optional config file lives at `~/.nodeup/config.yaml` (path
override: `NODEUP_CONFIG`). The file is created and edited via the
`nodeup config {init,show,get,set}` subcommands.

Resolution precedence (highest first):

1. CLI flags (e.g. `--manager fnm`, `--no-migrate`)
2. Environment variables (`NODEUP_MANAGER`, `NODEUP_TRACK_LTS`, `NODEUP_CACHE_TTL`, ...)
3. Config file (`~/.nodeup/config.yaml`)
4. Built-in defaults

Explicit zero values are preserved at every layer — `NODEUP_TRACK_LTS=false`
overrides a file that says `track.lts: true`, and `nodeup config set
packages.skip ""` clears the default package-skip list. To restore a
layer's default, omit the key from the file rather than writing its
zero value.

## Config-file example

```yaml
schema_version: 1
manager: fnm
track:
  lts: true
  current: false
packages:
  migrate: true
  strategy: latest
  skip: [yarn, pnpm]
cleanup:
  auto: false
  prompt: true
cache:
  ttl: 7200
```

The file is written atomically (sibling temp + rename) with mode 0600.
`nodeup config set` validates before writing, so an invalid value
leaves the previous file untouched.

## Subcommands

| Subcommand | Purpose |
|---|---|
| `nodeup config show` | Print the merged (defaults < file < env) config as YAML, with a `# path: ...` header comment |
| `nodeup config get <key>` | Print the merged value of a dotted key (e.g. `packages.strategy`) |
| `nodeup config set <key> <value>` | Update one key in the config file; refuses to write invalid values |
| `nodeup config init [--force]` | Write a fresh config file with defaults; refuses to overwrite without `--force` |

## Schema

| Key | Type | Default | Description |
|---|---|---|---|
| `schema_version` | int | `1` | Schema revision. Reserved for future migrations. |
| `manager` | string | _auto-detect_ | Force a specific version manager |
| `track.lts` | bool | `true` | Track LTS upgrades |
| `track.current` | bool | `false` | Track Current upgrades |
| `packages.migrate` | bool | `true` | Migrate global packages on upgrade |
| `packages.strategy` | string | `exact` | `exact` (pin version) or `latest` (re-fetch) |
| `packages.skip` | []string | `[npm, corepack, npx]` | Packages to never migrate |
| `cleanup.auto` | bool | `false` | Auto-remove old versions |
| `cleanup.prompt` | bool | `true` | Ask before removing each old version |
| `cache.ttl` | int | `3600` | Cache TTL in seconds |