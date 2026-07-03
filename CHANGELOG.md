# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Post-upgrade cleanup prompt (`nodeup upgrade`): after a successful
  upgrade, nodeup asks whether to delete the old Node.js versions
  left behind. The prompt offers three options — `y` deletes every
  candidate, typing a specific version (e.g. `20.18.0`) deletes only
  that one, and `N` (or empty enter) skips. Candidates are computed
  as `installed \ {new LTS, new Current, currently active}`, where
  the active version is detected via `<manager> current` (or per-
  manager equivalent). The new `Manager.Current()` interface method
  backs the exclusion — nvm-windows returns the
  `ErrNVMWindowsNotImplemented` sentinel and the exclusion is
  skipped. New flags: `--cleanup` (auto-confirm), `--cleanup-version
  <v>` (repeatable, picks specific versions), plus the existing
  `--no-cleanup` to skip the prompt entirely. Config equivalents
  (`cleanup.auto`, `cleanup.prompt`) ship under the same names; see
  `docs/managers.md#post-upgrade-cleanup` for the full precedence
  table. Closes #41.
- Native mutation commands for all 7 working managers (fnm, nvm,
  Volta, asdf, mise, n, nodenv): `Install`, `Uninstall`, `Use`,
  `SetDefault`, and `GlobalNpmPrefix` are now real shell-outs rather
  than stubs returning `ErrXxxNotImplemented`. Volta's `SetDefault`
  and n's `SetDefault` are intentional no-ops (those managers pin
  per-project, not per-machine). `GlobalNpmPrefix` returns the
  per-version npm global modules directory for each manager's on-
  disk layout, which the migration step needs to enumerate packages.
  nvm-windows remains unsupported (no install/uninstall CLI on that
  platform); its stubs still return
  `ErrNVMWindowsNotImplemented` so callers can detect and skip.
  Per-manager cleanup behavior is documented in
  `docs/managers.md#post-upgrade-cleanup`. Closes #40.

