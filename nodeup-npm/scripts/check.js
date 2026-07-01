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
// .goreleaser.yaml — see the `archives[].name_template` there:
//   {{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}

'use strict';

const PLATFORM_TO_OS = {
  darwin: 'darwin',
  linux: 'linux',
  win32: 'windows',
  freebsd: null, // not built; intentional
  openbsd: null, // not built; intentional
  sunos: null, // not built; intentional
};

const ARCH_TO_GOARCH = {
  x64: 'amd64',
  arm64: 'arm64',
  ia32: null, // not built
  arm: null, // not built
  ppc64: null, // not built
  s390x: null, // not built
};

const platform = process.platform;
const arch = process.arch;

const os = PLATFORM_TO_OS[platform];
const goarch = ARCH_TO_GOARCH[arch];

if (!os || !goarch) {
  console.error('');
  console.error('  nodeup: unsupported platform/architecture');
  console.error(`    platform=${platform}, arch=${arch}`);
  console.error('');
  console.error('  Built binaries are available for:');
  console.error('    OS:      darwin, linux, windows');
  console.error('    arch:    amd64, arm64');
  console.error('');
  console.error('  See https://github.com/dipto0321/nodeup/releases');
  console.error('  for direct binary downloads, or open an issue if');
  console.error('  you need support for this platform.');
  console.error('');
  process.exit(1);
}

// All checks passed — let npm continue.
process.exit(0);