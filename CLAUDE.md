# CLAUDE.md — nodeup

`nodeup` is a cross-platform Go CLI that auto-detects a Node.js version manager, upgrades LTS and Current Node versions, and migrates global npm packages. Module: `github.com/dipto0321/nodeup`. Go 1.24.

## Build & test commands

```bash
make build          # compile → ./bin/nodeup
make test           # go test -race -coverprofile=coverage.out ./...
make lint           # golangci-lint run ./...
make ci             # tidy + fmt + vet + lint + test (full local CI)
make run ARGS="upgrade --dry-run"   # build + run with args

go test ./internal/detector/...          # single package
go test ./internal/detector/... -run TestNvm  # single test
```

## Architecture

```
cmd/nodeup/main.go         entrypoint; injects version/commit/date via ldflags
internal/cli/              cobra command wiring — thin layer, delegates to internal/
  root.go                  NewRootCmd; registers all subcommands
  upgrade.go               nodeup upgrade (Phase 4, merged)
  check.go / list.go / packages.go / config.go / version.go
internal/detector/         Manager interface + one file per manager
  detector.go              DetectAll(), ResolveManager()
  fnm.go nvm.go volta.go asdf.go mise.go n.go nodenv.go nvm_windows.go
internal/node/
  dist.go                  nodejs.org/dist/index.json client + 24h TTL cache
internal/packages/         npm global snapshot / restore / migrate (merged in PR #19)
  snapshot.go              Snapshot(ctx, managerName, version) → ~/.../snapshots/<mgr>-<ver>.json
                          Restore(ctx, managerName, version) ([]PackageResult, error)
                          RestoreFromSnapshot(ctx, path) ([]PackageResult, error)
  report.go                MigrationReport: per-package results, Save() → ~/.../reports/migration-<ts>.json
  sentinel.go              UpgradeSentinel: in-progress marker for interrupted-upgrade replay
internal/platform/
  platform.go              DataDir(), SnapshotsDir(), CacheDir(), LockPath(), IsWindows(), …
  shell.go                 RunShell() — all shell exec goes here
internal/ui/               single source of truth for user-facing output
  mode.go                  Mode (PlainMode / FancyMode) + DecideMode (TTY + NO_COLOR + --no-color gating)
  theme.go                 DefaultTheme — lipgloss styles for FancyWriter
  writer.go                Writer interface + PlainWriter / FancyWriter implementations
  spinner.go               bubbletea-backed braille spinner for long-running steps
  prompt.go                huh-backed Confirm / Select helpers for interactive flows
```

## Key invariants — read before writing code

**Output routing:** All user-facing strings flow through `internal/ui`. Never use `fmt.Println` or `cmd.Printf` in business logic. `internal/cli/root.go` pkg-doc enforces this. Violation: anything in `internal/` or `cmd/` that directly prints without going through `ui`.

**Error handling:** `errcheck` is enabled and treated as a bug. Every error return must be handled. In cobra `RunE` functions, use `cmd.Context()` not `context.Background()` — `contextcheck` linter is enabled and will flag `context.Background()` calls inside functions that have a live context.

**Paths:** Always use `filepath.Join()`. Never hardcode `/` or `\\`. Use `os.UserHomeDir()` for home directory. Platform data dirs come from `platform.DataDir()`.

**Shell commands:** All exec calls go through `platform.RunShell()`. Shell-quote any path that may contain spaces (especially on Windows).

**Platform-specific code:** Use `//go:build windows` build tags on `*_windows.go` files. Files without build tags must compile on all three OSes.

**Dependencies:** No new dependencies without a rationale line in the PR body. Core runtime deps: `cobra`, `Masterminds/semver/v3`, `yaml.v3`, plus the Charm stack for `internal/ui`: `lipgloss` (styling), `bubbletea` (spinner), `huh` (prompts). The plain-vs-fancy switch is the single decision point — see `internal/ui/mode.go`.

