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
- [ ] npm wrapper published to registry (Phase 7)

## Post-release

- [ ] GitHub Discussion / announcement posted
- [ ] Any release-blocker issues filed as follow-up
- [ ] `main` branch advanced to next version in CHANGELOG.md