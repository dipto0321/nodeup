---
name: issue-workflow
description: Drive a single GitHub issue from "open on main" through branch → commits → PR → CI green → squash-merge → issue closed → main synced. Replaces the old scripts/issue-workflow.sh. Use when the user says "work on issue #N", "implement issue #N", "finish issue #N", "next issue", or asks to start a feature/fix branch tied to a specific issue number.
---

# issue-workflow

Drive one GitHub issue through the full branch → PR → merge → cleanup
loop. This skill is the AI-equivalent of the old `scripts/issue-workflow.sh`
shell script: instead of imperative bash, it produces a tracked list of
tasks the assistant walks through one at a time.

The skill encodes the project conventions in [`CONTRIBUTING.md`](../../CONTRIBUTING.md):
- branch off `main`, keep `main` clean
- one logical change per PR
- Conventional Commits (`type(scope): subject`)
- squash-merge, source branch deleted on merge
- PR title = first commit subject
- allowed types: `feat`, `fix`, `docs`, `style`, `refactor`, `perf`, `test`, `build`, `ci`, `chore`, `revert`
- allowed scopes: `detector`, `manager`, `packages`, `node`, `config`, `ui`, `platform`, `cli`, `deps`, `release`, `ci`, `docs`, `lint`
- every PR body must reference its source issue via `Closes #N` or `Refs #N`

## When to invoke

The user explicitly asks to work on an issue by number, or says
"next issue" / "finish the open issue" / "implement #N". Do not invoke
this skill for ad-hoc exploratory work or for changes that don't have
a tracking issue — open an issue first.

## Inputs to collect up front

Before doing anything, ask the user (use `AskUserQuestion` if more than
one is ambiguous):

1. **Issue number** — the only required input. If the user says "next
   issue", find the lowest-numbered open issue via `gh issue list
   --state open --limit 1 --json number,title`.
2. **Type / scope / slug** — for branching. Try to derive these from
   the issue title (it should follow `type(scope): subject` per
   CONTRIBUTING.md). If the title doesn't match, ask the user to
   rename it OR to pass the three explicitly.

   Slug rules: lowercase, non-alnum → `-`, trim leading/trailing `-`.
   Branch name is `<type>/<scope>/<slug>`.

## Workflow (in order)

Create one `TaskCreate` per step below, mark `in_progress` when you
start it, `completed` when done. Don't skip steps. If a step fails,
stop and surface the failure to the user — do not paper over it.

### 1. Inspect the issue

`gh issue view <N> --json title,state,labels,body,number`

Confirm `state == OPEN`. If closed, tell the user and stop. Skim the
body for scope, linked issues, and any "Out of scope" notes.

### 2. Sync local main

```
git fetch origin main --quiet
git checkout main
git pull --ff-only origin main
```

If `git pull` refuses (non-fast-forward), stop and tell the user — do
not force.

### 3. Create the feature branch

`git checkout -b <type>/<scope>/<slug> origin/main`

Verify with `git rev-parse --abbrev-ref HEAD` that you're on the new
branch.

### 4. Plan the work

Before writing any code, read enough of the codebase to know:
- which files this issue touches (use `Glob`/`Grep`)
- what the change shape looks like (one paragraph mental summary)
- what tests need to be added or updated
- what docs/CHANGELOG need updating

If the change is larger than ~200 lines or spans 3+ packages, surface
that to the user — `CONTRIBUTING.md` says "one logical change per PR"
and a too-large PR may need to be split into multiple issues.

### 5. Implement + test + lint loop

For each logical chunk of work:
- write the change
- `go build ./...`
- `go vet ./...`
- `go test ./...` (or scoped to the package)
- `gofmt -l .` (no diffs)
- commit with a Conventional Commits subject that matches the PR
  title the user wants

Subject must match `^(feat|fix|docs|style|refactor|perf|test|build|ci|chore|revert)(\([a-z]+\))?!?: .{1,100}$`.
Body lines ≤100 cols. Footer for `Refs #N` / `BREAKING CHANGE:` etc.

