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

# 2. Log in to npmjs.com. Since the Dec 2025 token changes,
#    `npm login` produces a short-lived session token (~2 hours)
#    rather than a long-lived classic token, so re-login is normal
#    after breaks. It will prompt for an OTP from your
#    authenticator app.
npm login

# 3. From the wrapper directory, publish. npm prompts for a fresh
#    OTP at publish time (separate from the login OTP) — keep your
#    authenticator open.
cd nodeup-npm
npm publish
cd ..
```

**Required once, not per release:**

- An `npmjs.com` account with publish rights on the `nodeup-cli`
  package name. The bare `nodeup` name is owned by an unrelated
  2015 package (`romanmt/nodeup`, "a simple cluster implementation
  for node") so we ship under `nodeup-cli`. `npm publish` will fail
  with `You do not have permission to publish "nodeup-cli"` if the
  name isn't claimed yet on your account.
- **2FA enabled** on the npm account, with an authenticator-app
  factor (TOTP). Configure under
  `https://www.npmjs.com/settings/<your-username>/security` →
  Two-Factor Authentication → **Authenticator app**. SMS and
  email-only are not accepted for publish. Save the recovery codes
  in your password manager.

**Repeat for every wrapper version bump.** The flow is the same
when the wrapper pins to a new `binaryVersion`: confirm the GitHub
release exists, `npm pack --dry-run`, then `npm publish`.

**Can I automate it?** Yes, but not for the very first publish. The
current npm auth model (since the Dec 9, 2025 token changes)
offers two options — pick one before wiring up `release.yml`:

**Option A — OIDC Trusted Publishing (recommended).** GitHub
Actions can publish without a long-lived secret. The runner
exchanges its built-in OIDC token for a one-hour publish token
scoped to the `nodeup-cli` package. Per-package config lives in
npm's "Trusted Publisher" UI.

1. After the first manual `npm publish` succeeds, open
   <https://www.npmjs.com/package/nodeup-cli/access> (or the
   "Trust" tab on the package page) and add a trusted publisher:
   - Provider: **GitHub Actions**
   - Repository: `dipto0321/nodeup`
   - Workflow: `release.yml`
   - Environment: *(leave blank unless you use one)*
2. Add a publish step to `.github/workflows/release.yml` after
   the GoReleaser job:

   ```yaml
   - name: Publish npm wrapper (OIDC)
     if: startsWith(github.ref, 'refs/tags/v')
     working-directory: ./nodeup-npm
     run: npm publish --provenance --access public
   ```

   No secret is needed. The OIDC exchange happens automatically
   when `GITHUB_TOKEN` is present. The publish token is short-lived
   (~1 hour) and tied to a specific workflow run, so leaking it is
   not a concern.

**Option B — Granular access token as `NPM_TOKEN`.** Browser-only
flow (npm doesn't yet support granular-token creation via the CLI
as of writing). Use this if you want to keep auth out of GitHub
Actions entirely, or if you're publishing from a non-GitHub CI.

1. Go to
   <https://www.npmjs.com/settings/<your-username>/tokens> and
   click **Generate New Token**.
2. Fill in:
   - **Name / description**: e.g. `nodeup-cli-ci-publish`
   - **Expiration**: 30 days (granular tokens max out at 90 days;
     rotate before expiry)
   - **Packages and scopes**: select `nodeup-cli` only (not `*`,
     not unscoped — limit the blast radius)
   - **Permissions**: `Read and write` on packages
   - **Bypass 2FA**: ON (so the CI publish doesn't need an OTP)
3. The token shows once. Copy it.
4. Add it as a repo secret:

   ```bash
   gh secret set NPM_TOKEN --repo dipto0321/nodeup
   # paste the token at the prompt
   ```

5. Add a publish step that uses it:

   ```yaml
   - name: Publish npm wrapper (token)
     if: startsWith(github.ref, 'refs/tags/v')
     working-directory: ./nodeup-npm
     run: npm publish --provenance --access public
     env:
       NODE_AUTH_TOKEN: ${{ secrets.NPM_TOKEN }}
   ```

**Which one?** A unless you have a reason for B. A leaves no
secret to rotate, no scope to limit, and no token to leak. B
exists for non-GitHub CI and for when you want a human-readable
audit trail of which token published which version.

## Post-release

- [ ] GitHub Discussion / announcement posted
- [ ] Any release-blocker issues filed as follow-up
- [ ] `main` branch advanced to next version in CHANGELOG.md