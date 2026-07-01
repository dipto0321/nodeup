# SPEC — nodeup

Distilled from code (README, CLAUDE.md, CONTRIBUTING.md, docs/*, and
`internal/*` source) on 2026-07-02, post-merge of PR #56 (`5ddbe43`).
Caveman encoding per FORMAT.md convention (not present at repo root —
using shapes from the caveman skill).

## §G Goal

Cross-platform Go CLI: auto-detect Node.js version manager, upgrade
LTS/Current Node, migrate global npm packages across versions. Zero
manual steps, single static binary, no Node runtime dependency.

## §C Constraints

- Go 1.24, single static binary, `CGO_ENABLED=0`.
- Core deps only: `cobra`, `Masterminds/semver/v3`, `yaml.v3`. New dep !
  rationale line in PR body.
- ∀ shell exec → `platform.RunShell()` (argv, no shell) | `RunShellScript()`
  (bash -c, nvm only — must `QuotePath` every interpolated value).
- ∀ error return → handled (errcheck enabled in CI).
- ∀ cobra `RunE` → `ctx = cmd.Context()`, ⊥ `context.Background()`
  (contextcheck enabled in CI).
- ∀ path construction → `filepath.Join()`, `os.UserHomeDir()`, ⊥ hardcoded
  `/` or `\`.
- `*_windows.go` → `//go:build windows`; other files compile ∀ 3 OS
  (macOS, Linux, Windows).
- ∀ user-facing output → `internal/ui` (? not yet implemented — currently
  direct `cmd.Printf`/`fmt.Fprintf` throughout `internal/cli`, tolerated
  as pre-`ui`-package debt).
- packages `{npm, corepack, npx}` ⊥ migrate (bundled w/ Node itself).
- Manager resolution order: `--manager` flag → `~/.nodeup/config.yaml` →
  env vars → PATH → well-known dirs.
- Squash-merge only, one logical change per PR, Conventional Commits
  enforced by commitlint.

## §I Interfaces

- cmd: `nodeup upgrade [--lts|--current|--dry-run|--yes|--no-migrate|--no-cleanup|--cleanup|--cleanup-version <v>|--manager <name>]`
- cmd: `nodeup check [--json]`
- cmd: `nodeup list [--json]` (? stub only, ⊥ implemented — see T4)
- cmd: `nodeup packages {snapshot|restore [--from <path>]|diff}`
- cmd: `nodeup config {show|get <key>|set <key> <val>|init [--force]}`
- cmd: `nodeup version`
- api: `GET https://nodejs.org/dist/index.json` → `Manifest ([]ManifestVersion)`
- file: `~/.nodeup/config.yaml` (override: `$NODEUP_CONFIG`; home override: `$NODEUP_HOME`)
- file: `<DataDir>/snapshots/<manager>-<node-version>.json`
- file: `<DataDir>/upgrade-in-progress.json` (sentinel)
- file: `<DataDir>/nodeup.lock` (? built, ⊥ wired — see T3)
- env: `NODEUP_MANAGER`, `NODEUP_TRACK_LTS`, `NODEUP_TRACK_CURRENT`,
  `NODEUP_PACKAGES_MIGRATE`, `NODEUP_PACKAGES_STRATEGY`,
  `NODEUP_CACHE_TTL`, `NODEUP_CONFIG`, `NODEUP_HOME`
- type: `Manager interface` → `Name() string`, `Detect() bool`,
  `Version() (string, error)`, `ListInstalled() ([]semver.Version, error)`,
  `Install/Uninstall/Use/SetDefault(v semver.Version) error`,
  `GlobalNpmPrefix(v semver.Version) (string, error)`,
  `Current() (semver.Version, error)`
- 8 managers implementing `Manager`: fnm, nvm, volta, asdf, mise, n,
  nodenv, nvm-windows

## §V Invariants

- V1: ∀ shell exec → `platform.RunShell()` | `RunShellScript()` (never raw `os/exec`).
- V2: ∀ user-facing string → `internal/ui` (? aspirational, see §C).
- V3: ∀ error return → handled, wrapped w/ `%w` for context.
- V4: ∀ cobra `RunE` → `cmd.Context()`, ⊥ `context.Background()`.
- V5: ∀ path → `filepath.Join()`, ⊥ hardcoded separators.
- V6: packages `{npm, corepack, npx}` ⊥ ever migrated.
- V7: manager resolution: `--manager` flag ! win > config file > env > PATH > well-known dirs.
- V8: config precedence: CLI flags > env vars > config file > defaults.
- V9: config explicit-zero preserved @ ∀ layer (e.g. `NODEUP_TRACK_LTS=false`
  overrides file's `track.lts: true`; `packages.skip ""` clears default list).
- V10: config file write ! atomic (temp file same dir + rename), mode `0600`.
- V11: `config set`/`init` ! validate before write; invalid value ⊥ touch existing file.
- V12: `*_windows.go` → `//go:build windows`; sibling files compile ∀ 3 OS.
- V13: snapshot lookup key = `(manager, node-version)` of the version being
  snapshotted; restore must look up the *same* key it was written under.
- V14: `Manager.Current()` error → caller treats active version as "unknown",
  ⊥ auto-delete it during cleanup (fail closed, not open).
- V15: any string reaching `RunShellScript` (nvm only) that originates from
  an env var (`NVM_DIR`) ! be safe against `$(...)`/backtick expansion —
  currently `QuotePath` does not guarantee this (see known issue #43).
- V16: cleanup: a version ⊥ deleted w/o explicit per-version `y` confirm |
  explicit `--cleanup`/`--cleanup-version`/`cfg.Cleanup.Auto`.
- V17: cleanup candidates = `installed \ {new LTS, new Current, active version}`.
- V18: `--no-cleanup` ! win over every other cleanup flag, including `--yes`.
- V19: multi-version uninstall loop: 1 failure ⊥ abort batch — continue +
  report per-candidate result.
- V20: `semver.Version.String()` output ⊥ ever contain shell metacharacters
  (`$`, backtick, quote, space) — parser is anchored/character-restricted,
  so version strings are safe as shell args by construction.
- V21: lock file (`<DataDir>/nodeup.lock`) ! held around any critical
  section that mutates installed Node versions or the config file
  concurrently (? built but not wired — see T3).

## §T Tasks

id|status|task|cites
T1|.|fix snapshot/restore version-key mismatch in upgrade flow|V13,#42
T2|.|fix NVM_DIR shell-injection surface (QuotePath doesn't escape `$`/backtick)|V15,#43
T3|.|wire AcquireLock/Release into upgrade + config set/init|V21,#44
T4|.|implement `nodeup list` (currently a stub)|I.list,#45
T5|.|fix installPackages abort-on-first-failure; wire or delete MigrationReport|V19,#46
T6|.|fix sentinel lifecycle (deleted on restore failure; never cleared after manual restore)|#47
T7|.|add HTTP timeout/retry/context + atomic cache write to node manifest fetch|#48
T8|.|fix `context.Background()` → `cmd.Context()` in internal/cli/packages.go|V4,#49
T9|.|fix asdf ASDF_DIR vs ASDF_DATA_DIR inconsistency (docs + system_node.go)|V7,#50
T10|.|validate manager-name arg against allowlist in packages restore/diff|V5,#51
T11|.|add context.Context param to Manager interface methods|V4,#53
T12|.|refresh CLAUDE.md known-bugs section + phase-status table|#54
T13|.|fix `--yes` silently overriding `--no-cleanup`|V16,V18,#57
T14|.|fail closed (not open) when Current() errors during cleanup|V14,#58
T15|.|verify/fix `n` manager's reliance on `n current` subcommand|#59
T16|.|fix cleanup docs inaccuracies (CHANGELOG issue ref, nvm-windows note, PATH failure mode, Phase 7 conflict)|#60
T17|.|dead-code cleanup: report.go, FileOverlay(), confirm var, fnm.go error wrap, nvm Current() test gap|#61
T18|.|design + implement internal/ui output layer|V2,#74

## §B Bugs

id|date|cause|fix

(Empty — this distillation captures intended/tested behavior as of
2026-07-02. Known live violations of §V above are tracked as GitHub
issues #42-#61 and mirrored in §T. Run `/ck:backprop` per-issue as each
is fixed to populate this table with `cause → invariant` entries.)
