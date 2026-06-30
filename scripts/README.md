# scripts/

Project-local automation. Currently:

- `issue-workflow.sh` — drive a single GitHub issue through the full
  branch → PR → merge → cleanup loop. Three subcommands:
  - `start <issue#>` — sync `main` and create `<type>/<scope>/<slug>` off `origin/main`
  - `pr-body <issue#>` — print a `.github/PULL_REQUEST_TEMPLATE.md`-shaped body
  - `finish` — push, open PR, watch CI, squash-merge, delete branch, sync `main`

  Enforces the conventions in `CONTRIBUTING.md` (Conventional Commits,
  allowed types/scopes, one logical change per PR, squash-merge,
  source-branch deletion). See `./issue-workflow.sh --help` for details.

## Why a shell script (and not a Makefile target)

The Makefile is for **build/test/lint/release** commands — anything
that produces artifacts you can ship. `issue-workflow.sh` is a
**git/GitHub plumbing** helper: it talks to `gh`, reads PR titles,
force-pushes with `--force-with-lease`, and reshapes your local
checkout. Mixing those into `make` would couple the build to GitHub
auth and to `gh` being installed, which isn't true for every
contributor's machine.