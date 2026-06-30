# nodeup

> Automated Node.js version upgrade + global package migration CLI.
> Cross-platform · Multi-manager · Zero manual steps.

[![CI](https://github.com/dipto0321/nodeup/actions/workflows/ci.yml/badge.svg)](https://github.com/dipto0321/nodeup/actions/workflows/ci.yml)
[![Release](https://github.com/dipto0321/nodeup/actions/workflows/release.yml/badge.svg)](https://github.com/dipto0321/nodeup/releases)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/dipto0321/nodeup)](https://goreportcard.com/report/github.com/dipto0321/nodeup)

Every time Node.js ships a new LTS or Current release, you need to:

1. Look up the new version on nodejs.org
2. `fnm install <version>` (or `nvm install`, or `volta install node@<ver>`, ...)
3. Snapshot your global `npm` packages from the old version
4. Switch to the new version and reinstall every package
5. Optionally clean up the old version

`nodeup` collapses all of that into a single command:

```bash
nodeup upgrade
```

It auto-detects your version manager, fetches the latest LTS and Current
from `nodejs.org`, installs them, snapshots your global packages, migrates
them over, and (optionally) cleans up — all interactively, all resumable.

---

## Features

- 🔍 **Auto-detects** your Node.js version manager: `fnm`, `nvm`, `Volta`,
  `asdf`, `mise`, `n`, `nodenv`, `nvm-windows`
- 📦 **Migrates global npm packages** across Node versions automatically
- 🌍 **Cross-platform**: macOS, Linux, Windows (incl. ARM64)
- 💾 **Single static binary** — no runtime, no npm dependency
- 🛡️ **Resumable**: snapshots are written to disk before mutation
- 🧪 **Dry-run mode** — see the plan before anything changes
- 🔌 **Zero lock-in**: works on top of your existing manager, doesn't replace it

## Compatibility notes

A few things worth knowing before you run `nodeup`:

- **`nvm` is a shell function, not a binary** — `nodeup` transparently
  sources `~/.nvm/nvm.sh` (or `$NVM_DIR/nvm.sh`) before calling it. No
  setup required.
- **Multiple managers installed?** `nodeup` prompts you to pick one
  the first time and remembers it in `~/.nodeup/config.yaml`. You can
  override per-invocation with `--manager <name>`.
- **System Node (e.g. installed via Homebrew, apt, or the Windows
  installer) is detected but cannot be upgraded** — install a version
  manager first if you want `nodeup` to manage it.
- **Bundled packages are always skipped during migration**: `npm`,
  `corepack`, and `npx` ship with Node itself and are not reinstalled.
- **Native addons may need a rebuild** after a major Node version
  bump. If something like `node-sass` or `sharp` misbehaves, run
  `npm rebuild -g` against the new version.
- **Concurrent runs are blocked** via a lock file at
  `~/.nodeup/nodeup.lock`. If a run crashes mid-upgrade, the next
  invocation offers to restore from the snapshot written at the start.

## Installation

### Homebrew (macOS, Linux)

```bash
brew install dipto0321/tap/nodeup
```

### Scoop (Windows)

```powershell
scoop bucket add dipto0321 https://github.com/dipto0321/scoop-bucket
scoop install nodeup
```

### npm wrapper (any platform)

```bash
npm install -g nodeup
```

The npm wrapper downloads the right binary for your platform at install time.

### Direct binary download

Grab the archive for your OS/arch from the [Releases page](https://github.com/dipto0321/nodeup/releases):

```bash
# macOS Apple Silicon
curl -L https://github.com/dipto0321/nodeup/releases/latest/download/nodeup_$(curl -s https://api.github.com/repos/dipto0321/nodeup/releases/latest | grep tag_name | cut -d'"' -f4 | tr -d v)_darwin_arm64.tar.gz | tar xz
sudo mv nodeup /usr/local/bin/

# Linux x86_64
curl -L https://github.com/dipto0321/nodeup/releases/latest/download/nodeup_*_linux_amd64.tar.gz | tar xz
sudo mv nodeup /usr/local/bin/
```

### From source

```bash
go install github.com/dipto0321/nodeup/cmd/nodeup@latest
```

## Quickstart

```bash
# See what's available without installing anything
nodeup check

# Upgrade both LTS and Current, migrate global packages, then ask about cleanup
nodeup upgrade

# Just LTS
nodeup upgrade --lts

# Just Current
nodeup upgrade --current

# Plan only — no changes
nodeup upgrade --dry-run

# Non-interactive (CI-friendly)
nodeup upgrade --yes
```

If you have multiple managers installed, `nodeup` will prompt you to pick one
the first time and remember it in `~/.nodeup/config.yaml`.

## Commands

```
nodeup upgrade              Upgrade LTS and/or Current
nodeup check                Show what's available, install nothing
nodeup list                 List installed versions via your manager
nodeup packages             Manage global package snapshots
nodeup config               Manage nodeup configuration
nodeup version              Print version info
```

Run `nodeup <command> --help` for the full flag reference.

## How it works

In short:

1. **Detect** which version manager(s) are installed
2. **Resolve** to a single manager (prompt if multiple)
3. **Fetch** the latest LTS and Current from `nodejs.org/dist/index.json`
4. **Diff** installed vs remote to compute the upgrade plan
5. **Snapshot** the global packages of each version being replaced
6. **Install** the new versions via your manager
7. **Migrate** the snapshots to the new versions
8. **Cleanup** old versions (opt-in)

## Configuration

The optional config file lives at `~/.nodeup/config.yaml`:

```yaml
manager: fnm
track:
  lts: true
  current: false
packages:
  migrate: true
  strategy: exact   # exact | latest
  skip:
    - npm
    - corepack
cleanup:
  auto: false
  prompt: true
cache:
  ttl: 3600
```

Flags override env vars (`NODEUP_MANAGER`, `NODEUP_TRACK_LTS`, `NODEUP_CACHE_TTL`)
override the file. See [`docs/configuration.md`](./docs/configuration.md) for the
full schema.

## Project status

This is the **v1.0.0 development line**. See `CHANGELOG.md` for what's done.

| Version | Status | Notes |
|---|---|---|
| v1.0.0 | 🛠 in development | Phase 1 ✅ — 8/8 managers detected. Phase 2 ✅ — `nodeup check` with nodejs.org/dist/index.json fetch + TTL cache. Phase 3 ✅ — package snapshot/restore + migration report. Phase 4 — end-to-end upgrade command. |

Phase 1 is the **detection surface** — every manager is recognized and the
version + installed-list reads return real data (PRs #1–#8). Subsequent
phases layer commands on top: `nodeup check` (Phase 2) → `nodeup packages`
(Phase 3) → `nodeup upgrade` end-to-end (Phase 4) → `nodeup config`
(Phase 5) → first tagged release (Phase 7).

## Docs index

| Topic | Doc |
|---|---|
| Supported version managers, detection, locking to one | [`docs/managers.md`](./docs/managers.md) |
| Config schema, precedence rules, env vars | [`docs/configuration.md`](./docs/configuration.md) |
| Install channels (Homebrew / Scoop / npm / binary / source) | [`docs/installation.md`](./docs/installation.md) |
| First-stable + patch release runbook | [`docs/release-checklist.md`](./docs/release-checklist.md) |

## Contributing

See [`CONTRIBUTING.md`](./CONTRIBUTING.md) for the working contract —
branching, Conventional Commits rules, local dev (`make ci`), PR
workflow, issue / security etiquette, and coding style.

## License

[MIT](./LICENSE) © dipto0321