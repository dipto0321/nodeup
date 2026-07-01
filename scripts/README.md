# scripts/

Project-local automation. Currently empty — the `issue-workflow.sh`
shell script that used to live here was replaced by the
[`.claude/skills/issue-workflow/SKILL.md`](../.claude/skills/issue-workflow/SKILL.md)
skill, which drives a GitHub issue through branch → PR → merge →
cleanup using AI-orchestrated tasks instead of bash.

## Why a skill (and not a Makefile target or a shell script)

The Makefile is for **build/test/lint/release** commands — anything
that produces artifacts you can ship. A shell script for the
branch → PR → merge loop is a poor fit because:

- It needs to parse Conventional Commits titles (fragile in bash ERE).
- It needs to call `gh` (couples the contributor's machine to GitHub
  auth).
- It needs to watch CI and react to review comments — bash can't do
  that without complex polling that paged output will eventually
  break.
- The puku editor crashes when interleaved `gh` output is paged in
  multiple terminal tabs (see `.conversation-history/.conversation-history-chunk4.md`
  section 17 for the full diagnosis).

A skill moves the orchestration into the AI: the assistant reads the
issue, edits files, runs validation, opens the PR, reads review
comments, and merges. The shell just executes commands, it doesn't
encode the workflow.

## When new scripts are appropriate

Add a new `scripts/<name>.sh` only for **build/test/release plumbing**
that needs to run on machines without an AI in the loop — e.g. CI
helpers, release packaging steps, local bootstrap. For anything
involving GitHub interaction or sequential decision-making, write a
skill instead.