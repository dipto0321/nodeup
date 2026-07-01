# Installation

All five channels below are wired up and tested. The GoReleaser config
that produces the binary archives lives in `.goreleaser.yaml`; the
Homebrew tap formula and Scoop bucket manifest are pushed
automatically on every `v*.*.*` tag (see `.github/workflows/release.yml`).

## Supported platforms

| OS | Architectures | Notes |
|---|---|---|
| macOS | arm64, amd64 | Universal binary if needed |
| Linux | arm64, amd64 | Most distros; glibc-based |
| Windows | amd64 | nvm-windows supported |

## Pick a channel

Each channel is a deliberate trade between **convenience**, **how
strongly you trust the package manager you already use**, and **how
much you want `nodeup` coupled to anything else on your system**. Use
the table below to pick — the per-channel sections below it show
exactly what to run.

| Channel | One-line install | Auto-updates? | Lives in… | Best for… | Avoid if… |
|---|---|---|---|---|---|
| **Homebrew** | `brew install dipto0321/tap/nodeup` | Yes — `brew upgrade` | `/opt/homebrew/bin/` (macOS) or `/home/linuxbrew/.linuxbrew/bin/` (Linux) | macOS / Linux devs already using Homebrew for tooling | You don't have Homebrew and don't want to set it up just for one tool |
| **Scoop** | `scoop install nodeup` (after adding the bucket) | Yes — `scoop update` | `%USERPROFILE%\scoop\apps\nodeup\` | Windows devs already using Scoop | You prefer winget / Chocolatey — neither is wired up today |
| **npm wrapper** | `npm install -g nodeup` | Semi — `npm update -g nodeup` only when a wrapper version bumps | Inside a Node install, on your global `node_modules/` path | JS devs who already treat `npm i -g` as the source of truth for CLIs; locked-down machines that block system installers but allow npm globals | You want a CLI that lives completely outside any Node install, or you want to track Go-binary releases the moment they tag (the wrapper lags by one publish) |
| **Direct binary** | `curl … \| tar xz` | No — you re-run to update | Wherever you put the extracted binary | CI pipelines, reproducible installs, lock-down environments without npm or brew | You want auto-updates; you'll forget to re-run |
| **From source** | `go install ./cmd/nodeup@latest` | No — re-run against a new tag | `$GOBIN` or `$GOPATH/bin` | `nodeup` contributors and Go developers who want HEAD | Anyone who doesn't have Go installed and isn't trying to hack on the tool |

## Channels

### Homebrew (macOS, Linux)

```bash
brew install dipto0321/tap/nodeup
```

**Who this is for:** developers on macOS or Linux who already use
Homebrew for dev tooling (`brew` is the package manager for
command-line apps, distinct from the App Store). The tap
[`dipto0321/homebrew-tap`](https://github.com/dipto0321/homebrew-tap)
holds the formula and is auto-pushed by GoReleaser on every
`v*.*.*` tag.

**Tradeoffs:** upgrades happen via `brew upgrade`, which all Homebrew
users already run on a schedule. The install lives in Homebrew's
prefix (typically `/opt/homebrew/bin` on Apple Silicon), completely
independent of any Node install — safe for `nvm`/`fnm`/`Volta` users
who don't want a Node coupling.

### Scoop (Windows)

```powershell
scoop bucket add dipto0321 https://github.com/dipto0321/scoop-bucket
scoop install nodeup
```

**Who this is for:** Windows developers already using Scoop as their
package manager. The bucket
[`dipto0321/scoop-bucket`](https://github.com/dipto0321/scoop-bucket)
holds the manifest and is auto-pushed by GoReleaser.

**Tradeoffs:** user-level install (no admin shell), upgrades via
`scoop update nodeup`. Same Node-decoupled shape as Homebrew.

### npm wrapper (any platform with Node ≥ 14)

```bash
npm install -g nodeup
```

**Who this is for:** developers who already treat `npm install -g` as
the canonical way to install CLIs. Common in JavaScript-heavy
projects where the team's onboarding script already runs `npm i -g
<tool1> <tool2> …`. Also a good fit when system package installs are
blocked but npm globals are allowed.

**Tradeoffs:**

- **Slight version lag.** The wrapper pins to the Go-binary version
  in its `binaryVersion` field. A new Go release needs a new wrapper
  publish — `npm update -g nodeup` only gets you a new Go binary
  after the wrapper version that pins to it ships. To jump to a
  brand-new release ahead of that, use a direct-binary install.
- **Coupled to a Node install.** The wrapper and binary live inside
  a Node install (the one you ran `npm i -g` against). For `nvm` /
  `fnm` / `Volta` users this means re-installing the wrapper after
  every Node bump — exactly what `nodeup` is supposed to remove.
  For those users, prefer Homebrew / Scoop / direct-binary.
- **No Node runtime needed at runtime.** The wrapper's
  `postinstall` script downloads a static Go binary; the `node`
  runtime is used only during install (for the script itself) and
  is never invoked by `nodeup` once installed.

See [`nodeup-npm/README.md`](../nodeup-npm/README.md) for the full
install / update / uninstall flow.

### Direct binary download

Best for CI, locked-down environments, and anyone who wants zero
install ceremony. Grab the archive for your OS/arch from the
[Releases page](https://github.com/dipto0321/nodeup/releases):

```bash
# macOS Apple Silicon
curl -L https://github.com/dipto0321/nodeup/releases/latest/download/nodeup_$(curl -s https://api.github.com/repos/dipto0321/nodeup/releases/latest | grep tag_name | cut -d'"' -f4 | tr -d v)_darwin_arm64.tar.gz | tar xz
sudo mv nodeup /usr/local/bin/

