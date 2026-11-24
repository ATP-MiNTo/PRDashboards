// Writes out/csp.json with the sha256 of every inline <script> in the static export, so the
// server can allow exactly those scripts in its Content-Security-Policy.
import { createHash } from 'node:crypto';
import { readdirSync, readFileSync, writeFileSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { pathToFileURL } from 'node:url';

const INLINE_SCRIPT = /<script\b([^>]*)>([\s\S]*?)<\/script>/gi;
const INLINE_STYLE_BLOCK = /<style\b/i;
const STYLE_ATTRIBUTE = /<[a-z][^>]*\sstyle\s*=/i;

/** Returns the CSP source expression for each inline script in html, in document order. */
export function inlineScriptHashes(html) {
  const hashes = [];
  for (const [, attrs, body] of html.matchAll(INLINE_SCRIPT)) {
    if (/\ssrc\s*=/i.test(attrs)) continue;
    hashes.push(`'sha256-${createHash('sha256').update(body, 'utf8').digest('base64')}'`);
  }
  return hashes;
}

/** Describes inline styles, which `style-src 'self'` blocks. */
export function inlineStyleProblems(html) {
  const problems = [];
  if (INLINE_STYLE_BLOCK.test(html)) problems.push('<style> block');
  if (STYLE_ATTRIBUTE.test(html)) problems.push('style attribute');
  return problems;
}

function htmlFiles(dir) {
  const files = [];
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) files.push(...htmlFiles(path));
    else if (entry.name.endsWith('.html')) files.push(path);
  }
  return files;
}

function main() {
  const out = resolve(import.meta.dirname, '..', 'out');
  const files = htmlFiles(out);
  if (files.length === 0) {
    console.error(`csp-hashes: no .html files in ${out}; run next build first`);
    process.exit(1);
  }
  const hashes = new Set();
  const styleProblems = [];
  for (const file of files) {
    const html = readFileSync(file, 'utf8');
    for (const hash of inlineScriptHashes(html)) hashes.add(hash);
    for (const problem of inlineStyleProblems(html)) styleProblems.push(`${file}: ${problem}`);
  }
  if (styleProblems.length > 0) {
    console.error("csp-hashes: inline styles are blocked by style-src 'self':");
    for (const p of styleProblems) console.error(`  - ${p}`);
    process.exit(1);
  }
  const scriptHashes = [...hashes].sort();
  writeFileSync(join(out, 'csp.json'), `${JSON.stringify({ scriptHashes }, null, 2)}\n`);
  console.log(`csp-hashes: ${scriptHashes.length} inline script hashes from ${files.length} pages`);
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) main();
