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

See [`nodeup.md`](./nodeup.md) for the full design doc. In short:

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

This is the **v1.0.0 development line**. See [`nodeup.md`](./nodeup.md) for the
phased execution plan and `CHANGELOG.md` for what's done.

| Version | Status | Notes |
|---|---|---|
| v1.0.0 | 🛠 in development | Initial release: all managers, all platforms |

## Contributing

Contributions welcome! See [`CONTRIBUTING.md`](./CONTRIBUTING.md) (TBD) and the
branching / commit conventions in [`nodeup.md`](./nodeup.md) §11–§12.

TL;DR:

- Branch from `main`: `feat/<scope>/<short-desc>`, `fix/<scope>/<short-desc>`, etc.
- Conventional Commits: `feat(detector): add Volta support`
- One PR per logical change
- CI must be green; `make ci` runs everything locally

## License

[MIT](./LICENSE) © dipto0321