Commit author should be the assistant's Co-Authored-By line per the
project's commit style.

### 6. Run `make ci`

`make ci` runs `tidy` + `vet` + `test` + `golangci-lint`. Must be green
before opening the PR. If `golangci-lint` complains, fix the lint
issues — don't suppress.

### 7. Generate the PR body

Use the template at `.github/PULL_REQUEST_TEMPLATE.md`. Fill in:
- `## Summary` — one or two sentences, end with `Closes #N`
- `## Linked issues` — `Closes #N`
- `## Type of change` — check the matching box
- `## Checklist` — leave unchecked only the ones the user hasn't done
- `## Scope notes` — anything intentionally left out
- `## Screenshots / output` — if user-facing output changed

### 8. Push and open the PR

```
git push -u origin <branch>
gh pr create --base main --head <branch> \
  --title "<first commit subject>" \
  --body-file /tmp/pr-body-<N>.md
```

Capture the PR number from the output.

### 9. Watch CI

`gh pr checks <N> --watch --fail-fast --interval 30`

Don't poll in a loop. Don't run multiple `gh pr checks` in parallel.
If a check fails, read the failure log, fix it, push an amendment,
re-run.

### 10. Address review comments

If a reviewer (human or bot like Copilot) leaves comments on the PR,
read each one, decide whether to apply the fix or push back with a
rationale. Don't blindly accept suggestions — apply the ones that
improve the code, push back on the ones that don't.

### 11. Squash-merge

```
gh pr merge <N> --squash --delete-branch
```

If the repo's branch policy refuses, retry with `--admin` and tell
the user you used admin override.

### 12. Verify the issue closed

`gh issue view <N> --json state,stateReason`

If `stateReason == COMPLETED` and the linked PR shows in
`closedByPullRequestReferences`, you're done. If the issue is still
open, the PR body is missing `Closes #N` — fix the PR description
with `gh pr edit <N> --body-file ...` and re-check.

### 13. Sync local main and clean up

```
git checkout main
git pull --ff-only origin main
git branch -d <branch>           # local cleanup
```

If `git branch -d` refuses (branch not fully merged), the squash-merge
didn't actually merge — go back to step 11.

### 14. Recurse to the next open issue

Find the next open issue (lowest number that is still OPEN and not
blocked). Tell the user what the next issue is and ask whether to
continue.

## Failure modes to handle

| Symptom | What to do |
|---|---|
| Issue title doesn't match `type(scope): …` | Ask user to rename it, OR pass `--type/--scope/--slug` explicitly |
| `git pull --ff-only` fails | Stop — `main` has diverged. Tell user. |
| `golangci-lint` reports a finding | Fix the code, don't suppress |
| `gh pr create` errors about base branch policy | The repo requires admin merge — use `--admin` on step 11 |
| Reviewer comment is wrong or out-of-scope | Push back politely with rationale in a reply |
| Squash-merge silently fails | Check `gh pr view <N> --json state` — likely blocked by CI |
| Issue didn't auto-close | PR body missing `Closes #N`. Edit with `gh pr edit`. |
| `git branch -d` says branch not fully merged | Squash-merge didn't actually merge — investigate |

## Output expectations

When the skill completes successfully, summarize in one short message:

```
Issue #N closed via PR #M.
Branch: <branch>
Squash-merge: <short-sha>
Next open issue: #<next> — <title>
Want me to start on it?
```

## Why this is a skill and not a bash script

The old `scripts/issue-workflow.sh` was 265 lines of bash that:
- parsed Conventional Commits titles with bash regex (fragile)
- called `gh` directly (coupled contributor machines to GitHub auth)
- embedded `gh pr checks --watch` in a `sleep 600; kill $watch_pid`
  pattern that crashed the IDE when paged output interleaved with
  other terminal tabs
- made every contributor's shell the orchestration layer

This skill moves orchestration into the AI: the assistant reads the
issue, plans the change, edits files, runs validation, opens the PR,
reads review comments, and merges. No contributor shell needed.
The shell is just the place commands execute, not the place workflows
are encoded.