# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

### Added
- Node.js versions API client (`internal/node`): fetch nodejs.org/dist/index.json with 24h TTL cache, LatestLTS and LatestCurrent resolvers
- `nodeup check` command: displays available LTS and Current versions with optional --json and --offline flags
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