# Supported Version Managers

> _Placeholder doc. Full content lands in Phases 1 and 5 alongside the
> detector implementations._

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