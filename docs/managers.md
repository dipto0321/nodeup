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

> _Status: Phase 1 ✅ done — all 8 managers detected · last updated 2026-06-29_