**Manager detection order:** `--manager` flag → `~/.nodeup/config.yaml` → auto-detect (env vars → PATH → well-known dirs). `DetectAll()` returns a `Registry`; `ResolveManager(reg, preferred)` picks one or errors. When multiple managers found and no preference, the caller should use `ResolveInteractive` (a `huh`-backed Select that lives in `internal/detector/interactive.go` — wired up by #118 as part of the `huh` migration).

**Packages to skip during migration:** `npm`, `corepack`, `npx` — these are bundled with Node and must not be migrated.

## Known bugs (do not re-introduce)

`ManifestVersion.LTS` in `internal/node/dist.go:25-62` is now properly
typed as `LTSCodename *string` with a custom `UnmarshalJSON` that
handles the nodejs.org `lts` JSON union (Current releases have
`lts: false`, LTS releases have `lts: "<codename>"`). The pre-fix
shape (typed `bool` with a useless `TS string \`json:"ts"\`` fallback)
is gone. Don't "fix" this back to a plain `bool`.

## Commit & PR conventions

Enforced by commitlint (`wagoid/commitlint-github-action@v5`). Violations block merge.

**Types:** `feat`, `fix`, `docs`, `style`, `refactor`, `perf`, `test`, `build`, `ci`, `chore`, `revert`

**Scopes:** `detector`, `manager`, `packages`, `node`, `config`, `ui`, `platform`, `cli`, `deps`, `release`, `ci`, `docs`, `lint`

**Branch naming:** `feat/<scope>/<slug>`, `fix/<scope>/<slug>`, `chore/<scope>/<slug>`, `docs/<slug>`, `ci/<slug>`, `test/<scope>/<slug>`

**PR rules:** One logical change per PR. PR title follows commitlint. Squash-merged, source branch deleted. No "fix typo/lint" follow-up commits in the same PR. CI must be green (lint + test on ubuntu/macos/windows + build matrix).

## Branch protection (main)

- Require PR + 1 approving review + code owner review (`@dipto0321` owns `*`)
- Required checks: `Lint (ubuntu)`, `Test (ubuntu-latest)`, `Test (macos-latest)`, `Test (windows-latest)`
- No force pushes, no deletion; `enforce_admins: false` (owner can bypass)

## AI workflow — standing orders (do not re-ask)

When the user says **"start the next issue"** (or any equivalent — "work on the next one", "what's next", "pick up the next issue"), treat the following as the **default, no-clarification-needed workflow**:

1. **Identify the next issue.** Run `gh issue list --state open --limit 30`. Pick the lowest-numbered open issue that is not a meta-tracking issue, unless the user says otherwise. If only meta-issues remain (e.g. Phase 6 cross-platform polish — issue #16), split the meta-issue into focused sub-issues first (one per concern, one PR each), then start the first sub-issue.
2. **Use the `issue-workflow` skill.** Invoke
   [`.claude/skills/issue-workflow/SKILL.md`](./.claude/skills/issue-workflow/SKILL.md)
   to drive the full branch → PR → merge → cleanup loop. The skill
   creates one tracked `TaskCreate` per step (sync main, branch, plan,
   implement+test, PR body, push, watch CI, address review, merge,
   verify close, recurse). Don't re-implement the workflow inline —
   the skill exists so the orchestration is consistent and reviewable.
   The PR body the skill generates includes `Closes #<issue#>` —
   **do not edit this out**. The `Closes` keyword is what auto-closes
   the issue when the PR is merged.
3. **Verify the linkage before merging.** After `gh pr create`, run `gh pr view <PR#> --json body | grep -E 'Closes|Fixes' #<issue#>'` to confirm the PR body references the issue. If it does not, edit the PR body via `gh pr edit <PR#> --body-file <new-body.md>` before merging. **Do not squash-merge a PR that is not linked to its source issue.**
4. **Merge with admin override when needed.** This repo's `enforce_admins: false` means solo-author merges need `--admin`. The conventional sequence is:
   ```bash
   gh pr merge <PR#> --squash --delete-branch --admin \
       --body "Closes #<issue#>. Squash-merged per CONTRIBUTING.md."
   git checkout main && git pull --ff-only origin main
   git remote prune origin   # clean up the deleted feature branch's tracking ref
   ```
5. **Verify the issue auto-closed.** After the merge, run `gh issue view <issue#> --json state --jq .state` and confirm `CLOSED`. If still `OPEN`, add the issue close manually with `gh issue close <issue#> -c "Closed by <PR#>"`.
6. **Then recurse.** Re-run `gh issue list --state open --limit 30`. If another issue is queued, ask the user to confirm continuation or proceed (if the user already said "keep going", just proceed).

The `make next-issue` target still works for human contributors who
want a quick branch-off-main shortcut, but it now just prints the
reminder to use the skill — it does not shell out to a bash
orchestrator.

## Phase status

| Phase | Status | Branch / PR |
|---|---|---|
| 0 — Scaffold | Done | merged |
| 1 — Detector engine | Done | merged |
| 2 — Node version API | Done | merged |
| 3 — Package snapshot/restore | Done | merged (PR #19) |
| 4 — Upgrade command + UI | Done | merged |
| 5 — Config subsystem | Done | merged |
| 6 — Cross-platform polish | Done | merged (interrupted-upgrade sentinel, `QuotePath`, system-node classifier) |
| 7 — Distribution packaging | Done | GoReleaser, brew/scoop/npm wrappers all shipped (#17, #18); manual v1.0.0 npm publish (#35) bootstrapped the OIDC trust |
| 8 — v1.0.0 → v1.1.0 releases | Done | `nodeupx@1.0.1` (and now `@1.1.0` matching the Go binary) live on npm; trusted-publisher OIDC flow active for every tag (`#109`) |

## On-disk data layout

`platform.DataDir()` resolves to:
- Linux: `$XDG_DATA_HOME/nodeup` or `~/.local/share/nodeup`
- macOS: `~/Library/Application Support/nodeup`
- Windows: `%APPDATA%\nodeup`

Subdirectories: `snapshots/`, `cache/`, `reports/`. Lock file: `nodeup.lock`.

Snapshot filename convention: `<manager>-<node-version>.json`
Cache files: `node-dist-index.json` + `node-dist-index.json.meta` (RFC3339 expiry)
