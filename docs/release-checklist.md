# Release Checklist

The release pipeline (`.github/workflows/release.yml` + GoReleaser v2)
fires automatically on a `v*.*.*` tag push. This checklist is the
human-side companion for shipping any release — including patch
releases off `main`. The first `nodeupx@1.0.1` bootstrap publish was
the only manual step ever required and is preserved below for
historical reference.

## Pre-release (on `chore/release/vX.Y.Z` branch)

- [ ] All planned issues / PRs for this version are merged to `main`
- [ ] `CHANGELOG.md` regenerated from conventional commits
- [ ] README install instructions verified against actual artifacts
- [ ] All docs (`docs/*.md`) reviewed for accuracy
- [ ] `make ci` green locally
- [ ] Cross-platform smoke test:
  - [ ] `nodeup check` on macOS (arm64)
  - [ ] `nodeup check` on Linux (amd64)
  - [ ] `nodeup check` on Windows (amd64) [if Windows runner available]
- [ ] Manual upgrade flow tested on at least one platform
- [ ] GitHub Action secrets present:
  - [ ] `HOMEBREW_TAP_TOKEN`
  - [ ] `SCOOP_BUCKET_TOKEN`
  - [ ] `CODECOV_TOKEN` (optional)

## Tagging

```bash
git checkout main
git pull
git tag -a vX.Y.Z -m "Release vX.Y.Z"
git push origin vX.Y.Z
```

## Post-tag (release.yml fires automatically)

- [ ] GitHub Release visible at /releases/tag/vX.Y.Z
- [ ] All 5 binaries attached + checksums.txt
- [ ] Homebrew formula pushed to `dipto0321/homebrew-tap`
- [ ] Scoop manifest pushed to `dipto0321/scoop-bucket`
- [ ] `publish-npm` job green; `npm view nodeupx version` matches
      the tag (automatic via OIDC Trusted Publishing — see below;
      **manual for the very first `nodeupx` publish only**)

### Publishing `nodeupx` (`nodeup-npm/`) to the npm registry

The npm wrapper lives in `nodeup-npm/` at the repo root. Publishing
happens **after** the GitHub release is up: the wrapper's
`postinstall` script (`scripts/install.js`) fetches the binary
matching the `binaryVersion` field in `package.json`, so the GitHub
release for that version **must already exist** — otherwise every
install 404s on the binary download.

**The decided flow is OIDC Trusted Publishing.** The `publish-npm`
job in `.github/workflows/release.yml` is already wired: on every
`v*.*.*` tag push it runs after GoReleaser, exchanges the runner's
OIDC token for a one-hour publish token scoped to `nodeupx`, and
runs `npm publish --provenance --access public`. No `NPM_TOKEN`
secret, nothing to rotate. From the second publish onward there is
**no manual npm step at all** — tag, wait, verify.

The only manual publish is the **first one**: npm's Trusted
Publisher config lives on the package's access page, which doesn't
exist until the package does. So the one-time bootstrap is:

#### One-time bootstrap (first `nodeupx` publish)

Account prerequisites:

- An `npmjs.com` account. The bare `nodeup` name is owned by an
  unrelated 2015 package (`romanmt/nodeup`, "a simple cluster
  implementation for node") so we ship under `nodeupx` (unclaimed
  as of 2026-07-04; the earlier `nodeup-cli@1.0.0` was unpublished).
- **2FA enabled** with an authenticator-app factor (TOTP), under
  `https://www.npmjs.com/settings/<your-username>/security` →
  Two-Factor Authentication → **Authenticator app**. SMS and
  email-only are not accepted for publish. Save the recovery codes
  in your password manager.

```bash
# 1. Sanity-check the tarball. This should print exactly 5 files:
#    LICENSE, README.md, package.json, and the two scripts.
#    No bin/, no .npmignore, no node_modules.
cd nodeup-npm
npm pack --dry-run

# 2. Log in. Since the Dec 2025 token changes, `npm login` produces
#    a short-lived session token (~2 hours); it prompts for an OTP
#    from your authenticator app.
npm login

# 3. Publish. npm prompts for a fresh OTP at publish time (separate
#    from the login OTP) — keep your authenticator open.
npm publish
cd ..
```

Then register the trusted publisher so every later version ships
from CI:

1. Open <https://www.npmjs.com/package/nodeupx/access> (or the
   "Trust" tab on the package page) and add a trusted publisher:
   - Provider: **GitHub Actions**
   - Repository: `dipto0321/nodeup`
   - Workflow: `release.yml`
   - Environment: *(leave blank — this repo doesn't use one)*
2. Nothing to change in the repo — `release.yml` already carries
   the `publish-npm` job (`id-token: write`, clean `.npmrc` with no
   `_authToken` line, Node 24 for npm ≥ 11.5.1). See the job's
   comments for the OIDC footguns it deliberately avoids.

#### Every subsequent release

Automatic. The tag push runs GoReleaser, then `publish-npm`
publishes the wrapper via OIDC with `--provenance` (npm shows the
"Built and signed on GitHub Actions" badge). Verify with
`npm view nodeupx version`. If the job fails with `ENEEDAUTH`,
check that the trust entry on npmjs.com still matches this repo,
the `release.yml` filename, and the GitHub user — that trio is the
whole auth surface.

**Token-based fallback** (not used here; only if publishing from a
non-GitHub CI): a granular access token, package-scoped to
`nodeupx`, 90-day max expiry, stored as `NPM_TOKEN` and passed as
`NODE_AUTH_TOKEN` to `npm publish`. Prefer the trusted publisher —
it leaves no secret to rotate or leak.

## Post-release

- [ ] GitHub Discussion / announcement posted
- [ ] Any release-blocker issues filed as follow-up
- [ ] `main` branch advanced to next version in CHANGELOG.md