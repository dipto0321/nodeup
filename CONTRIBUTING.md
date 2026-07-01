# Contributing to nodeup

Thanks for considering a contribution. `nodeup` is a small Go CLI with a
single maintainer and a strict, automated workflow — read this once before
opening an issue or a PR.

## Quick orientation

- **Language / runtime:** Go 1.22+
- **Architecture:** see the `How it works` section in [`README.md`](./README.md)
  for the high-level pipeline. The single source of truth for implementation
  details is the Go source under `internal/` plus inline godoc.
- **Branching:** always branch from `main`. `main` is protected — every
  change ships via PR and is squash-merged with the source branch deleted.
- **Releases:** tag-driven. Pushing a `v*.*.*` tag to `origin` fires the
  GoReleaser workflow (`.github/workflows/release.yml` →
  [Homebrew tap](https://github.com/dipto0321/homebrew-tap),
  [Scoop bucket](https://github.com/dipto0321/scoop-bucket), npm wrapper).

## Branching conventions

Branch names use one of:

| Prefix | Use for |
|---|---|
| `feat/<scope>/<slug>` | New user-facing feature |
| `fix/<scope>/<slug>` | Bug fix |
| `chore/<scope>/<slug>` | Maintenance, deps, refactors with no behavior change |
| `docs/<slug>` | Documentation only |
| `ci/<slug>` | CI/CD only |
| `test/<scope>/<slug>` | Tests only |
| `refactor/<scope>/<slug>` | Refactor only |

`<scope>` is one of the commitlint scopes (see below). Examples:
`feat/check/dist-index-fetcher`, `fix/detector/nvm-windows-registry`,
`docs/clarify-installation`, `ci/bump-golangci-lint`.

## Commit messages — Conventional Commits

Enforced by [commitlint](https://github.com/conventional-changelog/commitlint)
in CI (`wagoid/commitlint-github-action@v5`). Local check: `npx commitlint --edit`.

**Allowed types:** `feat`, `fix`, `docs`, `style`, `refactor`, `perf`,
`test`, `build`, `ci`, `chore`, `revert`.

**Allowed scopes:** `detector`, `manager`, `packages`, `node`, `config`,
`ui`, `platform`, `cli`, `deps`, `release`, `ci`, `docs`, `lint`.

**Breaking changes:** append `!` after the type/scope and add a
`BREAKING CHANGE:` footer in the commit body, e.g.:

```
feat(config)!: drop legacy manager=auto key

BREAKING CHANGE: config.manager=auto is no longer recognized. Use
config.manager=<name> explicitly, or omit the key for auto-detect.
```

**Format:**

```
<type>(<scope>): <subject>            # imperative mood, <= 100 chars, no trailing period
<blank line>
<body wrapped at 100 cols>             # explain *what* and *why*, not how
<blank line>
<footer>                               # BREAKING CHANGE: …, Refs #…, Signed-off-by: …
```

## Pull request workflow

1. **One PR per logical change.** Don't bundle unrelated fixes.
2. **Title = first commit subject.** PR title goes through the same
   commitlint rules.
3. **Fill in `.github/PULL_REQUEST_TEMPLATE.md`.** The Type-of-change
   checklist maps to the SemVer bump.
4. **CI must be green.** Same 10-check matrix as `main`:
   - 5 builds (linux/darwin × amd64/arm64 + windows/amd64)
   - 1 lint (golangci-lint + commitlint)
   - 3 OS tests (ubuntu/macos/windows)
   - 1 CoGitto status check
5. **Squash-merge with the source branch deleted** — same as PRs #1–#10.
6. **No force-pushes after review.** Force-pushes during authoring are
   fine (use `--force-with-lease`).
7. **Every PR must reference its source issue in the body** — use
   `Closes #N` (closing the issue on merge) or `Refs #N` (linked but
   not auto-closed). The
   [`.claude/skills/issue-workflow`](./.claude/skills/issue-workflow/SKILL.md)
   skill fills this in automatically from the issue number when the
   AI generates the PR body; do not strip it. A PR without an issue
   reference will be sent back for revision.

## Local development

```bash
# All CI checks locally
make ci

# Just the Go bits
make build
make test
make lint

# One package
go test ./internal/detector/...

# One test (regex matched against func names)
go test ./internal/detector/... -run TestNvm
```

`make ci` runs `tidy`, `vet`, `test`, and `golangci-lint run`. There is
no separate "docs regen" step — docs are pure markdown and updated by
hand in PRs.

## Issue etiquette

- **Bug reports:** use `.github/ISSUE_TEMPLATE/bug_report.md`. Reproduce
  with `--verbose` (`-v`) and paste the relevant lines. Fill in `nodeup
  version`, OS/arch, manager, manager version, and installed Node
  versions (`nodeup list`).
- **Feature requests:** use `.github/ISSUE_TEMPLATE/feature_request.md`.
  Read the in-template "Out-of-scope check" before filing — `.nvmrc` /
  `.node-version` management, `npm`-itself updates, `yarn`/`pnpm`
  globals, and self-updates are explicitly out of scope for v1.
- **Security issues:** do **not** open a public issue. Email the
  maintainer directly (see GitHub profile) and give 90 days for a
  coordinated fix.

## Coding style

- **Go:** enforced by `golangci-lint` with `errcheck`, `staticcheck`,
  `gocritic`, etc. (` .golangci.yml`). Two-space tabs, no manual
  alignment columns, no exported names without a godoc comment.
- **Commits:** squash-merged, so each commit's diff can be standalone.
  No "fix typo" / "fix lint" follow-ups in the same PR.
- **User-facing output:** goes through `internal/ui` — never
  `fmt.Println` from business logic. This keeps output testable and
  uniform (color, spinners, summary tables).
- **No new dependencies without a rationale line in the PR body.** We
  lean on the stdlib + cobra + yaml.v3.

## Sign-off (future)

When the project gains external contributors, we'll require a
[DCO](https://developercertificateoforigin.org/) sign-off (`Signed-off-by:`
trailer on every commit). Not required today; will be enabled by a
`.github/workflows/dco.yml` check before the v1.0.0 release.