### Added (continued)
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
  prints a hint to stderr naming the snapshot path and the exact
  `nodeup packages restore --from <path>` invocation the user can
  copy to replay the migration (PR #29). `nodeup packages restore`
  accepts a `--from <snapshot-path>` flag for restoring from a
  non-default location, mirroring the sentinel's stored path.
- `internal/node`: manifest fetch from nodejs.org gained an HTTP
  timeout, context threading, retry-with-backoff for transient
  failures, and atomic cache writes. Previously `FetchManifest`
  called `http.Get(url)` against `http.DefaultClient`, which has no
  timeout — a hung nodejs.org (or a corporate proxy that drops the
  connection silently) could block `nodeup upgrade` / `nodeup check`
  forever, with no way out except Ctrl-C (and even Ctrl-C had no
  effect since no context was threaded through). The new
  `FetchManifestCtx(ctx)` (the existing `FetchManifest()` is kept
  as a `context.Background()`-using wrapper) builds the request via
  `http.NewRequestWithContext`, uses a package-level
  `http.Client{Timeout: 30s}`, and retries up to 3 times with
  exponential backoff (200ms, 400ms, 800ms, capped at 2s) for
  network errors and 5xx / 408 / 429 responses. Permanent 4xx errors
  are not retried. `saveToCache` now writes the data and meta files
  via temp-file + rename so two concurrent `nodeup` invocations
  refreshing the cache cannot leave a mismatched state. The cache
  helpers (`loadFromCacheAt`, `saveToCacheAt`) take explicit
  `cachePaths` so the new tests can drive them hermetically. Both
  CLI callers (`upgrade.go`, `check.go`) now pass
  `cmd.Context()` through. Closes #48.
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

### Fixed
- `nodeup-npm/package.json`: declare `tar` as an explicit
  runtime dependency. Pre-fix, `nodeup-npm/scripts/install.js`
  did `const tar = require('tar')` at module load time with a
  comment claiming "npm bundles tar; no extra dep" — that
  comment was incorrect. npm uses tar internally, but it is
  not guaranteed to be reachable from an installed package's
  script context: yarn/pnpm do not hoist npm's internals, and
  npm's modern flat-install behavior is incidental. Without
  the explicit dep, `require('tar')` failed immediately on
  every yarn / pnpm install with
  `Error: Cannot find module 'tar'` mid-`postinstall`, on
  every non-Windows platform. yarn/pnpm users were completely
  locked out of the npm install path
  (`docs/installation.md` lists yarn / pnpm alongside npm as
  supported install methods). Pinned to `^6.2.1` to match the
  package's `engines.node: ">=14"` floor (v7 requires Node 16+).
  The misleading comment in install.js is replaced with an
  explicit pointer to this issue + a note that the dependency
  is now declared. Also dropped the unused `zlib` import the
  pre-fix file carried (lint unused statics had not caught it
  because it's not a Go file). The transitive `npm audit`
  warning that surfaces against any unpinned `tar@^6.x` (re:
  #65 / node-tar GHSA advisories) is deferred to issue #65 —
  the v6→v7 bump also forces our `engines.node` floor up to
  16, which is a separate decision. Closes #63.
- `.golangci.yml` / `Makefile` / `.github/workflows/ci.yml`:
  migrated to the golangci-lint **v2** schema. The pre-fix file
  targeted the v1 schema (no `version:` field, flat
  `linters.enable`, `output.formats` as a slice), but the
  Makefile told contributors to `brew install golangci-lint` —
  which installs the current major version, v2.x, today. Any
  contributor with a current Homebrew got
  `Error: can't load config: ... 'output.formats' expected a
  map, got 'slice'` instead of a lint failure, blocking
  `make lint` and `make ci` end-to-end. CI was unaffected
  because `golangci-lint-action@v6` pinned to `version:
  v1.64.8`, but the local dev workflow documented in the
  Makefile was effectively broken. We migrated with
  `golangci-lint migrate` (preserving the original
  exclusions/presets/rationale in hand-written comments),
  bumped CI to `golangci/lint-action@v8` pinned to
  `v2.12.2` (matching what Homebrew ships today), and updated
  the Makefile `lint` target to refuse to run when
  golangci-lint isn't installed and point contributors at
  the exact `go install` command matching the CI pin. The
  same lint set + exclusion rules + presets that v1 ran
  with still apply (`bodyclose`, `contextcheck`, `errcheck`,
  `gocritic`, `govet`, `ineffassign`, `misspell`,
  `staticcheck`, `unused`; `gosimple` is folded into
  `staticcheck` in v2; formatters `gofmt` + `goimports`
  with `local-prefixes: github.com/dipto0321/nodeup`).
  Confirmed locally: `make ci` is now green on a Homebrew
  v2.12.2 install. Closes #62.
- `internal/detector/n.go`: `N.Current()` no longer shells out
  to `n current` — an undocumented subcommand that upstream
  `tj/n` resolves as a label equivalent to "latest" and
  side-effects a download of the newest available Node.js
  version. Pre-fix, every `Current()` invocation would have
  silently mutated the user's machine by running the
  catch-all arm `install "$1"` for "current", which fetches
  the latest semver from nodejs.org, downloads the tarball,
  extracts it, and activates it. Since callers treat
  `Current()` errors as "active version unknown, don't
  exclude it" (the safe-by-convention default per the
  `Manager` interface doc), every n-using machine ran the
  cleanup step without active-version exclusion on every
  invocation — and would now also have the latest version
  installed behind the user's back. The replacement reads
  the active version off `$N_PREFIX/bin/node --version`
  instead: n's `activate` function copies the active
  node binary into that path, so its `--version` is the
  authoritative source for "what's active". Side-effect-free,
  matches every supported n install, and the parser
  (`parseNNodeVersion`) handles both `vX.Y.Z` and bare
  `X.Y.Z` shapes. Doc comments at the top of `n.go` and
  the mutation-methods block both spell out the "do not
  use `n current`" reasoning. New tests in `n_test.go` pin
  the new path (`TestN_Current_InvokesNodeVersionNotNCurrent`
  explicitly fails if the literal `n current` request re-
  appears), parser cases (`TestParseNNodeVersion_*`), and
  error propagation (`TestN_Current_PropagatesRunShellError`,
  `TestN_Current_PropagatesParseError`). The pre-fix tests
  (`TestParseNCurrent_*`, `TestNCurrent_InvokesShell`) are
  deleted — they codified the buggy behavior. Closes #59.
- `internal/cli` (`upgrade.go`): `Manager.Current()` failure no
  longer leaves the active Node.js version unprotected from
  cleanup deletion. Pre-fix, the post-upgrade cleanup step
  silently swallowed `Current()` errors and proceeded with the
  zero `semver.Version` for "active" — which `cleanupCandidates`
  treats as "no active version to exclude" (by design, to avoid
  the zero value matching a real version). The practical effect
  was: a `Current()` failure (e.g. `nvm` after `nvm deactivate`,
  or any transient `node -p` parse hiccup) turned the version
  powering the user's shell into an ordinary cleanup candidate,
  and a `--cleanup` / `--yes` / `cfg.Cleanup.Auto` invocation
  would auto-delete it. We now fail closed: `currentErr != nil`
  sets a new `cleanupConfig.ForcePerVersion` flag (downgrading
  `AutoDeleteAll` to `false` and forcing `PerVersion=true` so
  nothing gets auto-deleted), and surfaces a stderr warning
  explaining why the per-version prompt is firing. The
  `NonInteractive` short-circuit still wins over `ForcePerVersion`
  (skipping cleanup is a stronger fail-closed stance than
  per-version prompting), and the active-version exclusion logic
  in `cleanupCandidates` is intentionally unchanged. New tests
  in `cleanup_test.go` pin the downgrade behavior end-to-end
  (`AutoDeleteAll=true → ForcePerVersion=true` requires per-
  version y/N, `PerVersion=false → ForcePerVersion=true` still
  requires per-version y/N, and `NonInteractive=true +
  ForcePerVersion=true` still skips entirely). Closes #58.
- `internal/cli` (`upgrade.go`): `--yes --no-cleanup` no longer
  silently auto-deletes every eligible old Node.js version. The
  `if yes { ... }` block at upgrade.go:107-111 ran unconditionally
  AFTER the `switch` that was supposed to make `--no-cleanup` win,
  so it flipped `NonInteractive` back to `false` and set
  `AutoDeleteAll=true`, regardless of `noCleanup`. A CI script
  that habitually passes `-y`/`--yes` (the documented
  `--no-cleanup` contract is "never prompt, never delete") would
  therefore lose Node installs it never asked to remove. The
  cleanup-toggle resolution is now factored into a pure helper
  `resolveCleanupConfig(noCleanup, autoCleanup, yes, versions, cfg)`
  in `cleanup.go` so it's testable in isolation; the `--yes`
  block is guarded by `&& !noCleanup`, restoring `--no-cleanup`'s
  precedence. New tests in `cleanup_config_test.go` pin the
  precedence table (including the negative-path matrix where
  `--no-cleanup` beats every other knob). Closes #57.
- `internal/cli/packages.go` (`runRestore` and `runDiff`): the
  `<manager>` positional argument is now validated against a
  canonical allowlist (`detector.IsAllowedManagerName`) before any
  filesystem path is constructed. Pre-fix, the manager name was
  interpolated verbatim into a snapshot filename
  (`fmt.Sprintf("%s-%s.json", managerName, version)`) and the
  result was `filepath.Join`'d into the snapshots dir — and
  `filepath.Join` collapses `..` segments, so a manager name like
  `../../tmp/evil` resolved outside the snapshots directory. An
  attacker with a local file-placement primitive (a shared temp
  directory, a cloned repo containing a payload filename like
  `<prefix>../../tmp/evil-1.0.0.json`) could have the resulting
  snapshot's `Packages` list piped straight into
  `npm install -g <name>@<version>` with no validation. The
  allowlist is built from `detector.AllowedManagerNames()` (a new
  helper that derives the list from `detector.All()`), so the
  set stays in sync with the per-platform build files; the match
  is byte-for-byte case-sensitive (matching the `--manager` flag),
  and the error message surfaces the offender and the allowlist
  so typos remain user-fixable. `internal/detector/manager_names.go`
  (new file) holds the helpers; `manager_names_test.go` pins
  AllowedManagerNames ↔ All() parity and IsAllowedManagerName
  behaviour (incl. traversal payloads, case-fold negativity, and
  near-miss strings). Closes #51.
- `internal/cli`: upgrade loop's restore step was looking up its
  snapshot by the **new** installed Node version
  (`packages.Restore(ctx, mgr, *v)` where `v` came from `toInstall`),
  but the upgrade flow only writes snapshots for the **previously**
  installed versions. The restore then read a non-existent
  `<DataDir>/snapshots/<mgr>-<newVersion>.json`, hit
  `read snapshot: open …: no such file or directory`, and the loop's
  blanket `cmd.Printf("Warning: restore failed: …")` swallowed the
  error — silently no-op'ing the package migration for every newly
  installed Node. The restore step now resolves the snapshot path the
  same way the sentinel does (latest-installed-version key) and
  replays via `packages.RestoreFromSnapshot(ctx, snapshotPath)`,
  carrying the user's globals forward to the new default exactly
  once. Added a regression test pinning the
  `RestoreFromSnapshot`-reads-source-path contract. Closes #42.
- `internal/platform/shell.go` (`QuotePath`): on POSIX the quoting
  rule for paths embedded in `bash -c` scripts now uses **single
  quotes** with `'\''` to escape embedded `'` bytes. The previous
  rule used double quotes and only escaped `"` and `\\`, so a path
  containing `$USER`, `$(...)`, or backticks was still subject to
  shell expansion. `NVM.Version()` builds a `source <path> && nvm
  --version` script using whatever `NVM_DIR` (or `~/.nvm`) provides,
  so setting `NVM_DIR='/tmp/$(touch /tmp/PWNED)'` would expand
  inside bash's double quotes before reaching nvm. Single-quote
  wrapping disables bash variable expansion, command substitution,
  and backtick substitution entirely. The Windows branch
  (cmd.exe-unaware double-quote wrapping for paths-with-spaces) is
  unchanged. The test table for `QuotePath` now expects single quotes
  on POSIX, plus a dedicated `TestQuotePathNeutralizesShellInjection`
  regression test, plus a `TestNVM_Version_NVMDirShellInjectionIsNeutralized`
  detector-level test that points `NVM_DIR` at a payload string and
  asserts no unquoted `$(touch` occurrence in the emitted script.
  Closes #43.
- `internal/cli`: the `platform.AcquireLock`/`Release` infrastructure
  (flock on POSIX, `LockFileEx` on Windows, stale-PID detection)
  existed but had zero call sites anywhere in the CLI, despite the
  README and `package` doc comments claiming concurrent runs are
  blocked. Two parallel `nodeup upgrade` invocations could snapshot,
  install, and migrate against the same data dir with no guard
  between them; two parallel `nodeup config set` invocations raced
  on read-modify-write of the YAML file (each Save is atomic on its
  own, but the second writer's atomic rename silently clobbers the
  first writer's in-memory changes — a lost update). `runUpgrade`,
  `config set`, and `config init` now `defer platform.AcquireLock()`
  around their critical sections and surface `ErrAlreadyLocked`
  with a "another nodeup process holds the lock" hint instead of
  silently racing. Dry-run is read-only and intentionally does NOT
  acquire the lock (it returns before any mutation). The README's
  stale lock-path documentation (`~/.nodeup/nodeup.lock`) is fixed
  to point at the actual `platform.LockPath()` resolution
  (`<DataDir>/nodeup.lock` per OS — Linux XDG, macOS
  `~/Library/Application Support/nodeup/nodeup.lock`, Windows
  `%APPDATA%\nodeup\nodeup.lock`). Closes #44.
- `internal/cli`: the upgrade-in-progress sentinel had a broken
  lifecycle in both directions. On the `nodeup upgrade` side, the
  deferred `RemoveSentinel()` fired unconditionally as long as the
  sentinel had been armed — even when the post-install package
  restore had failed and only logged a warning. That left the
  user with installed Node versions, unmigrated global packages,
  AND the "resume breadcrumb" silently deleted: there was no way to
  recover through the documented `--from <sentinel path>` path. We
  now track `restoreSucceeded` alongside the existing
  `sentinelArmed` and only remove the sentinel when both are true.
  On the manual `nodeup packages restore` side — the exact command
  `PersistentPreRunE` tells the user to run after an interrupted
  upgrade — a successful restore now calls `packages.RemoveSentinel()`
  so subsequent `nodeup` invocations stop printing the
  "interrupted upgrade" warning forever. Removal failures there are
  logged, not returned: the user's primary goal (restored packages)
  has been achieved and a stale sentinel is cosmetic. Also corrects
  the CHANGELOG claim that "next run prompts the user to replay" —
  the implementation only prints a two-line stderr hint
  (`To resume: nodeup packages restore --from <path>`); no
  interactive prompt is shown. Closes #46.
- `nodeup list` was a stub that printed "not yet implemented (Phase
  1)". It now does the obvious thing — resolve every detected
  manager, call each one's `ListInstalled()`, and render the union
  either as a JSON envelope (`--json`) or a one-line-per-manager
  table. Versions are sorted ascending per manager (semver, so
  pre-release tags order correctly); the currently active version
  (first manager that answers `Current()`) is surfaced alongside
  the listing in the JSON envelope and as a trailing line in the
  table. The new `--manager` flag narrows the listing to a single
  manager (case-insensitive), matching the `--manager` flag on
  `nodeup upgrade` and `nodeup check`. Errors from a single
  manager's `ListInstalled` are captured in the JSON envelope's
  per-entry `error` field rather than aborting the whole listing,
  mirroring the soft-fail policy of `nodeup check`. Adds a
  dedicated `internal/cli/list_test.go` covering the JSON envelope
  (omits-on-nil current/error), the table renderer (empty state,
  mixed empty/error/success), and the `--manager` resolution
  helper. Closes #45.
- `internal/cli/packages.go`: `runSnapshot` and `runRestore` now
  pass `cmd.Context()` to `packages.Snapshot` / `packages.Restore`
  (and to the `--from` branch via the same `ctx` local) instead of
  `context.Background()`. Both are cobra `RunE` functions with a
  live context available — using `context.Background()` meant
  Ctrl-C during `nodeup packages snapshot` or `nodeup packages
  restore` could not cancel the in-flight `npm ls -g` / `npm
  install -g` subprocess, so the child process kept running after
  the user aborted. The fix is the same contextcheck violation
  `internal/cli/upgrade.go` and `check.go` already addressed as
  part of #48 — `packages.go` was the last `RunE` file in the tree
  still using `context.Background()`. The unused `"context"`
  import is dropped. Closes #49.
- `internal/detector/system_node.go`: the asdf branch of
  `managerManagedRoots` was reading `$ASDF_DIR` to discover the
  manager's data dir, while `asdfDataDir()` (the function the
  detector's actual `Install`/`ListInstalled` paths shell out
  against) reads `$ASDF_DATA_DIR` — and `docs/managers.md`
  documented `$ASDF_DIR` in the supported-manager table. The three
  were inconsistent: a user with `ASDF_DIR=/path/to/asdf-source`
  (the source checkout from git-clone) would have `nodeup` classify
  the `node` binary under that source dir as `manager`-managed even
  though the manager's actual install location was elsewhere
  (typically `~/.asdf`), and a user with the canonical `ASDF_DATA_DIR`
  override would have it silently ignored by the path classifier.
  Unified all three on `$ASDF_DATA_DIR`, matching the variable
  `asdfDataDir()` already used and the official asdf docs.
  Updated `system_node_test.go`'s "asdf env wins" case to drive
  the new variable. Closes #50.
- `internal/cli/packages.go` (`runRestore` and `runDiff`): the
  `<manager>` positional argument is now validated against a
  canonical allowlist (`detector.IsAllowedManagerName`) before any
  filesystem path is constructed. Pre-fix, the manager name was
  interpolated verbatim into a snapshot filename
  (`fmt.Sprintf("%s-%s.json", managerName, version)`) and the
  result was `filepath.Join`'d into the snapshots dir — and
  `filepath.Join` collapses `..` segments, so a manager name like
  `../../tmp/evil` resolved outside the snapshots directory. An
  attacker with a local file-placement primitive (a shared temp
  directory, a cloned repo containing a payload filename like
  `<prefix>../../tmp/evil-1.0.0.json`) could have the resulting
  snapshot's `Packages` list piped straight into
  `npm install -g <name>@<version>` with no validation. The
  allowlist is built from `detector.AllowedManagerNames()` (a new
  helper that derives the list from `detector.All()`), so the
  set stays in sync with the per-platform build files; the match
  is byte-for-byte case-sensitive (matching the `--manager` flag),
  and the error message surfaces the offender and the allowlist
  so typos remain user-fixable. `internal/detector/manager_names.go`
  (new file) holds the helpers; `manager_names_test.go` pins
  AllowedManagerNames ↔ All() parity and IsAllowedManagerName
  behaviour (incl. traversal payloads, case-fold negativity, and
  near-miss strings). Closes #51.
- `nodeup-npm/scripts/install.js`: verify the downloaded binary
  archive against the SHA256 published in the release's
  `checksums.txt` before extracting. Pre-fix, the install script
  trusted whatever bytes came back from `github.com` /
  `objects.githubusercontent.com` and wrote them straight to
  `./bin/nodeup` — a TLS-stripping proxy, compromised CDN, or
  manipulated DNS could swap in an attacker-controlled binary
  and the user would have no way to know. Two independent fixes
  in this change:
  - **Redirect allowlist** (`followHops`): the one-hop redirect
    from `github.com` to `objects.githubusercontent.com` is now
    validated against `ALLOWED_REDIRECT_HOSTS` (`Set(['objects.
    githubusercontent.com'])`). Any redirect to a non-allowlisted
    host aborts the install with the offending hostname named.
    `MAX_REDIRECT_HOPS = 2` (the known GitHub chain length)
    bounds the walker; longer chains throw before any byte is
    written to disk.
  - **SHA256 verification**: `fetchExpectedHash(repo, tag,
    archiveName)` downloads `checksums.txt` first (fail fast —
    no point pulling a multi-MB archive we can't verify).
    `downloadTo(url, destPath)` then streams the archive to disk
    while piping it through `crypto.createHash('sha256')`, so
    the hash is computed as the bytes arrive (no second pass
    over the file). On mismatch, the half-saved archive is
    unlinked before the script dies — a forensic investigation
    of the failing install doesn't leave the
    (potentially attacker-controlled) bytes sitting in tmp.
  - `parseChecksumsTxt` accepts both GoReleaser's `<hash>  <filename>`
    (two-space) shape and the `sha256sum -b` / `--tag` `<hash>
    *<filename>` (binary-mode `*`) shape, with or without space
    between the `*` and the filename. Comment lines and blank
    lines are skipped; malformed lines are silently dropped.
  - `nodeup-npm/scripts/install_test.js` (new file): 12 pure-
    helper smoke tests covering `parseChecksumsTxt` (two-space,
    binary-mode-`*`, malformed-line skipping, basename keying),
    `followHops` (allow-listed redirect, off-CDN redirect
    rejection, hop-count ceiling, missing-Location-header
    rejection, unexpected-status rejection), and the end-to-end
    integrity path (missing-archive-in-checksums error names
    both the offender and the available set, hash mismatch is
    detected, streaming hash equals direct hash). Tests run
    via `node nodeup-npm/scripts/install_test.js` and exit 0
    on green; on failure they print the offending case and exit
    1. No JS test framework is introduced (the project already
    has a 1-line `require('tar')` check that didn't justify
    pulling in jest/mocha/vitest either; see #63). Closes #64.
- `nodeup-npm/scripts/check.js`: replace the per-axis platform /
  arch lookup with a single combined-key `Set` of (Node.js
  `process.platform` / `process.arch`) pairs that mirrors the
  GoReleaser build matrix. Pre-fix, the script checked
  `PLATFORM_TO_OS[platform]` and `ARCH_TO_GOARCH[arch]`
  independently — so `process.platform === 'win32'` plus
  `process.arch === 'arm64'` mapped to `(windows, arm64)` and
  both lookup tables returned non-null, the preinstall check
  passed, and `install.js` then 404'd trying to download a
  release archive that was never built (`.goreleaser.yaml`
  line 45-47 explicitly excludes `goos: windows, goarch: arm64`
  from the build matrix). The new lookup is one map keyed on
  the actual (platform, arch) pair: `linux/{amd64,arm64}`,
  `darwin/{amd64,arm64}`, `win32/amd64`. `win32/arm64` is
  intentionally absent so the preinstall check matches the
  release artifact set 1:1. Tests in `check_test.js` pin the
  SUPPORTED set contents and pin the rejection of the
  previously-buggy `win32/arm64` (and the partial-match /
  sibling-prefix / trailing-slash variants that an env-tampering
  user might try). Closes #65 (point 2).
- `nodeup-npm/scripts/install.js`: belt-and-suspenders zip-slip
  / tar-slip guards on archive extraction, plus a partial-state
  cleanup at the start of the install. Pre-fix, the install
  path trusted whatever entries the tar / zip extractor wrote —
  the `tar.x()` call has no `filter` argument, and the
  Windows-zip path shells out to `Expand-Archive` / `unzip -o`,
  neither of which validates that extracted entry paths stay
  inside the temp dir. The actual attack surface is bounded by
  the SHA256 verification added in #64 (a hostile archive would
  have to defeat that first), but a defensive check costs
  nothing and removes the dependency entirely:
  - `isPathInside(parent, child)` resolves both paths via
    `path.resolve` (sibling-prefix-safe — `/tmp/ab` is NOT
    inside `/tmp/a`) and uses `path.relative` to detect
    `..` traversal and absolute-path escapes.
  - `extractTarGz` now passes a `filter` callback that runs
    `isPathInside(outDir, entryPath)` for every entry and
    throws with the offending entry + resolved path if it
    escapes. tar 6.x already refuses `..`-containing entries
    by default (`node_modules/tar/lib/unpack.js:277-286`), so
    this is defense in depth — if a future regression flips
    the tar option, the install still refuses.
  - `extractZip` now goes through `safeExtractZip`, which
    lists the archive's entries first (`unzip -Z1` on POSIX,
    `[System.IO.Compression.ZipFile]::OpenRead(...).Entries`
    on Windows), validates each entry's resolved path against
    `outDir`, and only then runs the actual extraction. This
    is the real fix: `unzip -o` and `Expand-Archive` will
    both happily extract a `../escape.txt` if the archive
    contains one. tar doesn't have this problem; only the
    zip path does.
  - `main()` now `fs.rmSync(binaryDest, { force: true })` at
    the start of the install, before any download. Pre-fix,
    a late-stage failure (chmod throwing after renameSync
    succeeded, an OOM mid-extraction) left a non-executable
    `bin/nodeup` on disk; a retry's `renameSync` then either
    errored with EEXIST or silently overwrote a half-broken
    file. Cleaning up at the start makes the install path
    effectively transactional.
  - Tests in `install_test.js` cover `isPathInside` (inside /
    equals / parent / sibling-prefix / `..` traversal /
    absolute-outside / trailing-slash-normalisation) and the
    end-to-end filter callback (the good entry passes, the
    slip entry throws, the absolute-outside entry throws).
    No JS test framework introduced; same reasoning as #63.
    Closes #65 (points 1 and 3).
- `.github/workflows/release.yml`: bump the GoReleaser job's
  `go-version` pin from `'1.22'` to `'1.24'` to match
  `go.mod`'s `go 1.24.0` directive and `.github/workflows/ci.yml`'s
  lint job (the test jobs in ci.yml use `go-version-file: 'go.mod'`
  which auto-derives, so we have to keep the explicit pin here in
  sync by hand). Pre-fix, the comment at release.yml:6 claiming
  "same version pin as CI" was misleading — the divergence was
  masked by Go's default `GOTOOLCHAIN=auto` (which auto-downloads
  the missing toolchain), but that meant the release build
  silently depended on network access to proxy.golang.org during
  the release job — a step CI never exercises, since CI already
  has 1.24. If `GOTOOLCHAIN=local` is ever set (org policy,
  air-gapped runner), the release build would break outright at
  tag-push time, the one moment there's no opportunity to catch
  it in a normal PR-gated CI run. A multi-line inline comment
  next to the pin explains the parity-with-go.mod invariant so
  future bumps in either direction are caught in code review.
  Closes #66.
