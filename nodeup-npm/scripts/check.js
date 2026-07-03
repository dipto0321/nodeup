#!/usr/bin/env node
//
// preinstall.js — runs BEFORE `npm install` downloads anything.
//
// We check that the user's environment can actually run the Go binary
// we're about to fetch. The npm wrapper is a thin downloader; if the
// binary can't run on this OS/arch, we fail fast with a clear error
// instead of letting `npm install` succeed and `nodeup upgrade`
// failing later with a cryptic exec error.
//
// This script exits 0 on success (so npm proceeds) and 1 on failure
// (so npm aborts with the message visible in the install log).
//
// Mapping rules mirror the GoReleaser archive names in
// .goreleaser.yaml — see the `archives[].name_template` and
// `builds[].ignore` there:
//   {{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}
//
// IMPORTANT: We check the (platform, arch) PAIR against an explicit
// allowlist of combinations GoReleaser actually builds. Pre-fix
// (see #65), the script mapped each axis independently:
//
//   PLATFORM_TO_OS[process.platform] -> 'darwin' | 'linux' | 'windows'
//   ARCH_TO_GOARCH[process.arch]     -> 'amd64' | 'arm64'
//
// So `process.platform === 'win32'` + `process.arch === 'arm64'`
// mapped to (windows, arm64) and BOTH lookup tables returned a
// non-null value — the preinstall check passed. But .goreleaser.yaml
// line 45-47 explicitly excludes that combination from the build
// matrix:
//
//   ignore:
//     - goos: windows
//       goarch: arm64 # windows/arm64 is rare; add later if demanded
//
// The user therefore got a clean `npm install` followed by a 404
// from install.js when it tried to download a release archive that
// was never built — exactly the "unsupported platform silently
// proceeds past the gate meant to catch it early" failure mode the
// preinstall exists to prevent. Fix: enumerate the supported
// combinations directly.

'use strict';

// Each entry mirrors a row in .goreleaser.yaml's build matrix:
//   goos:   linux | darwin | windows
//   goarch: amd64 | arm64
// minus the explicit ignore `{ goos: windows, goarch: arm64 }`.
//
// Keys use Node.js's process.platform / process.arch values, NOT
// GoReleaser's goos / goarch names — Node reports `win32` where
// GoReleaser uses `windows`, and using Node's native identifiers
// keeps the lookup map consistent with the values it consumes.
const SUPPORTED = new Set([
  'linux/amd64',
  'linux/arm64',
  'darwin/amd64',
  'darwin/arm64',
  'win32/amd64',
  // 'win32/arm64' is intentionally absent — see .goreleaser.yaml
  // `builds[].ignore` (the goos: windows, goarch: arm64 entry
  // that mirrors Node's win32/arm64). Adding it here without
  // also dropping the goreleaser ignore would re-introduce the
  // 404 race the preinstall check is supposed to prevent.
]);

const platform = process.platform;
const arch = process.arch;

if (!SUPPORTED.has(`${platform}/${arch}`)) {
  console.error('');
  console.error('  nodeup: unsupported platform/architecture');
  console.error(`    platform=${platform}, arch=${arch}`);
  console.error('');
  console.error('  Built binaries are available for:');
  for (const k of SUPPORTED) console.error(`    ${k}`);
  console.error('');
  console.error('  See https://github.com/dipto0321/nodeup/releases');
  console.error('  for direct binary downloads, or open an issue if');
  console.error('  you need support for this platform.');
  console.error('');
  process.exit(1);
}

// All checks passed — let npm continue.
process.exit(0);