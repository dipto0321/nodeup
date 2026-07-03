# nodeupx (npm wrapper)

> Thin npm wrapper around the [`nodeup`](https://github.com/dipto0321/nodeup)
> Go binary. Installs the right static binary for your OS/arch on
> `npm install -g nodeupx` and exposes `nodeup` on your `$PATH`.

This directory is the **npm distribution channel** for `nodeup`. The
package is published as **`nodeupx`** because the bare `nodeup`
name on npmjs.com is owned by an unrelated, dormant 2015 package
(`romanmt/nodeup`, "a simple cluster implementation for node").
The downloaded binary still ships as the `nodeup` CLI you know.

It does **not** contain the Go tool itself — it downloads the
matching binary from the [GitHub
release](https://github.com/dipto0321/nodeup/releases) that this
package's `binaryVersion` field points at.

## Install

```bash
npm install -g nodeupx
nodeup version
```

The `postinstall` script downloads a ~6 MB static Go binary into
`node_modules/nodeup/bin/` and symlinks it onto your global `$PATH`
as `nodeup`. No Go toolchain, no system services.

## Who should use this wrapper

**Pick the npm wrapper if you already live inside Node and want a
one-line install you trust from the npm registry.** It's especially
good when:

- You manage dev tooling via `npm` / `pnpm` / `yarn` and your team's
  onboarding doc already says `npm i -g <tool>`.
- You're on a machine where installing system packages (Homebrew,
  Scoop, apt) is restricted, but npm global installs are allowed.
- You want the install to **travel with your Node version** — when you
  upgrade Node and reinstall globals, `nodeup` reinstalls at the
  matching binary.

**Don't pick the npm wrapper if:**

- You want zero Node runtime coupling. Use **Homebrew** (macOS/Linux)
  or **Scoop** (Windows) — those install `nodeup` outside any Node
  install and update it independently.
- You maintain `nodeup` itself. Use `go install ./cmd/nodeup@latest`
  or **direct binary download** so you're not bound to whichever
  release the wrapper is pinned to.
- You're scripting the install in CI and want a deterministic binary
  with no postinstall network step. Use **direct binary download**
  against a pinned release tag.

## What it does on install

1. **`preinstall` (`scripts/check.js`)** — verifies your
   `process.platform` × `process.arch` is one of the matrix
   `nodeup` ships for (`darwin`/`linux`/`windows` × `amd64`/`arm64`).
   Exits non-zero so `npm` aborts if you ask for the wrapper on a
   platform we don't build for.
2. **`postinstall` (`scripts/install.js`)** — downloads
   `nodeup_<binaryVersion>_<os>_<arch>.{tar.gz,zip}` from the
   matching `v<binaryVersion>` GitHub release, extracts the binary
   into `node_modules/nodeup/bin/`, and chmods it executable. The
   `bin` field in `package.json` wires `nodeup` to that file.

`binaryVersion` is **pinned** in `package.json`. The wrapper does
**not** fetch "latest" — it fetches the exact release this package
version was tested against. A wrapper bump and the matching Go
release bump ship in the same commit.

## Updating

```bash
npm update -g nodeupx
```

You get a new wrapper version. If that wrapper pins a newer
`binaryVersion`, `npm` re-runs `postinstall` and downloads the new
binary. Old binaries are replaced; no leftover.

To jump to a brand-new release ahead of the next wrapper publish,
install the binary directly (see the main
[README](https://github.com/dipto0321/nodeup#installation)).

## Uninstall

```bash
npm uninstall -g nodeup
```

Removes the wrapper, the downloaded binary, and the `nodeup` symlink
from your global `$PATH`. `nodeup`'s runtime state at `~/.nodeup/`
is **not** touched — uninstalling the CLI keeps your snapshots and
config in place.

## License

[MIT](../LICENSE)