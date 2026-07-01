# Release Checklist

The release pipeline (`.github/workflows/release.yml` + GoReleaser v2)
fires automatically on a `v*.*.*` tag push. This checklist is the
human-side companion — use it for the **first stable release** (v1.0.0),
and ad-hoc when shipping a patch release.

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
- [ ] All 6 binaries attached + checksums.txt
- [ ] Homebrew formula pushed to `dipto0321/homebrew-tap`
- [ ] Scoop manifest pushed to `dipto0321/scoop-bucket`
- [ ] npm wrapper published to the npm registry (see below)

### Publishing `nodeup-npm` to the npm registry

The npm wrapper lives in `nodeup-npm/` at the repo root and ships via
`npm publish` **after** the GitHub release is up. The wrapper's
`postinstall` script (`scripts/install.js`) fetches the binary
matching the `binaryVersion` field in `package.json`, so the GitHub
release for that version **must already exist** before publishing
the wrapper — otherwise every install 404s on the binary download.

```bash
# 1. Sanity-check the tarball before publishing. This should print
#    exactly 5 files: LICENSE, README.md, package.json, and the two
#    scripts. No bin/, no .npmignore, no node_modules.
cd nodeup-npm
npm pack --dry-run
cd ..

# 2. Log in to npmjs.com (one-time per machine; uses your npm account
#    credentials + email/password). After 2FA is enabled on the
#    account, `npm login` will ask for a one-time code from your
#    authenticator.
npm login

# 3. From the wrapper directory, publish. npm will prompt for the
#    OTP code on publish (not on login) — keep your authenticator
#    open.
cd nodeup-npm
npm publish
cd ..
```

**Required once, not per release:**

- An `npmjs.com` account with publish rights on the `nodeup` package
  name. The package name must be claimed on npm before the first
  publish — `npm publish` will fail with `You do not have permission
  to publish "nodeup"` if it isn't.
- **2FA enabled** on the npm account. Publishing to npmjs.com
  requires 2FA; configure it under
  `https://www.npmjs.com/settings/<your-username>/security`.
  Authenticator-app mode (`Authenticator` level) is the minimum —
  SMS and email-only modes are not accepted for publish.

**Repeat for every wrapper version bump.** The flow is the same
when the wrapper pins to a new `binaryVersion`: confirm the GitHub
release exists, `npm pack --dry-run`, then `npm publish`.

**Can I automate it?** Yes, but not for the very first publish. Add
a repo secret `NPM_TOKEN` (a publish-only automation token from
`https://www.npmjs.com/settings/<your-username>/tokens`) and a
release workflow step that runs `npm publish` from `nodeup-npm/` on
tag push. Until you've done at least one manual publish to confirm
the package name and the account are wired up, keep this step in
the human checklist.

## Post-release

- [ ] GitHub Discussion / announcement posted
- [ ] Any release-blocker issues filed as follow-up
- [ ] `main` branch advanced to next version in CHANGELOG.md