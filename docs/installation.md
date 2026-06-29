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

## Channels

### Homebrew (macOS, Linux)

```bash
brew install dipto0321/tap/nodeup
```

### Scoop (Windows)

```powershell
scoop bucket add dipto0321 https://github.com/dipto0321/scoop-bucket
scoop install nodeup
```

### npm wrapper

```bash
npm install -g nodeup
```

### Direct binary

See the [Releases page](https://github.com/dipto0321/nodeup/releases).

### From source

Requires Go 1.22+.

```bash
go install github.com/dipto0321/nodeup/cmd/nodeup@latest
```

## Verifying

```bash
nodeup version
```

Should print a version, git commit, build date, and Go runtime info.