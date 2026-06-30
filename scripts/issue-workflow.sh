#!/usr/bin/env bash
# IMPORTANT: must be interpreted by bash, not zsh. zsh's `[[ str =~ regex ]]`
# has different group-capture semantics for parenthesized alternation
# (e.g. ($ALLOWED_SCOPES)), which breaks parse_issue_title. Running under
# bash gives consistent POSIX ERE semantics regardless of the user's login
# shell. The shebang above handles that for `path/to/script.sh` invocation;
# do NOT `source` this file from zsh.
# scripts/issue-workflow.sh — drive a single GitHub issue from branch → merged PR.
#
# Subcommands:
#   start  <issue#>                 create a feature branch off main for the issue
#   pr-body <issue#> [--type=...] [--scope=...] [--slug=...]
#                                   print a PR body template, filled in
#   finish                          push, open PR, squash-merge, delete branch, sync main
#
# Conventions enforced (per CONTRIBUTING.md):
#   - Branch off main, keep main clean.
#   - One logical change per PR.
#   - Conventional Commits (`type(scope): subject`).
#   - Squash-merged; source branch deleted on merge.
#   - PR title and squash-merge subject come from the first commit.
#   - Allowed types: feat, fix, docs, style, refactor, perf, test, build, ci, chore, revert
#   - Allowed scopes: detector, manager, packages, node, config, ui, platform, cli,
#                    deps, release, ci, docs, lint
#
# Usage examples:
#   ./scripts/issue-workflow.sh start 15
#   # …edit code, commit with `git commit -m "feat(config): add yaml loader"`
#   ./scripts/issue-workflow.sh pr-body 15 --type=feat --scope=config \
#       --slug=yaml-config > /tmp/pr.md
#   gh pr create --base main --head feat/config/yaml-config \
#       --title "feat(config): add yaml loader" --body-file /tmp/pr.md
#   ./scripts/issue-workflow.sh finish
#
# Requires: git, gh (authenticated for dipto0321/nodeup), make, golangci-lint v1.64.8.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

# Regex-safe alternations: bash [[ =~ ]] uses ERE, so alternatives must be
# pipe-separated inside a group, not space-separated.
ALLOWED_TYPES_RE="feat|fix|docs|style|refactor|perf|test|build|ci|chore|revert"
ALLOWED_SCOPES_RE="detector|manager|packages|node|config|ui|platform|cli|deps|release|ci|docs|lint"
# Space-separated lists for word-splitting/iteration in cmd_pr_body.
ALLOWED_TYPES="feat fix docs style refactor perf test build ci chore revert"
ALLOWED_SCOPES="detector manager packages node config ui platform cli deps release ci docs lint"

die() { printf "\033[31merror:\033[0m %s\n" "$*" >&2; exit 1; }
info() { printf "\033[36m==>\033[0m %s\n" "$*"; }
ok()   { printf "\033[32m✓\033[0m %s\n" "$*"; }

require_clean_tree() {
    if ! git diff --quiet --ignore-submodules HEAD 2>/dev/null; then
        die "working tree is dirty. Commit or stash first."
    fi
}

require_main_clean() {
    git rev-parse --abbrev-ref HEAD | grep -qx main \
        || die "run this from 'main' (currently on '$(git rev-parse --abbrev-ref HEAD)')"
}

ensure_gh_authed() {
    gh auth status >/dev/null 2>&1 \
        || die "gh is not authenticated. Run: gh auth login"
}

fetch_issue_meta() {
    local issue="$1"
    gh issue view "$issue" --json title,state,labels,body 2>/dev/null \
        || die "could not fetch issue #$issue. Is gh authed for this repo?"
}

