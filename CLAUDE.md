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
  upgrade.go               nodeup upgrade (Phase 4, PR #20 open)
  check.go / list.go / packages.go / config.go / version.go
internal/detector/         Manager interface + one file per manager
  detector.go              DetectAll(), ResolveManager()
  fnm.go nvm.go volta.go asdf.go mise.go n.go nodenv.go nvm_windows.go
internal/node/
  dist.go                  nodejs.org/dist/index.json client + 24h TTL cache
internal/packages/         npm global snapshot / restore / migrate (merged in PR #19)
  snapshot.go              Snapshot(ctx, managerName, version) → ~/.../snapshots/<mgr>-<ver>.json
  restore.go               Restore(ctx, managerName, version)
internal/platform/
  platform.go              DataDir(), SnapshotsDir(), CacheDir(), LockPath(), IsWindows(), …
  shell.go                 RunShell() — all shell exec goes here
internal/ui/               (planned, not yet implemented) all user-facing output
```

## Key invariants — read before writing code

**Output routing:** All user-facing strings flow through `internal/ui`. Never use `fmt.Println` or `cmd.Printf` in business logic. `internal/cli/root.go` pkg-doc enforces this. Violation: anything in `internal/` or `cmd/` that directly prints without going through `ui`.

**Error handling:** `errcheck` is enabled and treated as a bug. Every error return must be handled. In cobra `RunE` functions, use `cmd.Context()` not `context.Background()` — `contextcheck` linter is enabled and will flag `context.Background()` calls inside functions that have a live context.

**Paths:** Always use `filepath.Join()`. Never hardcode `/` or `\\`. Use `os.UserHomeDir()` for home directory. Platform data dirs come from `platform.DataDir()`.

**Shell commands:** All exec calls go through `platform.RunShell()`. Shell-quote any path that may contain spaces (especially on Windows).

**Platform-specific code:** Use `//go:build windows` build tags on `*_windows.go` files. Files without build tags must compile on all three OSes.

**Dependencies:** No new dependencies without a rationale line in the PR body. Core runtime deps: `cobra`, `Masterminds/semver/v3`. Planned but not yet in `go.mod`: `huh`, `bubbletea`, `lipgloss`, `gjson`, `yaml.v3`.

**Manager detection order:** `--manager` flag → `~/.nodeup/config.yaml` → auto-detect (env vars → PATH → well-known dirs). `DetectAll()` returns a `Registry`; `ResolveManager(reg, preferred)` picks one or errors. When multiple managers found and no preference, the caller should use `ResolveInteractive` (not yet implemented).

**Packages to skip during migration:** `npm`, `corepack`, `npx` — these are bundled with Node and must not be migrated.

## Known bugs (do not re-introduce)

`ManifestVersion.LTS` in `internal/node/dist.go:22` is typed `bool`, but the nodejs.org API returns a union: `false` for Current releases and a string codename (e.g. `"Iron"`) for LTS releases. The fallback `TS string \`json:"ts"\`` on line 23 does not help because the real JSON key is `lts`, not `ts`. Fix requires a custom `UnmarshalJSON` or `json.RawMessage` on the `LTS` field. This is a latent bug activated by `upgrade.go` calling `FetchManifest()`. Tracked in PR #20 review.

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
2. **Always pass the issue number through `issue-workflow.sh`.** Use `./scripts/issue-workflow.sh start <issue#>` to branch, and `./scripts/issue-workflow.sh pr-body <issue#>` to render the PR body. The body includes `Closes #<issue#>` — **do not edit this out**. The `Closes` keyword is what auto-closes the issue when the PR is merged.
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

The `make next-issue` target wraps steps 1 and 2 for humans. AI sessions should drive the same script directly.

## Phase status

| Phase | Status | Branch / PR |
|---|---|---|
| 0 — Scaffold | Done | merged |
| 1 — Detector engine | Done | merged |
| 2 — Node version API | Done | merged |
| 3 — Package snapshot/restore | Done | merged (PR #19) |
| 4 — Upgrade command + UI | In progress | `feat/upgrade/end-to-end` / PR #20 |
| 5 — Config subsystem | Not started | — |
| 6 — Cross-platform polish | Not started | — |
| 7 — Distribution packaging | Not started | — |
| 8 — v1.0.0 release | Not started | — |

## On-disk data layout

`platform.DataDir()` resolves to:
- Linux: `$XDG_DATA_HOME/nodeup` or `~/.local/share/nodeup`
- macOS: `~/Library/Application Support/nodeup`
- Windows: `%APPDATA%\nodeup`

Subdirectories: `snapshots/`, `cache/`, `reports/`. Lock file: `nodeup.lock`.

Snapshot filename convention: `<manager>-<node-version>.json`
Cache files: `node-dist-index.json` + `node-dist-index.json.meta` (RFC3339 expiry)
