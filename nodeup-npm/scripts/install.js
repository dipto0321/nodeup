#!/usr/bin/env node
//
// install.js — runs on `npm install` (postinstall) AND `npm install -g`
// (postinstall). Downloads the nodeup Go binary that matches this
// package's `binaryVersion` field for the user's OS/arch, extracts
// it to ./bin/, and chmods it executable on POSIX.
//
// We deliberately pin to a specific version (the one in package.json's
// `binaryVersion` field) rather than fetching "latest" so that:
//   - npm install is reproducible: same package version always pulls
//     the same binary version.
//   - users don't get a surprise major-version upgrade by running
//     `npm update -g` and getting the latest tag.
//   - the Go binary and the wrapper move together — they bump in the
//     same PR, get tested together, ship together.
//
// Bumping the binary: edit package.json's `binaryVersion`, commit,
// `npm publish`. The GoRelease pipeline still has to push the matching
// v<binaryVersion> tag first (the wrapper download will 404 otherwise).
//
// Mapping rules mirror the GoReleaser archive names in
// .goreleaser.yaml — see the `archives[].name_template` there:
//   {{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}
//
// Format overrides (line 53-55 of .goreleaser.yaml):
//   - format_overrides: { goos: windows, format: zip }
// So unix archives are .tar.gz, windows is .zip.

'use strict';

const fs = require('fs');
const path = require('path');
const https = require('https');
const { execFileSync } = require('child_process');
const zlib = require('zlib');
const tar = require('tar'); // npm bundles tar; no extra dep

// ---- helpers --------------------------------------------------------------

function die(msg, code = 1) {
  console.error('');
  console.error('  nodeup: ' + msg);
  console.error('');
  process.exit(code);
}

function info(msg) {
  console.log('  nodeup: ' + msg);
}

function loadPackage() {
  const pkgPath = path.join(__dirname, '..', 'package.json');
  const pkg = JSON.parse(fs.readFileSync(pkgPath, 'utf8'));
  if (!pkg.binaryVersion) {
    die('package.json is missing the binaryVersion field');
  }
  return pkg;
}

function platformOsArch() {
  // mirrors preinstall.js — kept in sync by convention. We re-derive
  // here rather than reading a shared file so that each script is
  // self-contained and can be inspected in isolation.
  const platform = process.platform;
  const arch = process.arch;

  const PLATFORM_TO_OS = {
    darwin: 'darwin',
    linux: 'linux',
    win32: 'windows',
  };
  const ARCH_TO_GOARCH = {
    x64: 'amd64',
    arm64: 'arm64',
  };

  const os = PLATFORM_TO_OS[platform];
  const goarch = ARCH_TO_GOARCH[arch];
  if (!os || !goarch) {
    die(`unsupported platform/architecture: ${platform}/${arch}`);
  }
  return { os, goarch };
}

function archiveName(os, arch, version) {
  const ext = os === 'windows' ? 'zip' : 'tar.gz';
  return `nodeup_${version}_${os}_${arch}.${ext}`;
}

function downloadTo(url, destPath) {
  return new Promise((resolve, reject) => {
    info(`downloading ${url}`);
    const request = (targetUrl) => {
      https
        .get(targetUrl, { headers: { 'user-agent': 'nodeup-npm-wrapper' } }, (res) => {
          // GitHub releases redirect to S3. Follow one redirect (the
          // pattern is consistent enough that two-hop handling would
          // be overkill for this use case).
          if (res.statusCode === 302 || res.statusCode === 301) {
            const redirect = res.headers.location;
            if (!redirect) {
              reject(new Error(`redirect with no Location header from ${targetUrl}`));
              return;
            }
            request(redirect);
            return;
          }
          if (res.statusCode !== 200) {
            reject(
              new Error(
                `download failed: HTTP ${res.statusCode} from ${targetUrl}`
              )
            );
            return;
          }
          const out = fs.createWriteStream(destPath);
          res.pipe(out);
          out.on('finish', () => out.close(() => resolve(destPath)));
          out.on('error', reject);
        })
        .on('error', reject);
    };
    request(url);
  });
}

function extractTarGz(archivePath, outDir) {
  return tar.x({
    file: archivePath,
    cwd: outDir,
    strip: 0, // GoReleaser archives don't have a top-level wrapper dir
  });
}

function extractZip(archivePath, outDir) {
  // No native zip support in Node — shell out to `unzip`. Available
  // by default on Windows 10+ (PowerShell Expand-Archive) and via the
  // `unzip` package on most POSIX distros. If neither is present the
  // user gets a clear error from the spawned process.
  const isWindows = process.platform === 'win32';
  if (isWindows) {
    execFileSync(
      'powershell.exe',
      [
        '-NoProfile',
        '-NonInteractive',
        '-Command',
        `Expand-Archive -LiteralPath "${archivePath}" -DestinationPath "${outDir}" -Force`,
      ],
      { stdio: 'inherit' }
    );
  } else {
    execFileSync('unzip', ['-o', archivePath, '-d', outDir], { stdio: 'inherit' });
  }
}

function chmodExec(filePath) {
  if (process.platform === 'win32') return;
  fs.chmodSync(filePath, 0o755);
}

// ---- main -----------------------------------------------------------------

(async function main() {
  const pkg = loadPackage();
  const { os, goarch } = platformOsArch();
  const version = pkg.binaryVersion;
  const archive = archiveName(os, goarch, version);

  const repo = 'dipto0321/nodeup';
  const tag = `v${version}`;
  const downloadUrl = `https://github.com/${repo}/releases/download/${tag}/${archive}`;

  const tmpDir = fs.mkdtempSync(path.join(require('os').tmpdir(), 'nodeup-'));
  const archivePath = path.join(tmpDir, archive);
  const binDir = path.join(__dirname, '..', 'bin');
  const binaryDest = path.join(binDir, process.platform === 'win32' ? 'nodeup.exe' : 'nodeup');

  fs.mkdirSync(binDir, { recursive: true });

  try {
    await downloadTo(downloadUrl, archivePath);

    if (os === 'windows') {
      extractZip(archivePath, tmpDir);
    } else {
      await extractTarGz(archivePath, tmpDir);
    }

    // GoReleaser archives have `nodeup` (or `nodeup.exe`) at the root.
    const extractedName = process.platform === 'win32' ? 'nodeup.exe' : 'nodeup';
    const extractedPath = path.join(tmpDir, extractedName);
    if (!fs.existsSync(extractedPath)) {
      die(`archive did not contain ${extractedName} — unexpected layout`);
    }
    fs.renameSync(extractedPath, binaryDest);
    chmodExec(binaryDest);

    info(`installed nodeup v${version} (${os}/${goarch}) -> ${binaryDest}`);
  } finally {
    try {
      fs.rmSync(tmpDir, { recursive: true, force: true });
    } catch (_) {
      // best-effort cleanup; tmp will be reaped by the OS
    }
  }
})().catch((err) => {
  die(err && err.message ? err.message : String(err));
});