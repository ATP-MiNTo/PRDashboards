// Version guard (see docs/TECH_SPEC.md section 14).
// Checks VERSION is semver, CHANGELOG.md leads with its dated entry, and, with --base <ref>, that
// VERSION moved forward from the base branch.
import { execFileSync } from 'node:child_process';
import { readFileSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { pathToFileURL } from 'node:url';

export const SEMVER =
  /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$/;
const ENTRY = /^## \[(?<version>[^\]]+)\] - (?<date>\d{4}-\d{2}-\d{2})$/;

function validDate(text) {
  const d = new Date(`${text}T00:00:00Z`);
  return !Number.isNaN(d.getTime()) && d.toISOString().startsWith(text);
}

/** Compares the numeric parts of two semver strings. Returns a negative, zero or positive number. */
export function compareSemver(a, b) {
  const pa = a.split(/[-+]/)[0].split('.').map(Number);
  const pb = b.split(/[-+]/)[0].split('.').map(Number);
  for (let i = 0; i < 3; i++) {
    if (pa[i] !== pb[i]) return pa[i] - pb[i];
  }
  return 0;
}

/** Returns a list of problems with VERSION and CHANGELOG.md. */
export function checkVersion({ version, changelog }) {
  const problems = [];
  if (!SEMVER.test(version)) {
    problems.push(`VERSION: "${version}" is not a valid semver version (expected X.Y.Z)`);
    return problems;
  }
  const headings = changelog.split('\n').filter((l) => l.startsWith('## '));
  if (headings.some((h) => /unreleased/i.test(h))) {
    problems.push('CHANGELOG.md: there is no "Unreleased" section; add a dated entry instead');
  }
  const top = headings.find((h) => !/unreleased/i.test(h));
  const match = top?.match(ENTRY);
  if (!top) {
    problems.push('CHANGELOG.md: no version entries found');
  } else if (!match) {
    problems.push(`CHANGELOG.md: top entry "${top}" must look like "## [${version}] - YYYY-MM-DD"`);
  } else {
    if (match.groups.version !== version) {
      problems.push(
        `CHANGELOG.md: top entry is ${match.groups.version} but VERSION is ${version}`,
      );
    }
    if (!validDate(match.groups.date)) {
      problems.push(`CHANGELOG.md: "${match.groups.date}" is not a valid date`);
    }
  }
  return problems;
}

/** Returns a list of problems when VERSION did not move forward from the base branch. */
export function checkAgainstBase({ version, baseVersion, base }) {
  if (!SEMVER.test(baseVersion)) return [];
  const cmp = compareSemver(version, baseVersion);
  if (cmp === 0) {
    return [`VERSION: ${version} equals the version on ${base}; bump it for this change`];
  }
  if (cmp < 0) {
    return [`VERSION: ${version} is lower than ${baseVersion} on ${base}`];
  }
  return [];
}

function baseVersionOf(ref) {
  try {
    return execFileSync('git', ['show', `${ref}:VERSION`], {
      encoding: 'utf8',
      stdio: ['ignore', 'pipe', 'pipe'],
    }).trim();
  } catch (error) {
    const stderr = String(error.stderr ?? '');
    if (/exists on disk, but not in|does not exist|path 'VERSION'/.test(stderr)) return null;
    throw new Error(`cannot read VERSION at ${ref}: ${stderr.trim() || error.message}`);
  }
}

function main() {
  const root = resolve(import.meta.dirname, '..');
  const args = process.argv.slice(2);
  const baseAt = args.indexOf('--base');
  const base = baseAt === -1 ? null : args[baseAt + 1];
  if (baseAt !== -1 && !base) {
    console.error('version-check: --base needs a git ref');
    process.exit(2);
  }
  const version = readFileSync(join(root, 'VERSION'), 'utf8').trim();
  const changelog = readFileSync(join(root, 'CHANGELOG.md'), 'utf8');
  const problems = checkVersion({ version, changelog });
  if (base) {
    const baseVersion = baseVersionOf(base);
    if (baseVersion !== null) problems.push(...checkAgainstBase({ version, baseVersion, base }));
  }
  if (problems.length > 0) {
    console.error(`version-check found ${problems.length} problem(s):`);
    for (const p of problems) console.error(`  - ${p}`);
    process.exit(1);
  }
  console.log(`version-check ok: ${version} in VERSION and CHANGELOG.md${base ? `, ahead of ${base}` : ''}`);
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) main();
