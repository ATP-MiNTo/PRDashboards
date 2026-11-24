import assert from 'node:assert/strict';
import { test } from 'node:test';
import { checkAgainstBase, checkVersion, compareSemver } from './check-version.mjs';

const changelog = (...entries) => `# Changelog\n\nIntro.\n\n${entries.join('\n\n')}\n`;
const entry = (v, date = '2026-11-24') => `## [${v}] - ${date}\n\n### Added\n\n- Things.`;

test('a matching dated top entry passes', () => {
  assert.deepEqual(checkVersion({ version: '0.1.0', changelog: changelog(entry('0.1.0'), entry('0.0.1')) }), []);
});

test('VERSION must be semver', () => {
  for (const v of ['', '1', '1.2', 'v1.2.3', '01.2.3', '1.2.3.4']) {
    assert.match(checkVersion({ version: v, changelog: changelog(entry('1.2.3')) })[0], /not a valid semver/, v);
  }
  assert.deepEqual(checkVersion({ version: '1.0.0-rc.1', changelog: changelog(entry('1.0.0-rc.1')) }), []);
});

test('the top entry must match VERSION', () => {
  const problems = checkVersion({ version: '0.2.0', changelog: changelog(entry('0.1.0')) });
  assert.match(problems[0], /top entry is 0\.1\.0 but VERSION is 0\.2\.0/);
});

test('an Unreleased heading is rejected', () => {
  const problems = checkVersion({ version: '0.1.0', changelog: changelog('## [Unreleased]', entry('0.1.0')) });
  assert.match(problems[0], /no "Unreleased" section/);
  assert.equal(problems.length, 1);
});

test('the heading format and date are checked', () => {
  assert.match(checkVersion({ version: '0.1.0', changelog: changelog('## 0.1.0') })[0], /must look like/);
  assert.match(checkVersion({ version: '0.1.0', changelog: changelog(entry('0.1.0', '2026-13-40')) })[0], /not a valid date/);
  assert.match(checkVersion({ version: '0.1.0', changelog: '# Changelog\n' })[0], /no version entries/);
});

test('compareSemver orders numerically', () => {
  assert.ok(compareSemver('0.10.0', '0.9.0') > 0);
  assert.ok(compareSemver('0.0.1', '0.1.0') < 0);
  assert.equal(compareSemver('1.2.3', '1.2.3'), 0);
  assert.equal(compareSemver('1.2.3-rc.1', '1.2.3'), 0);
});

test('VERSION must move forward from the base', () => {
  assert.deepEqual(checkAgainstBase({ version: '0.1.0', baseVersion: '0.0.1', base: 'origin/main' }), []);
  assert.match(checkAgainstBase({ version: '0.1.0', baseVersion: '0.1.0', base: 'origin/main' })[0], /equals the version/);
  assert.match(checkAgainstBase({ version: '0.0.1', baseVersion: '0.1.0', base: 'origin/main' })[0], /lower than/);
  assert.deepEqual(checkAgainstBase({ version: '0.1.0', baseVersion: 'junk', base: 'origin/main' }), []);
});
