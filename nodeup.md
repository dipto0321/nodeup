# `nodeup` — Full Project Blueprint

> Automated Node.js version upgrade + global package migration CLI
> Cross-platform · Multi-manager · Zero manual steps

---

## Table of Contents

1. [Why This Tool Exists](#1-why-this-tool-exists)
2. [Language Choice: Go](#2-language-choice-go)
3. [Tool Name & Scope](#3-tool-name--scope)
4. [How It Works — Core Algorithm](#4-how-it-works--core-algorithm)
5. [Version Manager Detection Engine](#5-version-manager-detection-engine)
6. [Node.js Version Resolution](#6-nodejs-version-resolution)
7. [Global Package Migration](#7-global-package-migration)
8. [Architecture & Project Structure](#8-architecture--project-structure)
9. [CLI Design & Commands](#9-cli-design--commands)
10. [Edge Cases & Error Handling](#10-edge-cases--error-handling)
11. [Git & GitHub Workflow](#11-git--github-workflow)
12. [Conventional Commits](#12-conventional-commits)
13. [Versioning Strategy](#13-versioning-strategy)
14. [CI/CD with GitHub Actions](#14-cicd-with-github-actions)
15. [Publishing & Distribution](#15-publishing--distribution)
16. [Phased Execution Plan](#16-phased-execution-plan)

---

## 1. Why This Tool Exists

Every time Node.js releases a new LTS or Current version, a developer using `fnm` or `nvm` must:

1. Look up the new LTS version number at nodejs.org
2. Run `fnm install <version>` or `nvm install --lts`
3. Switch to the old version, run `npm list -g --depth=0`, copy the list
4. Switch to the new version, re-install each package manually
5. Optionally remove the old version
6. Repeat for Current if they track that too

`nodeup` collapses all of that into one command: `nodeup upgrade`.

---

## 2. Language Choice: Go

| Criterion | Go | Node.js | Python | Rust |
|---|---|---|---|---|
| Single binary | ✅ Yes | ❌ Needs runtime | ❌ Needs runtime | ✅ Yes |
| Cross-platform build | ✅ `GOOS/GOARCH` | ⚠️ Partial | ⚠️ Partial | ✅ Yes |
| Bootstrap paradox | ✅ None | ❌ Needs Node to manage Node | ✅ None | ✅ None |
| Distribution (Homebrew/Scoop) | ✅ First-class | ⚠️ Via npm only | ⚠️ Via pip/pypi | ✅ First-class |
| Compilation speed | ✅ Fast | N/A | N/A | ⚠️ Slow |
| Shell execution | ✅ `os/exec` | ✅ `child_process` | ✅ `subprocess` | ✅ `std::process` |
| GoReleaser support | ✅ Native | ❌ No | ❌ No | ✅ Via cargo |
| Learning curve | 🟡 Moderate | ✅ You know it | ✅ Easy | 🔴 Steep |

**Go wins** because:
- The bootstrap paradox is a real UX risk — if the tool is written in Node.js and the user's Node.js install is broken, the tool can't run.
- GoReleaser makes publishing cross-platform binaries to GitHub Releases, Homebrew, and Scoop nearly automatic.
- A single static binary means zero dependency installation for end users.

### Key Go Libraries

```
github.com/spf13/cobra          # CLI framework (industry standard)
github.com/charmbracelet/bubbletea  # TUI / interactive prompts
github.com/charmbracelet/lipgloss   # Terminal styling
github.com/charmbracelet/huh        # Form/select UI
github.com/tidwall/gjson            # Fast JSON parsing (nodejs.org API)
github.com/Masterminds/semver/v3    # Semantic version comparison
```

---

## 3. Tool Name & Scope

**Name: `nodeup`**

Alternatives considered: `nvmate`, `nodejump`, `nodekick`, `nshift`

`nodeup` reads naturally as "upgrade Node" and is short for shell use.

### In Scope (v1)
- Detect installed version managers
- Fetch LTS + Latest versions from nodejs.org API
- List global packages per installed Node version
- Install new Node versions via the detected manager
- Migrate global packages to new versions
- Remove old Node versions (opt-in)
- Dry-run mode

### Out of Scope (v1, consider for v2)
- Managing `.nvmrc` / `.node-version` files in projects
- Updating npm itself
- Managing yarn/pnpm global packages
- GUI / Electron wrapper
- Self-update mechanism

---

## 4. How It Works — Core Algorithm

```
nodeup upgrade
     │
     ▼
┌──────────────────────────────┐
│  1. DETECT VERSION MANAGERS  │
│  Scan $PATH + env vars       │
│  Found: [fnm, nvm]           │
└──────────┬───────────────────┘
           │ multiple found?
           ▼
┌──────────────────────────────┐
│  2. RESOLVE MANAGER          │
│  Single → use it             │
│  Multiple → prompt user      │
└──────────┬───────────────────┘
           │
           ▼
┌──────────────────────────────┐
│  3. FETCH NODE VERSIONS      │
│  GET nodejs.org/dist/index   │
│  Resolve: LTS latest = 20.x  │
│  Resolve: Current = 22.x     │
└──────────┬───────────────────┘
           │
           ▼
┌──────────────────────────────┐
│  4. DIFF INSTALLED vs REMOTE │
│  fnm list → [18.x, 20.x]    │
│  Remote LTS = 20.15.0 ✅     │
│  Remote Current = 22.5.0 ✨  │
│  "22.x not installed"        │
└──────────┬───────────────────┘
           │
           ▼
┌──────────────────────────────┐
│  5. SNAPSHOT GLOBAL PKGS     │
│  npm list -g --depth=0 --json│
│  Per version: snapshot saved │
└──────────┬───────────────────┘
           │
           ▼
┌──────────────────────────────┐
│  6. INSTALL NEW VERSIONS     │
│  fnm install 22.5.0          │
│  Verify install succeeded    │
└──────────┬───────────────────┘
           │
           ▼
┌──────────────────────────────┐
│  7. MIGRATE GLOBAL PACKAGES  │
│  fnm use 22.5.0              │
│  npm install -g <each pkg>   │
│  Report: ✅ installed / ❌ fail│
└──────────┬───────────────────┘
           │
           ▼
┌──────────────────────────────┐
│  8. CLEANUP (opt-in)         │
│  Prompt: remove 20.x? (y/N)  │
│  fnm uninstall 20.x          │
└──────────────────────────────┘
```

---

## 5. Version Manager Detection Engine

### Detection Strategy

Detection runs in **this priority order**:

```
1. Check CLI flags (--manager fnm)  ← user override, highest priority
2. Read config file (~/.nodeup.yaml)
3. Auto-detect from environment
4. Prompt if ambiguous
```

### Auto-Detection Logic (per manager)

```
fnm
  ├── env: FNM_DIR exists
  ├── binary: `which fnm` resolves
  └── files: ~/.local/share/fnm (Linux), ~/Library/Application Support/fnm (macOS)

nvm
  ├── env: NVM_DIR exists
  ├── shell: nvm is a shell function (not a binary)
  └── files: ~/.nvm/nvm.sh exists
  ⚠️  nvm is a SHELL FUNCTION, not an executable. Must be sourced.
      Strategy: `bash -i -c "nvm --version"` to detect
      Or: check for ~/.nvm/nvm.sh directly

volta
  ├── env: VOLTA_HOME exists
  ├── binary: `which volta` resolves
  └── files: ~/.volta/bin/volta

asdf (with nodejs plugin)
  ├── env: ASDF_DIR exists OR ~/.asdf exists
  ├── binary: `which asdf` resolves
  └── plugin: `asdf plugin list | grep nodejs`

mise (formerly rtx, asdf successor)
  ├── binary: `which mise` resolves
  └── plugin: `mise plugins list | grep node`

n (npm-based version manager)
  ├── binary: `which n` resolves
  └── env: N_PREFIX (optional)

nodenv
  ├── binary: `which nodenv` resolves
  └── files: ~/.nodenv/shims

nvm-windows (Windows only)
  ├── binary: `nvm.exe` in PATH
  └── registry: HKCU\Software\nvm-windows
```

### Multi-Manager Matrix

```go
// internal/detector/detector.go (concept)

type Manager interface {
    Name()    string
    Detect()  bool
    Version() (string, error)
    ListInstalled() ([]semver.Version, error)
    Install(version semver.Version) error
    Uninstall(version semver.Version) error
    Use(version semver.Version) error
    SetDefault(version semver.Version) error
    GlobalNpmPrefix(version semver.Version) (string, error)
}

func DetectAll() []Manager {
    candidates := []Manager{
        &FNM{}, &NVM{}, &Volta{}, &ASDF{}, &Mise{}, &N{}, &Nodenv{},
    }
    // On Windows, add NVMWindows{}
    
    var found []Manager
    for _, m := range candidates {
        if m.Detect() {
            found = append(found, m)
        }
    }
    return found
}
```

### The nvm Special Case

`nvm` is NOT a binary — it's a shell function loaded by `.bashrc`/`.zshrc`. This is the trickiest part of the entire project.

**Three strategies:**

```
Strategy A: Direct script execution (preferred)
  source ~/.nvm/nvm.sh && nvm <command>
  Spawned as: bash -c "source ~/.nvm/nvm.sh && nvm list"
  Portable, but requires bash

Strategy B: Call nvm shim if it exists
  Some installs create an nvm binary wrapper

Strategy C: Parse ~/.nvm/alias/default and ~/.nvm/versions/node/*
  Read the filesystem directly without invoking nvm at all
  Most reliable for listing/detecting versions
  Use Strategy A only for install/uninstall
```

---

## 6. Node.js Version Resolution

### The nodejs.org API

```
GET https://nodejs.org/dist/index.json
```

Returns an array of ALL Node.js releases. Each entry looks like:

```json
{
  "version": "v22.5.0",
  "date": "2024-07-17",
  "files": ["..."],
  "npm": "10.8.2",
  "v8": "12.7.130.2",
  "lts": false,
  "security": false
}
```

- `lts: false` means it's a Current (non-LTS) release
- `lts: "Iron"` means it IS an LTS release (the string is the codename)

### Resolution Rules

```
LTS Latest  = max(version) where lts !== false
Current     = max(version) where lts === false AND not a release candidate
LTS Active  = all versions where lts !== false AND EOL date > today
Maintenance = versions in security-only maintenance mode
```

### Caching

Cache the index.json response for 1 hour to avoid rate limits:
```
~/.nodeup/cache/node-dist-index.json
~/.nodeup/cache/node-dist-index.json.ttl
```

---

## 7. Global Package Migration

### Snapshot Format

```json
{
  "nodeVersion": "20.14.0",
  "snapshotDate": "2024-07-20T10:30:00Z",
  "manager": "fnm",
  "packages": [
    { "name": "typescript", "version": "5.4.2", "global": true },
    { "name": "nodemon",    "version": "3.1.0", "global": true },
    { "name": "pnpm",       "version": "9.1.0", "global": true }
  ]
}
```

Saved to: `~/.nodeup/snapshots/<manager>-<node-version>.json`

### Migration Logic

```
1. Switch to OLD version via manager
2. Run: npm list -g --depth=0 --json
3. Parse output → package list
4. Save snapshot to disk
5. Switch to NEW version via manager
6. For each package:
   a. npm install -g <name>@<version>    # pin exact (conservative)
      OR
      npm install -g <name>              # latest (aggressive, default)
   b. Verify: npm list -g <name>
   c. Record result: ✅ success | ❌ failed | ⚠️ installed but different version
7. Write migration report
```

### Packages to Skip

Some global "packages" are actually part of the Node.js install itself and should never be migrated:

```go
var skipPackages = map[string]bool{
    "npm":    true,  // managed by Node version itself
    "corepack": true,
    "npx":   true,
}
```

---

## 8. Architecture & Project Structure

```
nodeup/
├── cmd/
│   └── nodeup/
│       └── main.go              # Entrypoint, cobra root command
│
├── internal/                    # Not exported (internal use only)
│   ├── detector/
│   │   ├── detector.go          # DetectAll(), ResolveManager()
│   │   ├── fnm.go
│   │   ├── nvm.go
│   │   ├── volta.go
│   │   ├── asdf.go
│   │   ├── mise.go
│   │   ├── n.go
│   │   ├── nodenv.go
│   │   └── nvm_windows.go       # build tag: //go:build windows
│   │
│   ├── node/
│   │   ├── dist.go              # nodejs.org API client
│   │   ├── version.go           # Version parsing, LTS resolution
│   │   └── cache.go             # Response caching
│   │
│   ├── packages/
│   │   ├── global.go            # npm list -g parsing
│   │   ├── snapshot.go          # Save/load snapshots
│   │   └── migrate.go           # Migration logic
│   │
│   ├── config/
│   │   ├── config.go            # ~/.nodeup/config.yaml
│   │   └── defaults.go
│   │
│   ├── ui/
│   │   ├── prompt.go            # huh-based interactive prompts
│   │   ├── spinner.go           # bubbletea spinner for long ops
│   │   └── report.go            # Final summary table
│   │
│   └── platform/
│       ├── platform.go          # OS detection helpers
│       ├── shell.go             # Shell command execution helpers
│       └── paths.go             # XDG-compliant path resolution
│
├── .github/
│   ├── workflows/
│   │   ├── ci.yml               # Lint + test on every PR
│   │   └── release.yml          # GoReleaser on tag push
│   ├── ISSUE_TEMPLATE/
│   │   ├── bug_report.md
│   │   └── feature_request.md
│   └── PULL_REQUEST_TEMPLATE.md
│
├── docs/
│   ├── installation.md
│   ├── configuration.md
│   └── managers.md              # Per-manager notes
│
├── .goreleaser.yaml             # Build + publish config
├── .golangci.yml                # Linter config
├── go.mod
├── go.sum
├── Makefile
├── README.md
├── CHANGELOG.md
└── LICENSE                      # MIT
```

### Key Design Principles

- `internal/` packages cannot be imported by external projects (Go enforces this)
- Each version manager is its own file implementing the `Manager` interface
- Platform-specific code uses Go build tags: `//go:build windows`
- All user-facing strings go through `ui/` — never `fmt.Println` in business logic
- Config is read once at startup and passed down (no global state)

---

## 9. CLI Design & Commands

### Command Tree

```
nodeup
├── upgrade              # Main command — does the full flow
│   ├── --lts            # Upgrade LTS only
│   ├── --current        # Upgrade Current only
│   ├── --dry-run        # Show what would happen, no changes
│   ├── --no-migrate     # Skip package migration
│   ├── --no-cleanup     # Skip asking about old version removal
│   ├── --manager <name> # Force a specific manager
│   └── --yes            # Non-interactive, assume yes to all prompts
│
├── check                # Check what versions are available, no changes
│   └── --json           # Output as JSON
│
├── list                 # List installed Node versions (via detected manager)
│   └── --json
│
├── packages             # Manage global package snapshots
│   ├── snapshot         # Take a snapshot of current global packages
│   ├── list             # List packages for a Node version
│   ├── restore          # Re-install from a saved snapshot
│   └── diff             # Show diff between two snapshots
│
├── config               # Manage nodeup config
│   ├── set <key> <val>
│   ├── get <key>
│   └── show
│
└── version              # Print nodeup version
    └── --check          # Check if nodeup itself has a newer release
```

### Sample Output

```
$ nodeup upgrade

  nodeup v1.2.0

  Detecting version managers...
  ✓ Found: fnm (v1.35.1)

  Fetching Node.js release data...
  ✓ LTS:     v20.15.0 (Iron)
  ✓ Current: v22.5.0

  Installed versions:
    v18.20.3  (will be superseded by LTS)
    v20.14.0  (outdated LTS — 20.15.0 available)

  Plan:
  ─────────────────────────────────────────────────────
  [1] Upgrade LTS:     20.14.0 → 20.15.0
      Global packages: typescript@5.4.2, nodemon@3.1.0
  [2] Install Current: 22.5.0 (new)
      Migrate from:    20.14.0 snapshot
  ─────────────────────────────────────────────────────

  Proceed? (Y/n): Y

  [1/2] Upgrading LTS to 20.15.0...
    → Snapshotting packages from 20.14.0      ✓
    → Installing 20.15.0 via fnm              ✓
    → Migrating typescript@5.4.2              ✓
    → Migrating nodemon@3.1.0                 ✓
    → Setting 20.15.0 as LTS default          ✓

  [2/2] Installing Current 22.5.0...
    → Installing 22.5.0 via fnm               ✓
    → Migrating typescript@5.4.2              ✓
    → Migrating nodemon@3.1.0                 ✓

  Remove old versions?
    20.14.0 (no longer default) → Remove? (y/N): y
    18.20.3 → Remove? (y/N): n

  ─────────────────────────────────────────────────────
  Done in 43s
  Active: Node.js v20.15.0 (LTS Iron) | npm v10.7.0
  ─────────────────────────────────────────────────────
```

### `nodeup.yaml` Config File

```yaml
# ~/.nodeup/config.yaml
manager: fnm              # lock to a specific manager
track:
  lts: true               # track LTS upgrades
  current: false          # don't track Current
packages:
  migrate: true
  strategy: exact         # "exact" | "latest"
  skip:
    - npm
    - corepack
cleanup:
  auto: false             # never auto-remove old versions
  prompt: true
cache:
  ttl: 3600               # seconds
```

---

## 10. Edge Cases & Error Handling

### Detection Edge Cases

| Scenario | Handling |
|---|---|
| No manager found | Clear error: "No Node.js version manager detected. Install fnm, nvm, or volta first." |
| Manager found but Node not installed | "fnm detected but no Node versions installed. Run: fnm install --lts" |
| Multiple managers found | Interactive prompt: "We found fnm and nvm. Which do you want to use?" |
| System Node (installed via pkg manager) | Detect `/usr/local/bin/node` not under manager path, warn: "System Node detected but not managed by a version manager. nodeup cannot upgrade it." |
| Manager binary exists but broken | Capture stderr, report: "fnm found but returned an error: <message>" |

### Network Edge Cases

| Scenario | Handling |
|---|---|
| No internet | Use cached index.json if fresh enough. Error if stale: "No internet and cache is >24h old." |
| nodejs.org rate limit | Retry with exponential backoff (3 retries) |
| Corporate proxy | Respect HTTP_PROXY / HTTPS_PROXY environment variables |
| Partial JSON response | Error out, don't corrupt state |

### Package Migration Edge Cases

| Scenario | Handling |
|---|---|
| Package incompatible with new Node | Log as ⚠️ warning, continue with others. Report at end. |
| Package install fails (network) | Retry once. Log failure. Include in migration report. |
| Package was installed from git URL | npm list shows the git URL — install it as-is |
| Package has no version (linked) | Skip it with a note: "myproject is a linked package, skipping" |
| Migrating to much newer Node | Warn if major Node version gap > 2 (e.g., 16 → 22) |
| npm itself is being upgraded | Skip npm from migration; it comes bundled |
| Packages with native addons | Warn: these may need rebuild. `npm rebuild` hint. |

### Platform Edge Cases

| Scenario | Handling |
|---|---|
| Windows + nvm-windows | `nvm.exe` works as a real binary, different command syntax. Needs `nvm arch 64` consideration. |
| Windows path separators | Use `filepath.Join()` everywhere, never hardcode `/` |
| Windows + spaces in path | Quote all paths in shell commands |
| macOS + Apple Silicon vs Intel | GoReleaser builds `darwin/arm64` and `darwin/amd64` + universal binary |
| Linux + flatpak/snap Node | Detect if node binary is inside a flatpak/snap path and warn |
| Permission denied on global npm | Suggest `--prefix` fix or sudo warning |
| Shell not in PATH (nvm) | Source ~/.nvm/nvm.sh explicitly before each nvm command |

### State Corruption Edge Cases

| Scenario | Handling |
|---|---|
| Install succeeds, migration fails | Node is installed. Migration report saved. User can `nodeup packages restore` |
| Interrupted mid-migration | On next run: detect partial state from snapshot + migration log |
| Disk full during install | Catch error, suggest cleaning old versions |
| Concurrent nodeup runs | Lock file: `~/.nodeup/nodeup.lock` (check + fail fast) |

---

## 11. Git & GitHub Workflow

### GitHub Flow (the model)

```
main ─────────────────────────────────────────────────────────────►
      │                    │                    │
      │ feat/detect-volta  │ fix/nvm-source     │ v1.1.0 tag
      └──────────┐         └────────┐           │
                 │ PR               │ PR         │
                 └──────────────────┘           tag → GoReleaser
```

Rules:
- `main` is **protected** — no direct pushes
- Every change goes through a **PR**, even solo work
- `main` is always in a releasable state
- Tags trigger releases (`v1.0.0`, `v1.1.0`, `v2.0.0`)

### Branch Naming Convention

```
feat/<scope>/<short-description>
fix/<scope>/<short-description>
chore/<scope>/<short-description>
docs/<short-description>
test/<scope>/<short-description>
ci/<short-description>
refactor/<scope>/<short-description>
```

**Real examples:**
```
feat/detector/add-volta-support
feat/detector/add-mise-support
feat/packages/add-yarn-global-migration
fix/nvm/handle-bash-source-on-zsh
fix/windows/path-separator-in-nvm-windows
chore/deps/update-cobra-v1.9
docs/add-homebrew-install-guide
ci/add-windows-runner
test/manager/fnm-integration-tests
refactor/detector/extract-manager-interface
```

### PR Workflow

```
1. Create branch from main:
   git checkout main && git pull
   git checkout -b feat/detector/add-volta-support

2. Commit with conventional format (see Section 12)

3. Push and open PR:
   git push -u origin feat/detector/add-volta-support
   gh pr create --title "feat(detector): add Volta support" --body "..."

4. CI runs:
   - golangci-lint
   - go test ./...
   - go build (matrix: linux/mac/windows)

5. Merge via "Squash and Merge" to keep main history clean

6. Delete branch after merge
```

### Release Flow

```
1. All features for the release are merged to main

2. Update CHANGELOG.md:
   git checkout -b chore/release/v1.1.0
   # edit CHANGELOG.md
   git commit -m "chore(release): prepare v1.1.0"
   # open PR, merge

3. Tag on main:
   git checkout main && git pull
   git tag -a v1.1.0 -m "Release v1.1.0"
   git push origin v1.1.0

4. GitHub Actions: release.yml fires
   GoReleaser builds + publishes binaries
   GitHub Release created automatically
   Homebrew formula auto-updated
```

---

## 12. Conventional Commits

### Format

```
<type>(<scope>): <short description>

[optional body]

[optional footer(s)]
```

### Types

| Type | When to use |
|---|---|
| `feat` | New feature (triggers MINOR version bump) |
| `fix` | Bug fix (triggers PATCH version bump) |
| `docs` | Documentation only |
| `chore` | Maintenance, dependency updates |
| `test` | Adding or fixing tests |
| `ci` | CI/CD changes |
| `refactor` | Code restructure, no behavior change |
| `perf` | Performance improvement |
| `style` | Formatting, no logic change |
| `build` | Build system changes |

### Scopes (for this project)

```
detector    # Manager detection logic
manager     # Individual manager implementations (fnm, nvm, etc.)
packages    # Global package migration
node        # nodejs.org API / version resolution
config      # Config file handling
ui          # Terminal output / prompts
platform    # OS / platform-specific code
deps        # Dependency updates
release     # Release process
```

### Real Commit Examples

```bash
# Feature commits
git commit -m "feat(detector): add Volta detection via VOLTA_HOME env var"
git commit -m "feat(manager): implement fnm install and uninstall commands"
git commit -m "feat(packages): add snapshot save/restore for global packages"
git commit -m "feat(ui): add interactive manager selection prompt"
git commit -m "feat(node): cache nodejs.org dist index with TTL"

# Fix commits
git commit -m "fix(nvm): source nvm.sh in bash -i context for shell function"
git commit -m "fix(platform): handle Windows paths with spaces in quotes"
git commit -m "fix(packages): skip npm and corepack from migration list"
git commit -m "fix(detector): prefer fnm over nvm when both are detected"

# Chore / CI commits
git commit -m "chore(deps): update cobra from v1.8.0 to v1.9.0"
git commit -m "ci: add matrix for windows/linux/macos runners"
git commit -m "chore(release): prepare v1.0.0 changelog"

# Breaking change (triggers MAJOR version bump)
git commit -m "feat(config)!: rename manager_preference to manager in config

BREAKING CHANGE: config key renamed from manager_preference to manager.
Update your ~/.nodeup/config.yaml accordingly."
```

### Enforcing Conventional Commits

Use `commitlint` locally and in CI:

```yaml
# .commitlintrc.yml
extends:
  - '@commitlint/config-conventional'
rules:
  scope-enum:
    - 2
    - always
    - [detector, manager, packages, node, config, ui, platform, deps, release]
```

```yaml
# .github/workflows/ci.yml (snippet)
- name: Lint commits
  uses: wagoid/commitlint-github-action@v5
```

---

## 13. Versioning Strategy

### Semantic Versioning (SemVer)

```
v<MAJOR>.<MINOR>.<PATCH>

MAJOR: Breaking change (config format change, removed command, changed output format)
MINOR: New feature (new manager support, new command, new flag)
PATCH: Bug fix (edge case fix, wrong output, crash fix)
```

### Release Cadence

```
v0.1.0  — Alpha: core flow works for fnm + macOS only
v0.2.0  — Add nvm support
v0.3.0  — Add Windows support (nvm-windows)
v0.4.0  — Add Volta support
v0.5.0  — Add ASDF/Mise support
v0.6.0  — Add packages diff, restore commands
v0.7.0  — Add config file support
v0.8.0  — Add dry-run mode
v0.9.0  — Beta: polish UI, full test coverage
v1.0.0  — Stable: all planned managers supported, docs complete
```

### Pre-release Tags

```
v1.0.0-alpha.1    # early testers
v1.0.0-beta.1     # wider testing
v1.0.0-rc.1       # release candidate
v1.0.0            # stable
```

### CHANGELOG.md Format (Keep a Changelog)

```markdown
# Changelog

All notable changes to this project will be documented in this file.
The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

## [Unreleased]

## [1.1.0] - 2024-08-15
### Added
- Volta version manager support (#42)
- `--dry-run` flag for upgrade command (#38)

### Fixed
- nvm detection fails on zsh when NVM_DIR not exported (#40)

## [1.0.0] - 2024-07-20
### Added
- Initial stable release
- fnm, nvm support
- Global package migration
- macOS, Linux, Windows support
```

---

## 14. CI/CD with GitHub Actions

### ci.yml — Runs on every PR and push to main

```yaml
name: CI

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

jobs:
  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.22'
      - uses: golangci/golangci-lint-action@v4

  test:
    strategy:
      matrix:
        os: [ubuntu-latest, macos-latest, windows-latest]
    runs-on: ${{ matrix.os }}
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.22'
      - run: go test ./... -v -race -coverprofile=coverage.txt
      - uses: codecov/codecov-action@v4

  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.22'
      - run: go build ./cmd/nodeup
```

### release.yml — Runs on tag push (v*.*.*)

```yaml
name: Release

on:
  push:
    tags:
      - 'v*.*.*'

jobs:
  release:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0   # Full history for changelog
      - uses: actions/setup-go@v5
        with:
          go-version: '1.22'
      - uses: goreleaser/goreleaser-action@v6
        with:
          version: latest
          args: release --clean
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          HOMEBREW_TAP_TOKEN: ${{ secrets.HOMEBREW_TAP_TOKEN }}
```

---

## 15. Publishing & Distribution

### GoReleaser Configuration

```yaml
# .goreleaser.yaml
version: 2

before:
  hooks:
    - go mod tidy

builds:
  - id: nodeup
    main: ./cmd/nodeup
    binary: nodeup
    env:
      - CGO_ENABLED=0
    goos:
      - linux
      - windows
      - darwin
    goarch:
      - amd64
      - arm64
    ignore:
      - goos: windows
        goarch: arm64

archives:
  - format: tar.gz
    format_overrides:
      - goos: windows
        format: zip
    name_template: '{{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}'

checksum:
  name_template: 'checksums.txt'

brews:
  - name: nodeup
    repository:
      owner: dipto0321
      name: homebrew-tap
      token: "{{ .Env.HOMEBREW_TAP_TOKEN }}"
    homepage: "https://github.com/dipto0321/nodeup"
    description: "Automated Node.js version upgrade with global package migration"
    license: "MIT"

scoops:
  - repository:
      owner: dipto0321
      name: scoop-bucket
    homepage: "https://github.com/dipto0321/nodeup"
    description: "Automated Node.js version upgrade with global package migration"
    license: MIT

changelog:
  use: git
  sort: asc
  filters:
    exclude:
      - "^docs:"
      - "^chore:"
      - "^test:"
```

### Distribution Channels Summary

| Channel | Command | Audience |
|---|---|---|
| GitHub Releases | Download binary | All platforms |
| Homebrew | `brew install dipto0321/tap/nodeup` | macOS/Linux |
| Scoop | `scoop bucket add dipto0321 ...` then `scoop install nodeup` | Windows |
| npm wrapper | `npm install -g nodeup` | Any Node user |
| Snap | `snap install nodeup` | Ubuntu/Linux |
| AUR (v2) | `yay -S nodeup` | Arch Linux |

### npm Wrapper Package

Even though the tool is written in Go, publish a thin npm package that downloads the correct binary at postinstall:

```json
{
  "name": "nodeup",
  "version": "1.0.0",
  "description": "Automated Node.js version upgrade with global package migration",
  "bin": { "nodeup": "bin/nodeup" },
  "scripts": {
    "postinstall": "node scripts/install.js"
  }
}
```

The `install.js` detects `process.platform` + `process.arch`, downloads the correct binary from the GitHub Release, and saves it to `bin/nodeup`. This lets developers install with: `npm install -g nodeup` — very familiar UX.

### Required GitHub Repos

```
github.com/dipto0321/nodeup            # Main repo
github.com/dipto0321/homebrew-tap      # Homebrew tap (auto-updated by GoReleaser)
github.com/dipto0321/scoop-bucket      # Scoop bucket (auto-updated by GoReleaser)
```

---

## 16. Phased Execution Plan

### Phase 0 — Scaffolding (Day 1)
- [ ] `mkdir nodeup && cd nodeup && git init`
- [ ] `go mod init github.com/dipto0321/nodeup`
- [ ] Create directory structure
- [ ] Add `cobra` root command with `version` subcommand
- [ ] Add GitHub repository, branch protection on `main`
- [ ] Add `.github/workflows/ci.yml`
- [ ] Add `.goreleaser.yaml` skeleton
- [ ] Add `.golangci.yml`
- [ ] Add `LICENSE`, `README.md`, `CHANGELOG.md`
- [ ] First commit: `chore: initial project scaffolding`
- [ ] First tag: `v0.1.0-alpha.1`

### Phase 1 — Detection Engine (Branch: `feat/detector/core`)
- [ ] Implement `Manager` interface
- [ ] Implement fnm detector + commands
- [ ] Implement nvm detector + shell sourcing strategy
- [ ] Write unit tests for detection logic
- [ ] Add `nodeup list` command

### Phase 2 — Node Version Resolution (Branch: `feat/node/version-api`)
- [ ] Implement nodejs.org API client
- [ ] LTS and Current resolution
- [ ] Response caching with TTL
- [ ] Add `nodeup check` command

### Phase 3 — Package Migration (Branch: `feat/packages/migration`)
- [ ] `npm list -g --depth=0 --json` parsing
- [ ] Snapshot save/load
- [ ] Migration logic with per-package result tracking
- [ ] Add `nodeup packages snapshot/list/restore`

### Phase 4 — Upgrade Command (Branch: `feat/upgrade/core`)
- [ ] Wire detection → version resolution → snapshot → install → migrate
- [ ] Interactive prompts via `huh`
- [ ] Progress spinner for long operations
- [ ] Final summary report
- [ ] `--dry-run` flag

### Phase 5 — Additional Managers
- [ ] Volta (`feat/detector/add-volta`)
- [ ] ASDF (`feat/detector/add-asdf`)
- [ ] Mise (`feat/detector/add-mise`)
- [ ] n (`feat/detector/add-n`)
- [ ] nvm-windows (`feat/detector/add-nvm-windows`)

### Phase 6 — Config & Polish
- [ ] `~/.nodeup/config.yaml` support
- [ ] `nodeup config` subcommand
- [ ] Multi-manager prompt
- [ ] Windows path handling
- [ ] Error messages + help text polish

### Phase 7 — Release v1.0.0
- [ ] Full test coverage (unit + integration)
- [ ] README with all install methods
- [ ] CHANGELOG complete
- [ ] GoReleaser configured
- [ ] Homebrew tap repo created
- [ ] Scoop bucket repo created
- [ ] npm wrapper package
- [ ] `v1.0.0` tag pushed → GitHub Release auto-created

---

## Quick Reference: Day 1 Commands

```bash
# Init repo
mkdir nodeup && cd nodeup
git init
git checkout -b main

# Go module
go mod init github.com/dipto0321/nodeup

# Install core deps
go get github.com/spf13/cobra@latest
go get github.com/charmbracelet/huh@latest
go get github.com/charmbracelet/lipgloss@latest
go get github.com/Masterminds/semver/v3@latest
go get github.com/tidwall/gjson@latest

# Create remote
gh repo create dipto0321/nodeup --public --source=. --remote=origin

# First commit
git add .
git commit -m "chore: initial project scaffolding"
git push -u origin main

# Protect main branch
gh api repos/dipto0321/nodeup/branches/main/protection \
  --method PUT \
  --input protection.json
```