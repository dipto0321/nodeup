# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- YAML config file support (`internal/config`): the documented schema
  from `docs/configuration.md` is now first-class. Settings live in
  `~/.nodeup/config.yaml` (override with `$NODEUP_CONFIG` or redirect
  `$NODEUP_HOME`) and are merged with env vars and built-in defaults
  using a four-layer precedence chain (defaults < file < env < CLI
  flags). Every field — including `track.lts: false` and other
  explicit-zero values — is preserved across round-trips via a per-
  field set-flag overlay so partial files don't clobber defaults.
  Saves are atomic (temp + rename, mode 0600) and refuse to persist
  invalid configs.
- `nodeup config` subcommands:
  - `show` — print the merged effective config as YAML (the output
    round-trips with the file format).
  - `get <key>` — read a single dotted key (e.g. `packages.skip`).
  - `set <key> <value>` — edit a key in the file layer and save it.
    Validates before writing; rejects unknown keys and bad values.
  - `init [--force]` — scaffold a fresh config at the default path;
    refuses to overwrite without `--force`.
- Environment variable overlay (`NODEUP_MANAGER`, `NODEUP_TRACK_LTS`,
  `NODEUP_TRACK_CURRENT`, `NODEUP_PACKAGES_MIGRATE`,
  `NODEUP_PACKAGES_STRATEGY`, `NODEUP_CACHE_TTL`). Parse errors
  include the variable name so env typos surface immediately.
- `nodeup upgrade` now reads its effective config from
  `loadConfigOrDefault()`: `--manager` flag still wins over the file
  value, and `cfg.Packages.Migrate` replaces the hard-coded `true`
  so users can opt out globally.
- `scripts/issue-workflow.sh`: issue→branch→PR→squash-merge automation
  script (bug-fix in the same change: fixed the bash regex parser
  that was rejecting valid issue titles because space-separated
  alternations aren't valid ERE).

### Changed
- Consolidated the internal `nodeup.md` design doc into `README.md`
  (new "Compatibility notes" subsection, expanded Contributing
  conventions) and stripped the now-dangling `nodeup.md` references
  from source, config, docs, and issue templates. The file is removed.
- Dropped the stale "Placeholder doc" banners from
  `docs/managers.md`, `docs/configuration.md`, `docs/installation.md`,
  and `docs/release-checklist.md` and replaced them with concrete
  pointers to the source files or workflows that govern each doc's
  subject. No content removed; only stale phase references (Phase 5 /
  7 / 8) were rewritten to reflect actual state — Phase 1 (8/8
  managers detected) is now flagged as ✅ in the README's
  `Project status` table.
- `internal/node`: replaced the `(LTS bool, TS string)` pair on
  `ManifestVersion` with a single `LTSCodename *string` so the
  nodejs.org `lts` JSON union (`false` for Current, codename string
  for LTS) decodes cleanly via a custom `UnmarshalJSON`. The previous
  shape silently dropped the `TS` codename whenever `lts` was the JSON
  literal `false`, so `LatestLTS`/`LatestCurrent` could return the
  wrong row. Now both paths share one field and one decoder.

### Added
- Node.js versions API client (`internal/node`): fetch nodejs.org/dist/index.json with 24h TTL cache, LatestLTS and LatestCurrent resolvers
- `nodeup check` command: displays available LTS and Current versions with optional --json and --offline flags
- `nodeup upgrade` command (end-to-end): detect manager → resolve target
  LTS/Current → compute install plan (with `--dry-run`) → snapshot
  installed globals → install new versions → set default → restore
  packages. Supports `--lts` / `--current` to restrict, `--manager` to
  override detection, `--no-migrate` to skip package migration, and
  `--offline` to use the cached manifest.
- Package snapshot/restore (`internal/packages`): capture and restore global npm packages across Node versions
- Migration report: per-package result tracking with ok/failed/skipped status
- `nodeup packages snapshot`: snapshot the active version's global npm packages
- `nodeup packages list`: list all saved snapshots
- `nodeup packages restore <manager> <version>`: restore packages from a snapshot
- `nodeup packages diff <manager> <v1> <v2>`: compare two snapshots
- Initial project scaffolding (`chore: initial project scaffolding`)
- Cobra-based CLI with `upgrade`, `check`, `list`, `packages`, `config`, `version` subcommands
- Manager interface (`internal/detector`) covering `fnm`, `nvm`, `Volta`, `asdf`, `mise`, `n`, `nodenv`, and (Windows) `nvm-windows`
- Cross-platform helpers (`internal/platform`): data dir resolution, shell execution, concurrency lock
- GitHub Actions CI: golangci-lint, commitlint, cross-OS tests, cross-arch build matrix
- Release workflow: GoReleaser v2 → GitHub Release + Homebrew tap + Scoop bucket
- Conventional Commits enforcement via commitlint
- golangci-lint config (`errcheck`, `staticcheck`, `gocritic`, etc.)
- Makefile with `build`, `test`, `lint`, `fmt`, `ci`, `release-snap`, `release` targets
- GoReleaser config: 6 platform archives, SHA256, Homebrew tap, Scoop bucket

## [0.0.0] - 2024-07-01

### Added
- Project blueprint — internal design doc covering language choice, scope, detection engine, version resolution, package migration, architecture, CLI design, edge cases, git workflow, conventional commits, versioning, CI/CD, and distribution. (Superseded by `README.md`; the standalone doc was removed in the Unreleased section.)

[Unreleased]: https://github.com/dipto0321/nodeup/compare/v0.0.0...HEAD
[0.0.0]: https://github.com/dipto0321/nodeup/releases/tag/v0.0.0