# Heuristic: derive (type, scope, slug) from an issue title like
# "feat(config): add yaml config file support".
parse_issue_title() {
    local title="$1"
    if [[ "$title" =~ ^($ALLOWED_TYPES_RE)\(($ALLOWED_SCOPES_RE)\):\ (.+)$ ]]; then
        local type="${BASH_REMATCH[1]}"
        local scope="${BASH_REMATCH[2]}"
        local rest="${BASH_REMATCH[3]}"
        # slugify: lowercase, replace non-alnum with '-', trim leading/trailing '-'
        local slug
        slug="$(printf "%s" "$rest" | tr '[:upper:]' '[:lower:]' \
            | sed -E 's/[^a-z0-9]+/-/g; s/^-+//; s/-+$//')"
        printf "%s %s %s\n" "$type" "$scope" "$slug"
    elif [[ "$title" =~ ^($ALLOWED_TYPES_RE):\ (.+)$ ]]; then
        # type without scope: scope defaults to "core"
        local type="${BASH_REMATCH[1]}"
        local rest="${BASH_REMATCH[2]}"
        local slug
        slug="$(printf "%s" "$rest" | tr '[:upper:]' '[:lower:]' \
            | sed -E 's/[^a-z0-9]+/-/g; s/^-+//; s/-+$//')"
        printf "%s %s %s\n" "$type" "core" "$slug"
    else
        # Fall back: caller must pass --type/--scope/--slug explicitly.
        printf " \n"
    fi
}

cmd_start() {
    local issue="${1:-}"
    [[ -n "$issue" ]] || die "usage: issue-workflow.sh start <issue#>"

    require_main_clean
    require_clean_tree
    ensure_gh_authed

    info "fetching issue #$issue"
    local meta title state
    meta="$(fetch_issue_meta "$issue")"
    title="$(printf "%s" "$meta" | python3 -c "import sys,json; print(json.load(sys.stdin)['title'])")"
    state="$(printf "%s" "$meta" | python3 -c "import sys,json; print(json.load(sys.stdin)['state'])")"
    [[ "$state" == "OPEN" ]] || die "issue #$issue is $state, not OPEN"

    local parsed type scope slug branch
    parsed="$(parse_issue_title "$title")"
    type="$(awk '{print $1}' <<<"$parsed")"
    scope="$(awk '{print $2}' <<<"$parsed")"
    slug="$(awk '{print $3}' <<<"$parsed")"

    if [[ -z "$type" || -z "$scope" || -z "$slug" ]]; then
        die "issue title '$title' does not match '<type>(<scope>): <slug>'. " \
            "Pass --type/--scope/--slug explicitly, or rename the issue."
    fi

    branch="${type}/${scope}/${slug}"
    info "syncing main"
    git fetch origin main --quiet
    git pull --ff-only origin main

    info "creating branch '$branch' from origin/main"
    git checkout -b "$branch" origin/main

    ok "branch '$branch' is ready. Commit work with: git commit -m '$type($scope): ...'"
    printf "\nNext step:\n  ./scripts/issue-workflow.sh pr-body %s --type=%s --scope=%s --slug=%s\n" \
        "$issue" "$type" "$scope" "$slug"
}

cmd_pr_body() {
    local issue="" type="" scope="" slug=""
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --type=*)  type="${1#*=}";  shift ;;
            --scope=*) scope="${1#*=}"; shift ;;
            --slug=*)  slug="${1#*=}";  shift ;;
            --*)       die "unknown flag: $1" ;;
            *)
                [[ -z "$issue" ]] && issue="$1" || die "unexpected extra arg: $1"
                shift
                ;;
        esac
    done
    [[ -n "$issue" ]] || die "usage: issue-workflow.sh pr-body <issue#> [--type=...] [--scope=...] [--slug=...]"

    if [[ -z "$type$scope$slug" ]]; then
        local meta title
        meta="$(fetch_issue_meta "$issue")"
        title="$(printf "%s" "$meta" | python3 -c "import sys,json; print(json.load(sys.stdin)['title'])")"
        local parsed
        parsed="$(parse_issue_title "$title")"
        type="${type:-$(awk '{print $1}' <<<"$parsed")}"
        scope="${scope:-$(awk '{print $2}' <<<"$parsed")}"
        slug="${slug:-$(awk '{print $3}' <<<"$parsed")}"
    fi
    [[ -n "$type$scope$slug" ]] || die "could not derive type/scope/slug — pass them explicitly"

    cat <<EOF
