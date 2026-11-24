// Drift guard for the architecture docs (see docs/features/README.md).
// Checks the feature doc template, relative links and the index table in docs/architecture.md.
import { existsSync, readdirSync, readFileSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { pathToFileURL } from 'node:url';

export const HEADINGS = [
  'Purpose',
  'Data model',
  'Code map',
  'Key flows',
  'Outside calls',
  'Rules and invariants',
  'Tests',
];
export const STATUSES = ['Built', 'Partial', 'Not built'];

/** Removes fenced code blocks and inline code, so examples in them are not checked. */
export function stripCode(text) {
  return text
    .replace(/^ {0,3}(`{3,}|~{3,})[^\n]*\n[\s\S]*?^ {0,3}\1[ \t]*$/gm, '')
    .replace(/`[^`\n]*`/g, '');
}

/** Returns the relative file targets of Markdown links, without anchors. */
export function relativeLinks(text) {
  const targets = [];
  for (const m of stripCode(text).matchAll(/\[[^\]]*\]\((<[^>\n]*>|[^)\s]+)(?:\s+"[^"]*")?\)/g)) {
    let raw = m[1];
    if (raw.startsWith('<') && raw.endsWith('>')) raw = raw.slice(1, -1);
    if (/^([a-z][a-z0-9+.-]*:|#|\/\/)/i.test(raw)) continue;
    let file = raw.split(/[#?]/)[0];
    try {
      file = decodeURIComponent(file);
    } catch {
      // keep the raw text; the link is reported as broken if it does not exist
    }
    if (file) targets.push(file);
  }
  return targets;
}

/** Checks one feature doc. Returns the problems found and the doc's declared status. */
export function checkFeatureDoc(name, text) {
  const problems = [];
  const lines = stripCode(text).split('\n');
  const first = lines.find((l) => l.trim() !== '');
  if (!first || !/^# \S/.test(first)) {
    problems.push(`${name}: must start with a "# <Feature name>" title`);
  }
  const statusLine = lines.find((l) => l.startsWith('Status:'));
  let status = null;
  if (!statusLine) {
    problems.push(`${name}: missing a "Status:" line`);
  } else {
    status = STATUSES.find((s) => statusLine.startsWith(`Status: ${s}.`)) ?? null;
    if (!status) {
      problems.push(
        `${name}: Status line must start with one of ${STATUSES.map((s) => `"Status: ${s}."`).join(', ')}`,
      );
    }
  }
  const found = lines.filter((l) => l.startsWith('## ')).map((l) => l.slice(3).trim());
  if (found.join('|') !== HEADINGS.join('|')) {
    const missing = HEADINGS.filter((h) => !found.includes(h));
    const extra = found.filter((h) => !HEADINGS.includes(h));
    const detail = [
      missing.length ? `missing: ${missing.join(', ')}` : '',
      extra.length ? `unexpected: ${extra.join(', ')}` : '',
      !missing.length && !extra.length ? 'wrong order or duplicated' : '',
    ]
      .filter(Boolean)
      .join('; ');
    problems.push(`${name}: headings must be exactly ${HEADINGS.join(' > ')} (${detail})`);
  }
  return { problems, status };
}

/**
 * Rows of the index table as [{ doc, status }]. A row names its doc either as a link or as inline
 * code, for example `features/app-shell.md`.
 */
export function indexRows(architectureText) {
  const withoutFences = architectureText.replace(
    /^ {0,3}(`{3,}|~{3,})[^\n]*\n[\s\S]*?^ {0,3}\1[ \t]*$/gm,
    '',
  );
  const rows = [];
  let statusCol = -1;
  for (const line of withoutFences.split('\n')) {
    if (!line.trim().startsWith('|')) continue;
    const cells = line.split('|').map((c) => c.trim());
    if (cells.includes('Status')) statusCol = cells.indexOf('Status');
    const m = line.match(/(features\/[A-Za-z0-9._-]+\.md)/);
    if (!m) continue;
    const cell = statusCol >= 0 ? cells[statusCol] : undefined;
    rows.push({ doc: m[1], status: STATUSES.includes(cell) ? cell : null });
  }
  return rows;
}

/**
 * @param {{ architecture: string, features: Record<string, string>, exists: (path: string) => boolean }} input
 *   `features` maps "features/<name>.md" to its text (README included, or not).
 *   `exists` takes a path relative to the docs folder.
 * @returns {string[]} problems
 */
export function checkDocs({ architecture, features, exists }) {
  const problems = [];
  const statuses = {};
  const docNames = Object.keys(features).filter((n) => n !== 'features/README.md');

  for (const name of docNames) {
    const result = checkFeatureDoc(name, features[name]);
    problems.push(...result.problems);
    statuses[name] = result.status;
  }

  const sources = { 'architecture.md': architecture, ...features };
  for (const [name, text] of Object.entries(sources)) {
    const dir = dirname(name);
    for (const target of relativeLinks(text)) {
      const path = join(dir, target).split('\\').join('/');
      if (!exists(path)) problems.push(`${name}: broken link to ${target}`);
    }
  }

  const rows = indexRows(architecture);
  if (rows.length === 0) problems.push('architecture.md: no feature index table found');
  const indexed = new Set(rows.map((r) => r.doc));
  for (const name of docNames) {
    if (!indexed.has(name)) problems.push(`architecture.md: index table has no row for ${name}`);
  }
  for (const doc of indexed) {
    if (rows.filter((r) => r.doc === doc).length > 1) {
      problems.push(`architecture.md: more than one index row for ${doc}`);
    }
  }
  for (const row of rows) {
    if (!row.status) {
      problems.push(
        `architecture.md: index row for ${row.doc} has no valid status (expected Built, Partial or Not built)`,
      );
      continue;
    }
    if (!exists(row.doc)) {
      if (row.status !== 'Not built') {
        problems.push(`architecture.md: index row for ${row.doc} says "${row.status}" but the doc is missing`);
      }
    } else if (statuses[row.doc] && statuses[row.doc] !== row.status) {
      problems.push(
        `architecture.md: index says ${row.doc} is "${row.status}" but the doc says "${statuses[row.doc]}"`,
      );
    }
  }
  return problems;
}

function main() {
  const root = resolve(import.meta.dirname, '..', 'docs');
  const read = (p) => readFileSync(join(root, p), 'utf8');
  const featureFiles = readdirSync(join(root, 'features')).filter((f) => f.endsWith('.md'));
  const features = Object.fromEntries(
    featureFiles.map((f) => [`features/${f}`, read(`features/${f}`)]),
  );
  const problems = checkDocs({
    architecture: read('architecture.md'),
    features,
    exists: (p) => existsSync(join(root, p)),
  });
  if (problems.length > 0) {
    console.error(`docs-check found ${problems.length} problem(s):`);
    for (const p of problems) console.error(`  - ${p}`);
    process.exit(1);
  }
  const count = featureFiles.filter((f) => f !== 'README.md').length;
  console.log(`docs-check ok: ${count} feature docs, links and index consistent`);
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) main();
