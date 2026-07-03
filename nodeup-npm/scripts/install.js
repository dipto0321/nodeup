#!/usr/bin/env node
//
// install.js — runs on `npm install` (postinstall) AND `npm install -g`
// (postinstall). Downloads the nodeup Go binary that matches this
// package's `binaryVersion` field for the user's OS/arch, verifies
// its SHA256 against the release's published checksums.txt, extracts
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
//
// Integrity / supply-chain hygiene (see #64):
//   - The download URL follows one redirect via `request()`. Redirect
//     targets must be on the `objects.githubusercontent.com` host
//     (GitHub Releases' S3-backed CDN); any other host is rejected.
//     This blocks the trivial MITM-via-redirect attack surface.
//   - Before extraction, we download `checksums.txt` from the same
//     release tag, parse out the SHA256 for our exact archive name,
//     and recompute the archive's hash as we stream it to disk. A
//     mismatch aborts the install before the archive is ever
//     extracted. This catches any silent substitution (TLS-
//     inspecting proxy, compromised CDN, manipulated DNS, etc.)
//     even if the binary itself doesn't execute.

'use strict';

const fs = require('fs');
const path = require('path');
const https = require('https');
const crypto = require('crypto');
const url = require('url');
const { execFileSync } = require('child_process');
// `tar` is an EXPLICIT runtime dependency declared in this package's
// package.json `dependencies` block. The pre-fix comment
// ("npm bundles tar; no extra dep") was incorrect: npm uses tar
// internally, but it is not guaranteed to be reachable from an
// installed package's script context — yarn/pnpm do not hoist
// npm's internals, and npm's modern flat-install behavior is
// incidental. Without the explicit dep, `require('tar')` fails
// immediately at module load time on yarn/pnpm installs (and on
// some npm versions) with `Cannot find module 'tar'`. See #63.
const tar = require('tar');

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

  const osName = PLATFORM_TO_OS[platform];
  const goarch = ARCH_TO_GOARCH[arch];
  if (!osName || !goarch) {
    die(`unsupported platform/architecture: ${platform}/${arch}`);
  }
  return { os: osName, goarch };
}

function archiveName(os, arch, version) {
  const ext = os === 'windows' ? 'zip' : 'tar.gz';
  return `nodeup_${version}_${os}_${arch}.${ext}`;
}

// ---- networking ----------------------------------------------------------
//
// `requestOnce` performs a single GET against `targetUrl`, returning the
// `http.IncomingMessage` whose status code has already been validated
// against `expectedStatus` (default 200). It does NOT follow redirects;
// callers must inspect `res.headers.location` themselves and validate
// the target before calling again.
//
// `httpsGet` is the same, but for an explicit (parsed) `url.URL`. We
// keep them split so callers that already have a URL object don't pay
// for the parse twice.

// Allowed redirect targets: GitHub Releases redirects to their S3-
// backed CDN. We allowlist that single host — anything else (a
// compromised CDN, a TLS-stripping proxy, a typo in `Location`) is
// rejected. See #64.
const ALLOWED_REDIRECT_HOSTS = new Set(['objects.githubusercontent.com']);

// GitHub release URL → redirect target. Two hops is the known maximum
// (`github.com` → `objects.githubusercontent.com`). Anything beyond
// that is either a bug or an attack.
const MAX_REDIRECT_HOPS = 2;

function httpsGet(targetUrl) {
  return new Promise((resolve, reject) => {
    const parsed = new url.URL(targetUrl);
    const req = https.get(
      parsed,
      { headers: { 'user-agent': 'nodeup-npm-wrapper' } },
      (res) => resolve(res)
    );
    req.on('error', reject);
  });
}

function followHops(startUrl, hopsLeft, acceptStatus) {
  // Walk a chain of GitHub-releases redirects, returning the first
  // response whose status code is in `acceptStatus`. Throws if the
  // chain leaves the allowlisted host set or exceeds `hopsLeft`.
  return (async function walk(currentUrl, hopsUsed) {
    const res = await httpsGet(currentUrl);
    if (acceptStatus.includes(res.statusCode)) {
      return res;
    }
    if (res.statusCode === 301 || res.statusCode === 302 || res.statusCode === 303 || res.statusCode === 307 || res.statusCode === 308) {
      if (hopsUsed >= hopsLeft) {
        throw new Error(`too many redirects (max ${hopsLeft}) from ${currentUrl}`);
      }
      const next = res.headers.location;
      if (!next) {
        throw new Error(`redirect with no Location header from ${currentUrl}`);
      }
      // Resolve relative → absolute.
      const nextUrl = new url.URL(next, currentUrl).toString();
      const nextHost = new url.URL(nextUrl).hostname;
      if (!ALLOWED_REDIRECT_HOSTS.has(nextHost)) {
        throw new Error(
          `refusing to follow redirect from ${currentUrl} to ${nextUrl}: ` +
            `host ${nextHost} is not in the allowlist`
        );
      }
      // Drain the body so the connection can close cleanly before
      // we walk further down the chain.
      res.resume();
      return walk(nextUrl, hopsUsed + 1);
    }
    throw new Error(
      `unexpected HTTP ${res.statusCode} from ${currentUrl}: ` +
        `wanted one of [${acceptStatus.join(', ')}]`
    );
  })(startUrl, 0);
}

