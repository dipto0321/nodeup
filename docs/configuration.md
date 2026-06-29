# nodeup Configuration

> _Placeholder doc. Full content lands in Phase 5 alongside the config
> subsystem implementation._

The optional config file lives at `~/.nodeup/config.yaml`.

Resolution precedence (highest first):

1. CLI flags (e.g. `--manager fnm`)
2. Environment variables (`NODEUP_MANAGER`, `NODEUP_TRACK_LTS`, `NODEUP_CACHE_TTL`)
3. Config file
4. Built-in defaults

## Schema

| Key | Type | Default | Description |
|---|---|---|---|
| `manager` | string | _auto-detect_ | Force a specific version manager |
| `track.lts` | bool | `true` | Track LTS upgrades |
| `track.current` | bool | `false` | Track Current upgrades |
| `packages.migrate` | bool | `true` | Migrate global packages on upgrade |
| `packages.strategy` | string | `exact` | `exact` (pin version) or `latest` (re-fetch) |
| `packages.skip` | []string | `[npm, corepack, npx]` | Packages to never migrate |
| `cleanup.auto` | bool | `false` | Auto-remove old versions |
| `cleanup.prompt` | bool | `true` | Ask before removing each old version |
| `cache.ttl` | int | `3600` | Cache TTL in seconds |

See `nodeup.md` §9 for the design rationale.