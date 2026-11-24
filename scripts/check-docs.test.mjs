import assert from 'node:assert/strict';
import { test } from 'node:test';
import { checkDocs, checkFeatureDoc, HEADINGS, indexRows, relativeLinks } from './check-docs.mjs';

const doc = (status = 'Built', headings = HEADINGS) =>
  `# Thing\n\nStatus: ${status}. PRD: x.\n\n${headings.map((h) => `## ${h}\n\nNone.\n`).join('\n')}`;

const arch = (rows) =>
  `# Architecture\n\n| Feature | Doc | Status |\n| --- | --- | --- |\n${rows.join('\n')}\n`;

const exists = (known) => (p) => known.includes(p);

test('a doc with every heading and a status passes', () => {
  assert.deepEqual(checkFeatureDoc('f.md', doc()), { problems: [], status: 'Built' });
});

test('missing, extra and reordered headings are reported', () => {
  assert.match(checkFeatureDoc('f.md', doc('Built', HEADINGS.slice(1))).problems[0], /missing: Purpose/);
  assert.match(checkFeatureDoc('f.md', doc('Built', [...HEADINGS, 'Extra'])).problems[0], /unexpected: Extra/);
  assert.match(
    checkFeatureDoc('f.md', doc('Built', [...HEADINGS].reverse())).problems[0],
    /wrong order/,
  );
});

test('status line must be valid', () => {
  assert.match(checkFeatureDoc('f.md', doc('Done')).problems[0], /Status line must start/);
  assert.match(checkFeatureDoc('f.md', '# T\n\n## Purpose\n').problems[0], /missing a "Status:" line/);
});

test('code blocks do not count as headings or links', () => {
  const text = `${doc()}\n\`\`\`md\n## Fake\n[x](missing.md)\n\`\`\`\n`;
  assert.deepEqual(checkFeatureDoc('f.md', text).problems, []);
  assert.deepEqual(relativeLinks(text), []);
});

test('relative links are collected without anchors and external ones are skipped', () => {
  assert.deepEqual(relativeLinks('[a](b.md#x) [c](https://x.y) [d](#top) [e](../f.md "t")'), [
    'b.md',
    '../f.md',
  ]);
});

test('index rows accept links and inline code', () => {
  const rows = indexRows(
    arch(['| A | [features/a.md](features/a.md) | Built |', '| B | `features/b.md` | Not built |']),
  );
  assert.deepEqual(rows, [
    { doc: 'features/a.md', status: 'Built' },
    { doc: 'features/b.md', status: 'Not built' },
  ]);
});

test('consistent docs and index pass', () => {
  const problems = checkDocs({
    architecture: arch([
      '| A | [features/a.md](features/a.md) | Built |',
      '| B | `features/b.md` | Not built |',
    ]),
    features: { 'features/a.md': doc(), 'features/README.md': '# README\n' },
    exists: exists(['features/a.md', 'features/README.md']),
  });
  assert.deepEqual(problems, []);
});

test('a doc without an index row is reported', () => {
  const problems = checkDocs({
    architecture: arch(['| B | `features/b.md` | Not built |']),
    features: { 'features/a.md': doc() },
    exists: exists(['features/a.md']),
  });
  assert.deepEqual(problems, ['architecture.md: index table has no row for features/a.md']);
});

test('a built row without its doc is reported, a not built one is not', () => {
  const problems = checkDocs({
    architecture: arch(['| B | `features/b.md` | Built |', '| C | `features/c.md` | Not built |']),
    features: {},
    exists: exists([]),
  });
  assert.equal(problems.length, 1);
  assert.match(problems[0], /features\/b\.md.*doc is missing/);
});

test('status mismatch, duplicate rows and broken links are reported', () => {
  const problems = checkDocs({
    architecture: `${arch([
      '| A | [features/a.md](features/a.md) | Partial |',
      '| A | [features/a.md](features/a.md) | Partial |',
    ])}\n[gone](nope.md)\n`,
    features: { 'features/a.md': doc('Built') },
    exists: exists(['features/a.md']),
  });
  assert.equal(problems.length, 4);
  assert.ok(problems.some((p) => /more than one index row/.test(p)));
  assert.ok(problems.some((p) => /index says features\/a\.md is "Partial" but the doc says "Built"/.test(p)));
  assert.ok(problems.some((p) => /broken link to nope\.md/.test(p)));
});

test('a missing table is reported', () => {
  assert.deepEqual(checkDocs({ architecture: '# A\n', features: {}, exists: exists([]) }), [
    'architecture.md: no feature index table found',
  ]);
});