# Linux x86_64
curl -L https://github.com/dipto0321/nodeup/releases/latest/download/nodeup_*_linux_amd64.tar.gz | tar xz
sudo mv nodeup /usr/local/bin/

# Windows (PowerShell)
curl -L https://github.com/dipto0321/nodeup/releases/latest/download/nodeup_$(curl -s https://api.github.com/repos/dipto0321/nodeup/releases/latest | grep tag_name | cut -d'"' -f4 | tr -d v)_windows_amd64.zip -OutFile nodeup.zip
Expand-Archive nodeup.zip -DestinationPath .
# Move nodeup.exe somewhere on $PATH
```

**Who this is for:** anyone who wants a deterministic install — no
package manager metadata, no postinstall hook, no auto-update. Pin
the URL to a specific tag (not `latest`) to make the install
reproducible across CI runs.

**Tradeoffs:** you own the upgrade lifecycle. Re-run the command (or
script it) when you want a new version. The binary is fully
self-contained — no runtime, no Node, no manager.

### From source

```bash
go install github.com/dipto0321/nodeup/cmd/nodeup@latest
```

Requires Go 1.22+.

**Who this is for:** `nodeup` contributors and Go developers who want
to track `main` between tagged releases. The binary lands in
`$GOBIN` (or `$GOPATH/bin`), which Go is configured to put on
`$PATH` for you.

**Tradeoffs:** the slowest to upgrade (you recompile), but the
closest to the bleeding edge. Not appropriate for end users — they
should pick one of the channels above.

## Verifying

```bash
nodeup version
```

Should print a version, git commit, build date, and Go runtime info.

## Picking once and sticking

You can install via multiple channels at once if you want — they'll
write to different paths and the first one on `$PATH` wins. To
avoid the version-skew that causes, pick one and use it for upgrades
and uninstalls consistently:

- **Homebrew** owns upgrades → use `brew upgrade nodeup`.
- **Scoop** owns upgrades → use `scoop update nodeup`.
- **npm** owns upgrades → use `npm update -g nodeup`.
- **Direct binary** owns upgrades → re-run the curl one-liner (or
  pin a specific tag in CI).
- **From source** owns upgrades → `go install -u
  github.com/dipto0321/nodeup/cmd/nodeup@latest`.