- `Makefile`: inject `-X main.version=... -X main.commit=...
  -X main.date=...` ldflags into the `build` and `install`
  targets so a locally-built binary's `nodeup version` output
  reports the actual git commit and build timestamp instead of
  the package-level defaults (`dev` / `none` / `unknown` per
  `cmd/nodeup/main.go:20,23,26`). Pre-fix, every `make build` and
  every `go install .../cmd/nodeup` (or `@latest`) shipped with
  the defaults — defeating the install-verification flow
  documented in `docs/installation.md#verifying` ("Should print a
  version, git commit, build date, and Go runtime info") and
  undermining the bug-report triage instructions in
  `CONTRIBUTING.md` that ask reporters to paste `nodeup version`
  output (a locally-built binary was indistinguishable from any
  other local build, so triage couldn't tell if the bug reproed
  on the latest commit or on a stale checkout). Three new
  Makefile vars capture the values at parse time:
  - `VERSION` from `git describe --tags --always --dirty` —
    closest semver tag (when the repo has tags) or short SHA +
    `-dirty` if the working tree is dirty; falls back to `dev`
    when git is unavailable (snapshot tarball build).
  - `COMMIT` from `git rev-parse --short HEAD` — falls back to
    `none` if git is unavailable.
  - `DATE` from `date -u +%Y-%m-%dT%H:%M:%SZ` — RFC3339 UTC,
    matches `.goreleaser.yaml:37`'s `{{.CommitDate}}` shape.
  Each shell call is wrapped with `|| echo <fallback>` so a build
  inside a snapshot tarball or vendored copy without `.git/`
  falls back to safe defaults rather than crashing the build.
  Combined into a single `LDFLAGS` variable so both targets share
  one source of truth, and the build target now echoes
  `(version=…, commit=…, date=…)` after a successful build so
  the operator can confirm the injected values without running
  the binary. `internal/cli/version_test.go` (new, 5 tests)
  pins the exact output shape of `nodeup version` (line-by-line
  format, multi-line output, `go1.X.Y` runtime pattern,
  `<goos>/<goarch>` platform pattern, and `--check` flag is a
  no-op not an error) so a future refactor can't silently drop
  a field. Closes #68.
- `internal/cli/cleanup.go`: confirmation at the post-upgrade
  cleanup's all-or-nothing prompt is now sticky — answering
  `y` (delete all) or a specific version at the "What would you
  like to do?" prompt no longer triggers a second, separate
  per-version `Delete vX? [y/N]` loop on top of the explicit
  confirmation. Pre-fix, the per-version loop fired unconditionally
  for every candidate in `toOffer`, because `cfg.PerVersion`
  defaults to `true` from `cfg.Cleanup.Prompt`'s config default —
  so a user who answered `y` once at the all-or-nothing prompt
  saw nothing deleted if their terminal session's input stream
  ended before they re-answered for every candidate (the
  `default:` branch of `promptPerVersion` returns `false`, and
  empty / non-`y` input landed each version in `result.Skipped`
  with no visible error). User reported live: answered `y` at
  the cleanup prompt during a real `nodeup upgrade`, then
  `fnm list` afterward still showed the old versions — no
  deletion occurred, with no surface indication of why.
  Fix: set `cfg.PerVersion = false` for the per-version loop
  once a higher-level confirmation has been recorded. Three
  classes of higher-level confirmation trigger this:
  1. **`decision.deleteAll`** — `y` / `yes` at the all-or-nothing
     prompt.
  2. **`decision.deleteOne`** — a specific version typed at the
     all-or-nothing prompt (the user's choice IS the per-version
     confirmation).
  3. **`AutoDeleteAll` (without `ForcePerVersion`)** — set by
     `--cleanup`, `--yes`, or `cfg.Cleanup.Auto`. The user's
     pre-flight opt-in counts; the per-version prompt is redundant.
  The `ForcePerVersion` downgrade from #58 is the only path that
  keeps `cfg.PerVersion = true` after Step 1b, because it
  represents an inability to safely exclude the active version
  — see `TestCleanupPrompt_ForcePerVersionDowngradesAutoDeleteAll`,
  `TestCleanupPrompt_ForcePerVersionIgnoresPerVersionFalse`, and
  `TestCleanupPrompt_ForcePerVersionWithNonInteractive` for the
  preservation. `TestCleanupPrompt_PerVersionConfirm` and
  `TestCleanupPrompt_SpecificVersion` (the tests that pinned
  the buggy double-prompt behavior) are updated to assert the
  new correct behavior; `TestCleanupPrompt_DeleteAllSkipsPerVersionPrompt`
  is the explicit regression test for #76. Closes #76.
- `CLAUDE.md`: refresh the stale sections that have drifted out
  of sync with the actual code and with `README.md` /
  `CHANGELOG.md`. The "Known bugs (do not re-introduce)" entry
  used to describe a `ManifestVersion.LTS bool` + useless
  `TS string \`json:"ts"\`` fallback — but the actual code at
  `internal/node/dist.go:25-62` already uses `LTSCodename *string`
  with a custom `UnmarshalJSON` that handles the nodejs.org `lts`
  JSON union correctly. The pre-fix shape is gone; the doc would
  have misled a future contributor or AI assistant into
  "fixing" something that isn't broken. The phase-status table
  listed Phase 5 (Config subsystem) and Phase 6 (Cross-platform
  polish) as "Not started" — both are fully implemented per the
  CHANGELOG's Unreleased section and per `README.md`'s own
  Project-status table, which marks Phases 1-6 done. Updated
  Phases 4-6 to "Done" and Phase 7 to "In progress" (the
  GoReleaser / brew / scoop / npm distribution work tracked by
  #17 / #18 is genuinely outstanding). The architecture
  diagram listed `internal/packages/restore.go` for `Restore`
  — that file doesn't exist; `Restore` and the additional
  `RestoreFromSnapshot` both live in `snapshot.go`. The
  dependencies line listed `gjson` and `yaml.v3` as "planned
  but not yet in go.mod" — `yaml.v3` is in go.mod (used by
  the config subsystem), `gjson` was never added. Closes #54.

## [0.0.0] - 2024-07-01

### Added
- Project blueprint — internal design doc covering language choice, scope, detection engine, version resolution, package migration, architecture, CLI design, edge cases, git workflow, conventional commits, versioning, CI/CD, and distribution. (Superseded by `README.md`; the standalone doc was removed in the Unreleased section.)

[Unreleased]: https://github.com/dipto0321/nodeup/compare/v0.0.0...HEAD
[0.0.0]: https://github.com/dipto0321/nodeup/releases/tag/v0.0.0