## Summary

<!-- One or two sentences. What does this PR change and why? Closes #$issue. -->

## Linked issues

Closes #$issue

## Type of change

- [x] \`$type\` — see the title; check the matching box below
$(for t in $ALLOWED_TYPES; do [[ "$t" != "$type" ]] && printf "      - [ ] \`%s\`\n" "$t"; done)

## Checklist

- [x] Title follows Conventional Commits (\`$type($scope): subject\`)
- [x] I ran \`make ci\` locally and it passes
- [x] I added or updated tests for the change
- [x] I updated relevant docs (README, \`docs/\`, inline godoc)
- [x] No new linter warnings
- [ ] If breaking: I documented the migration path in the PR body and updated CHANGELOG.md

## Scope notes / things reviewers may want to look at

<!-- Anything you intentionally did NOT do, or that future PRs should pick up. -->

## Screenshots / output

<!-- If the PR changes user-facing output, paste before/after. -->
EOF
}

cmd_finish() {
    local current_branch
    current_branch="$(git rev-parse --abbrev-ref HEAD)"
    [[ "$current_branch" != "main" ]] || die "finish must be run from a feature branch (you are on 'main')"
    require_clean_tree
    ensure_gh_authed

    # Sanity: at least one commit ahead of main.
    local ahead behind
    ahead="$(git rev-list --count origin/main..HEAD)"
    behind="$(git rev-list --count HEAD..origin/main)"
    [[ "$ahead" -ge 1 ]] || die "no commits ahead of origin/main. Nothing to PR."
    info "branch is $ahead commit(s) ahead of main, $behind behind"

    # Sanity: conventional-commits subject on HEAD.
    local subject
    subject="$(git log -1 --pretty=%s)"
    if ! [[ "$subject" =~ ^($ALLOWED_TYPES_RE)\(($ALLOWED_SCOPES_RE)\):\  ]]; then
        die "HEAD subject '$subject' does not match conventional-commits pattern. " \
            "Fix with: git commit --amend -m '$subject'"
    fi

    info "pushing '$current_branch' to origin"
    git push -u origin "$current_branch"

    info "opening PR (base=main, head=$current_branch)"
    gh pr create --base main --head "$current_branch" \
        --title "$subject" --body-file <(./scripts/issue-workflow.sh pr-body 0 \
            --type="$(awk -F'[(]' '{print $1}' <<<"$subject")" \
            --scope="$(awk -F'[()]' '{print $2}' <<<"$subject")" \
            --slug="") \
        || die "gh pr create failed — fill in the body manually and retry"

    local pr_number
    pr_number="$(gh pr view "$current_branch" --json number --jq .number)"
    info "PR #$pr_number opened. Waiting for CI…"

    # Wait for required checks (best-effort; 10-min cap).
    if gh pr checks "$pr_number" --watch --fail-fast --interval 30 >/dev/null 2>&1 &
       local watch_pid=$!; sleep 600; kill $watch_pid 2>/dev/null; wait 2>/dev/null; then :; fi

    info "squash-merging PR #$pr_number and deleting '$current_branch'"
    gh pr merge "$pr_number" --squash --delete-branch \
        --body "Closes the linked issue. Squash-merged per CONTRIBUTING.md." \
        || die "merge failed — finish manually with: gh pr merge $pr_number --squash --delete-branch"

    info "syncing local main"
    git checkout main
    git pull --ff-only origin main

    ok "PR #$pr_number merged. '$current_branch' deleted. main is up to date."
}

case "${1:-}" in
    start)   shift; cmd_start "$@" ;;
    pr-body) shift; cmd_pr_body "$@" ;;
    finish)  shift; cmd_finish "$@" ;;
    -h|--help|help|"")
        sed -n '2,40p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
        ;;
    *) die "unknown subcommand: $1 (try: start | pr-body | finish)" ;;
esac
