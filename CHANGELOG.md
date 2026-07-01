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
- System-node classifier (`internal/detector/system_node.go`):
  classifies the `node` binary on PATH into one of `os-package`,
  `snap`, `flatpak`, `homebrew-core`, `manager`, or `unknown`, and
  surfaces a one-paragraph warning when the binary is one nodeup
  cannot (or should not) manage. Wired into `nodeup upgrade`
  (warning to stderr, after manager resolution) and `nodeup check`
  (rendered into the table output and the `--json` envelope).
  Manager-data-dir overrides (e.g. `NVM_DIR=/usr/local/nvm`) take
  precedence so a manager install inside an OS-shaped directory is
  still classified as `manager`. Closes #27.
- Interrupted-upgrade detection and replay: when `nodeup upgrade`
  snapshots packages at the start of a run, it writes a sentinel
  file recording the manager, the pre-upgrade version, and the
  snapshot path. If a subsequent run finds a leftover sentinel, it
  prompts the user to replay the package migration against the
  snapshot (PR #29). `nodeup packages restore` accepts a
  `--from <snapshot-path>` flag for restoring from a non-default
  location, mirroring the sentinel's stored path.
- Cross-platform path handling: `internal/platform.QuotePath` now
  enforces consistent shell-quoting across all `RunShell` callsites,
  so paths containing spaces (e.g. `C:\Users\Dipto Karmakar\...`)
  are passed through unmodified on Windows and double-quoted on POSIX
  shells. The previous behavior leaked the un-quoted path through
  nvm's `RunShell` call on Windows, breaking installs into
  space-containing paths. Closes #25.

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

### Removed
- `scripts/issue-workflow.sh` (the bash issue→branch→PR→squash-merge
  orchestrator) has been replaced by the project-local
  `.claude/skills/issue-workflow/SKILL.md` skill. The skill encodes
  the same workflow as AI-orchestrated `TaskCreate` steps so the
  editor doesn't have to shell out to a 265-line bash script. The
  previous entry in this changelog referencing the bash script is
  intentionally not duplicated here — see the squash-merged commit
  `chore(ci): replace issue-workflow.sh shell script with an AI skill`
  for the full rationale.

## [0.0.0] - 2024-07-01

### Added
- Project blueprint — internal design doc covering language choice, scope, detection engine, version resolution, package migration, architecture, CLI design, edge cases, git workflow, conventional commits, versioning, CI/CD, and distribution. (Superseded by `README.md`; the standalone doc was removed in the Unreleased section.)

[Unreleased]: https://github.com/dipto0321/nodeup/compare/v0.0.0...HEAD
[0.0.0]: https://github.com/dipto0321/nodeup/releases/tag/v0.0.0