// downloadTo fetches `url` and streams the body to `destPath`, while
// also piping the body through a SHA-256 hasher. When the stream
// finishes, the resolved value is `{ path: destPath, sha256: hex }`.
// Callers MUST verify the hash against an authoritative source before
// using the downloaded artifact.
function downloadTo(urlStr, destPath) {
  info(`downloading ${urlStr}`);
  return new Promise((resolve, reject) => {
    followHops(urlStr, MAX_REDIRECT_HOPS, [200])
      .then((res) => {
        const hasher = crypto.createHash('sha256');
        const out = fs.createWriteStream(destPath);
        res.on('data', (chunk) => hasher.update(chunk));
        res.on('error', reject);
        out.on('error', reject);
        out.on('finish', () => {
          out.close(() => resolve({ path: destPath, sha256: hasher.digest('hex') }));
        });
        res.pipe(out);
      })
      .catch(reject);
  });
}

// downloadText fetches a small text body, following the same redirect
// chain as downloadTo but returning the full body as a string. Used
// for checksums.txt (always tiny). Resolves to a string.
function downloadText(urlStr) {
  return new Promise((resolve, reject) => {
    followHops(urlStr, MAX_REDIRECT_HOPS, [200])
      .then((res) => {
        const chunks = [];
        res.setEncoding('utf8');
        res.on('data', (chunk) => chunks.push(chunk));
        res.on('error', reject);
        res.on('end', () => resolve(chunks.join('')));
      })
      .catch(reject);
  });
}

// parseChecksumsTxt turns the standard GoReleaser / `sha256sum -b`
// output (one `<hex>   <basename>` per line) into a Map keyed by
// the basename. Whitespace between the hash and the filename is
// either two spaces (GoReleaser convention) or `*  ` (binary mode
// flag, also accepted by coreutils when `sha256sum` is run with
// `--binary`). Both shapes decode the same.
function parseChecksumsTxt(body) {
  const out = new Map();
  for (const raw of body.split('\n')) {
    const line = raw.trim();
    if (line === '' || line.startsWith('#')) continue;
    // Match "<64-hex>  [<star>] [<filename>]". The optional `*`
    // marks binary mode and may be adjacent to the filename
    // (`sha256sum -b` style: `<hash> *<filename>`) or separated
    // by whitespace (`sha256sum --tag` style: `<hash> * <filename>`).
    // GoReleaser emits the plain two-space form. We accept all three.
    const m = /^([a-f0-9]{64})\s+\*?\s*(.+)$/.exec(line);
    if (!m) continue;
    out.set(m[2], m[1]);
  }
  return out;
}

// fetchExpectedHash downloads `checksums.txt` from the same release
// tag and returns the expected SHA256 for `archiveName`.
async function fetchExpectedHash(repo, tag, archiveName) {
  const checksumsUrl = `https://github.com/${repo}/releases/download/${tag}/checksums.txt`;
  info(`fetching checksums from ${checksumsUrl}`);
  const body = await downloadText(checksumsUrl);
  const checksums = parseChecksumsTxt(body);
  const expected = checksums.get(archiveName);
  if (!expected) {
    throw new Error(
      `checksums.txt from ${tag} does not include an entry for ${archiveName}; ` +
        `available entries: ${Array.from(checksums.keys()).join(', ') || '(none)'}`
    );
  }
  return expected;
}

// ---- extraction ----------------------------------------------------------

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
    // Download the expected hash FIRST — fail fast if the release
    // didn't publish checksums.txt or our artifact isn't in it, so
    // we don't waste bytes pulling an archive we can't verify.
    const expectedHash = await fetchExpectedHash(repo, tag, archive);
    info(`expected SHA256 for ${archive}: ${expectedHash}`);

    // Download the archive, hashing as we stream to disk.
    const { sha256: actualHash } = await downloadTo(downloadUrl, archivePath);
    info(`computed SHA256 for ${archive}: ${actualHash}`);

    if (actualHash !== expectedHash) {
      // Delete the half-saved archive before aborting so a
      // forensic investigation of the failing install at least
      // doesn't leave the (potentially attacker-controlled)
      // bytes sitting in the user's tmp dir.
      try { fs.unlinkSync(archivePath); } catch (_) {}
      die(
        `SHA256 mismatch downloading ${archive}:\n` +
          `  expected: ${expectedHash}\n` +
          `  actual:   ${actualHash}\n` +
          `refusing to extract. This is most likely a TLS-stripping\n` +
          `proxy, a compromised CDN, or a corrupted download — do NOT\n` +
          `retry. File an issue at https://github.com/${repo}/issues.`
      );
    }

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
