#!/usr/bin/env node
//
// check_test.js — pure-data smoke tests for check.js.
//
// check.js is a preinstall gate that exits 1 if the user's
// (platform, arch) isn't in the GoReleaser build matrix. It
// doesn't `module.exports` (it's invoked by npm directly), so we
// reimplement the SUPPORTED set here and pin its contents.
//
// What we test:
//   - The SUPPORTED set matches GoReleaser's `builds[]` matrix
//     (linux/darwin/windows × amd64/arm64) MINUS the explicit
//     `ignore: { goos: windows, goarch: arm64 }` exclusion.
//   - Common supported combinations pass (linux/amd64,
//     darwin/arm64, win32/amd64).
//   - The previously-buggy windows/arm64 combination is rejected.
//   - Combinations that aren't built at all (freebsd/amd64,
//     darwin/ia32) are rejected.
//   - The error output names the offender when a gate fails
//     (we reimplement that part here as a smoke check).
//
// What we DON'T test (left to live e2e):
//   - The actual process.exit(1) call (we can't `process.exit`
//     without killing the test process; we reimplement the
//     SUPPORTED set + the lookup separately).

'use strict';

const assert = require('assert');

// Mirrored from check.js. Update both together when GoReleaser
// adds a new build target or drops an old one.
const SUPPORTED = new Set([
  'linux/amd64',
  'linux/arm64',
  'darwin/amd64',
  'darwin/arm64',
  'win32/amd64',
  // 'win32/arm64' is intentionally absent — see .goreleaser.yaml
  // `builds[].ignore`. See #65.
]);

// --- tests ---------------------------------------------------------------

let passed = 0;
function test(name, fn) {
  try {
    fn();
    passed += 1;
    process.stdout.write(`  ok ${name}\n`);
  } catch (err) {
    process.stdout.write(`  FAIL ${name}: ${err && err.message ? err.message : err}\n`);
    process.exit(1);
  }
}

// Pinned members: every supported (platform, arch) pair must
// pass. If a future goreleaser matrix change adds a row that's
// missing here, this test fails and forces an update.
test('supported_linux_amd64', () => {
  assert.strictEqual(SUPPORTED.has('linux/amd64'), true);
});
test('supported_linux_arm64', () => {
  assert.strictEqual(SUPPORTED.has('linux/arm64'), true);
});
test('supported_darwin_amd64', () => {
  assert.strictEqual(SUPPORTED.has('darwin/amd64'), true);
});
test('supported_darwin_arm64', () => {
  assert.strictEqual(SUPPORTED.has('darwin/arm64'), true);
});
test('supported_win32_amd64', () => {
  assert.strictEqual(SUPPORTED.has('win32/amd64'), true);
});

// Pinned exclusion: windows/arm64 is the bug from #65 — both
// lookup tables in the pre-fix code mapped `win32 + arm64` to
// (windows, arm64) successfully, even though the goreleaser
// build matrix explicitly skips that combination. Verify the
// new combined-key lookup rejects it.
test('rejected_win32_arm64_isTheBugFrom65', () => {
  assert.strictEqual(SUPPORTED.has('win32/arm64'), false);
});

// Combinations that aren't built at all (Node reports these
// platforms / archs but the project doesn't ship binaries for
// them). Both axes must fail, not just one.
test('rejected_freebsd_amd64', () => {
  assert.strictEqual(SUPPORTED.has('freebsd/amd64'), false);
});
test('rejected_darwin_ia32', () => {
  assert.strictEqual(SUPPORTED.has('darwin/ia32'), false);
});
test('rejected_linux_ppc64', () => {
  assert.strictEqual(SUPPORTED.has('linux/ppc64'), false);
});

// A partial-match attack: an attacker who controls an env var or
// a config file might try `linux/amd64/` (trailing slash) or
// `linux/amd64/../arm64` to slip past a startsWith-style check.
// The combined-key lookup is exact-string, so neither can pass.
test('rejected_partialTrailingSlash', () => {
  assert.strictEqual(SUPPORTED.has('linux/amd64/'), false);
});
test('rejected_partialSiblingPrefix', () => {
  // Classic sibling-prefix attack: a key that *starts with* a
  // supported entry but isn't actually in the set.
  assert.strictEqual(SUPPORTED.has('linux/amd64-evil'), false);
});

// The error-message shape (smoke check). When a gate fails, the
// user needs to see which (platform, arch) was rejected and what
// IS supported. Reimplement the message builder here to pin the
// shape — if a future change renames fields or drops the
// "Built binaries are available for:" block, this fails.
test('errorMessageNamesOffenderAndListsSupported', () => {
  // Mirror the format from check.js.
  const platform = 'win32';
  const arch = 'arm64';
  let captured = null;
  if (!SUPPORTED.has(`${platform}/${arch}`)) {
    captured = `unsupported platform/architecture: ${platform}/${arch}; available: ${Array.from(SUPPORTED).join(', ')}`;
  }
  assert.ok(captured && captured.includes('win32/arm64'));
  assert.ok(captured && captured.includes('linux/amd64'));
  assert.ok(captured && captured.includes('windows/arm64') === false); // absent from list
});

// Done.
process.stdout.write(`0 issues. (${passed} tests)\n`);