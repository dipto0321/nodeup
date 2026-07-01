# Supported Version Managers

All 8 managers listed below are fully detected by `nodeup` as of v1.0.0
(macOS, Linux, and Windows where applicable). The matrix is the source of
truth — keep it in sync with `internal/detector/registry_*.go` and the
per-manager files (`fnm.go`, `nvm.go`, `volta.go`, `asdf.go`, `mise.go`,
`n.go`, `nodenv.go`, `nvm_windows.go`).

nodeup auto-detects the following version managers. Detection runs in
priority order — earlier managers win when multiple are installed.

| Manager | Platforms | Detection |
|---|---|---|
| [fnm](https://github.com/Schniz/fnm) | macOS, Linux, Windows | `fnm` on PATH, `FNM_DIR`, `~/Library/Application Support/fnm` (macOS) / `~/.local/share/fnm` (Linux) / `%AppData%\fnm` (Windows) |
| [nvm](https://github.com/nvm-sh/nvm) | macOS, Linux | `NVM_DIR`, `~/.nvm/nvm.sh` |
| [Volta](https://volta.sh) | macOS, Linux, Windows | `volta` on PATH, `VOLTA_HOME`, `~/.volta` |
| [asdf](https://asdf-vm.com) | macOS, Linux | `asdf` on PATH, `ASDF_DIR`, `~/.asdf`, `nodejs` plugin |
| [mise](https://mise.jdx.dev) | macOS, Linux | `mise` on PATH, `node` plugin |
| [n](https://github.com/tj/n) | macOS, Linux | `n` on PATH, `N_PREFIX` |
| [nodenv](https://github.com/nodenv/nodenv) | macOS, Linux | `nodenv` on PATH, `~/.nodenv/shims` |
| [nvm-windows](https://github.com/coreybutler/nvm-windows) | Windows only | `nvm.exe` on PATH, registry entry |

## The nvm special case

`nvm` is a shell function, not a binary. nodeup uses three strategies:

1. **Strategy C (preferred for reads):** Parse `~/.nvm/alias/default` and
   `~/.nvm/versions/node/*` directly — most reliable.
2. **Strategy A (for installs/uninstalls):** Spawn `bash -c "source
   ~/.nvm/nvm.sh && nvm <cmd>"` so the function is loaded.
3. **Strategy B (fallback):** Use a binary wrapper if one is installed.

## Locking to a specific manager

```bash
nodeup config set manager fnm
```

Or via flag:

```bash
nodeup upgrade --manager fnm
```

## When `node` on PATH doesn't belong to a manager

nodeup's job is to swap Node versions inside a *version manager* it
manages — `fnm`, `nvm`, `Volta`, `asdf`, `mise`, `n`, `nodenv`, or
`nvm-windows`. If the `node` binary that lives first on your PATH
was installed some other way, nodeup can't safely replace it: the
other installer would just put it back on the next update.

Both `nodeup upgrade` and `nodeup check` classify the `node` on PATH
into one of these buckets:

| Kind             | Example paths                                                        | nodeup's behavior |
|------------------|----------------------------------------------------------------------|-------------------|
| `manager`        | `~/.fnm/node-versions/v22/bin/node`, `~/.nvm/versions/node/...`      | Manages normally — no warning. |
| `os-package`     | `/usr/bin/node`, `/bin/node`, `/opt/node/...`, `C:\Program Files\nodejs\node.exe`, `~/scoop/apps/nodejs/...` | Prints a warning to stderr (upgrade) or table (check). The platform-specific hint names the right upgrade tool: `sudo apt upgrade nodejs`, `winget upgrade Node.js`, etc. |
| `snap`           | `/snap/bin/node`, `/snap/node/<rev>/bin/node`                        | Warns. Run `snap refresh node`. |
| `flatpak`        | `/var/lib/flatpak/runtime/node/...`, `/usr/libexec/flatpak/...`       | Warns. Run `flatpak update` (or uninstall the flatpak and let nodeup manage a manager install instead). |
| `homebrew-core`  | `/usr/local/bin/node`, `/opt/homebrew/bin/node`, `/usr/local/Cellar/node/...`, `/opt/homebrew/Cellar/node/...`, `/home/linuxbrew/.linuxbrew/bin/node` | Warns. Run `brew upgrade node`, or `brew uninstall node` and let nodeup take over. |
| `unknown`        | Anything that doesn't match the patterns above                       | Soft warning: "nodeup does not recognize this layout." |

The classifier is path-based and runs in two passes: first it asks
"is this inside a manager's data dir?" (so `NVM_DIR=/usr/local/nvm`
beats `/usr/local`'s OS-shape); if not, the binary's install path is
classified by structural cues (`/snap/bin/`, `/usr/bin/`, the
Homebrew wrapper under `/usr/local/bin/node`, etc.).

If you want nodeup to take over a node that's currently a system
install, **uninstall the system copy first** (e.g., `brew uninstall
node`, `sudo apt remove nodejs`, `snap remove node`), then make sure
the manager's shim directory comes earlier on PATH than the system
bin dir (e.g., `fnm env --use-on-cd | source`, or add
`$HOME/.fnm/current/bin` to your shell init). Once `which node`
points at a binary inside the manager's data dir, nodeup's next
run will classify it as `manager` and proceed without warning.

> _Status: Phase 7 in progress — post-upgrade cleanup + native mutation commands per manager._

## Post-upgrade cleanup

When `nodeup upgrade` finishes installing the new LTS and/or Current
versions, it asks whether you want to delete the old ones. The
prompt is enabled by default (so the upgrade doesn't lose data
silently), but the behavior is configurable.

### Candidates

A version is a **cleanup candidate** if all three are true:

1. The manager has it installed (`<manager> list` / equivalent).
2. It's NOT one of the versions we just installed (new LTS, new
   Current).
3. It's NOT the version that's currently active on your shell. We
   detect this via `<manager> current` (or `<manager> version`,
   `<manager> list --format=plain`, etc. — see per-manager table
   below). If the manager doesn't expose a "current version" query,
   the exclusion is skipped (better to over-prompt than to leave a
   broken shell).

For example, if you had 18.20.4, 20.18.0, and 22.11.0 installed and
upgraded to 22.11.0 (LTS) + 24.15.0 (Current), with 20.18.0 active,
the candidates are: just **18.20.4**. The new versions and the
active version are off-limits.

### Flags

| Flag                     | Effect                                                          |
|--------------------------|-----------------------------------------------------------------|
| (no flag)                | Prompt: `y` deletes all / typed version deletes one / `N` skips |
| `--cleanup`              | Skip the prompt; auto-confirm deletion of every candidate       |
| `--cleanup-version <v>`  | Skip the prompt; only delete the specified versions (repeatable; pairs with `--cleanup`) |
| `--no-cleanup`           | Skip the prompt AND don't delete anything                       |
| `--yes`                  | Implies `--cleanup` for non-interactive runs (e.g., CI)         |

Config equivalents (`~/.nodeup/config.yaml`):

```yaml
cleanup:
  auto: false   # set true to skip the all-or-nothing prompt
  prompt: true  # set false to skip per-version confirm
```

Precedence (highest first):

1. `--no-cleanup` — never prompt, never delete
2. `--cleanup` (or `cleanup.auto: true` in config) — auto-confirm
3. `--cleanup-version <v>` — restrict to specific versions
4. `cleanup.prompt: false` — skip per-version confirm
5. Default — interactive prompt

### Per-manager behavior

| Manager        | Install cmd                  | Uninstall cmd           | Current query                       | Notes |
|----------------|------------------------------|--------------------------|--------------------------------------|-------|
| **fnm**        | `fnm install <v>`            | `fnm uninstall <v>`      | `fnm current`                        | Refuses to uninstall the active version. The prompt excludes it. |
| **nvm**        | `source nvm.sh && nvm install -s <v>` | `… nvm uninstall <v>` | `… nvm current` (`system` / `none` → "unknown") | Uses `-s` to suppress prompts. |
| **Volta**      | `volta install node@<v>`     | `volta uninstall node@<v>` | First `node@<v>` row of `volta list --format=plain` | `SetDefault` is a no-op (Volta pins per-project, not per-machine). |
| **asdf**       | `asdf install nodejs <v>`    | `asdf uninstall nodejs <v>` | `asdf current nodejs`               | Plugin name is `nodejs` (not `node`). |
| **mise**       | `mise install node@<v>`      | `mise uninstall node@<v>`   | `mise current node`                 | `SetDefault` writes to `~/.config/mise/config.toml` via `mise use --global`. |
| **n**          | `n install <v>`              | `n uninstall <v>`        | `n current` (n ≥ 8)                  | `SetDefault` is a no-op (n auto-uses the latest install). |
| **nodenv**     | `nodenv install <v>`         | `nodenv uninstall <v>`   | `nodenv version` (treats `system` as "unknown") | SetDefault writes to `~/.nodenv/version`. |
| **nvm-windows**| **unsupported**              | **unsupported**          | returns `ErrNVMWindowsNotImplemented` | `nvm-windows` doesn't expose a CLI for install/uninstall — the upgrade command leaves the install list alone and prints a clear note. |

For nvm-windows, the upgrade proceeds normally but the cleanup
prompt is suppressed with a note: "nvm-windows cleanup is not yet
implemented — leave the old versions in place or remove them
manually via `nvm uninstall <v>` from an elevated shell."

### Failure modes

- **Manager not on PATH** — the cleanup step is skipped entirely;
  the upgrade itself still completes (Install/SetDefault will
  have already failed loudly if the manager wasn't there).
- **Uninstall fails** (permission denied, currently-active
  version, locked file, ...) — we record the failure and continue
  with the next candidate. The summary at the end lists both
  successes and failures, so you can clean up the leftovers
  manually.
- **Ctrl-C mid-prompt** — we exit cleanly; the upgrade is already
  done, the prompt is the only thing that gets interrupted.
