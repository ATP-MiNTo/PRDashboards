// Writes out/sw.js after the static export: the list of files to precache comes from out/ and the
// cache name from the VERSION file, so a new release replaces the old cache on activate.
import { readdirSync, readFileSync, writeFileSync } from 'node:fs';
import { join, relative, resolve, sep } from 'node:path';
import { pathToFileURL } from 'node:url';

export const OFFLINE = '/offline/';
/** Paths the worker never touches. They carry session state, events or one-time codes. */
export const NEVER = ['/api/', '/events', '/oauth/', '/auth/launch/'];

function walk(dir) {
  const files = [];
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) files.push(...walk(path));
    else files.push(path);
  }
  return files;
}

/** The URL a file of the export is served at, or null when it is not worth precaching. */
export function urlOf(rel) {
  const path = rel.split(sep).join('/');
  if (path === 'index.html') return '/';
  if (path.endsWith('/index.html')) return `/${path.slice(0, -'index.html'.length)}`;
  if (path.startsWith('_next/static/') && !path.endsWith('.map')) return `/${path}`;
  if (path.startsWith('icons/') || path.startsWith('cursors/') || path === 'manifest.webmanifest')
    return `/${path}`;
  return null;
}

export function precacheList(files) {
  const urls = files.map(urlOf).filter((u) => u !== null && !NEVER.some((p) => u.startsWith(p)));
  return [...new Set(urls)].sort();
}

export function worker(version, urls) {
  return `const CACHE = 'prdash-v${version}';
const PRECACHE = ${JSON.stringify(urls)};
const NEVER = ${JSON.stringify(NEVER)};
const OFFLINE = ${JSON.stringify(OFFLINE)};

self.addEventListener('install', (event) => {
  event.waitUntil(
    caches
      .open(CACHE)
      .then((cache) => cache.addAll(PRECACHE))
      .then(() => self.skipWaiting()),
  );
});

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches
      .keys()
      .then((keys) =>
        Promise.all(keys.filter((k) => k.startsWith('prdash-') && k !== CACHE).map((k) => caches.delete(k))),
      )
      .then(() => self.clients.claim()),
  );
});

self.addEventListener('fetch', (event) => {
  const { request } = event;
  const url = new URL(request.url);
  if (request.method !== 'GET' || url.origin !== self.location.origin) return;
  if (NEVER.some((p) => url.pathname.startsWith(p))) return;
  if (request.mode === 'navigate') {
    event.respondWith(
      fetch(request).catch(async () => (await caches.match(url.pathname)) ?? caches.match(OFFLINE)),
    );
    return;
  }
  if (PRECACHE.includes(url.pathname)) {
    event.respondWith(caches.match(url.pathname).then((hit) => hit ?? fetch(request)));
  }
});
`;
}

function main() {
  const web = resolve(import.meta.dirname, '..');
  const out = join(web, 'out');
  const version = readFileSync(join(web, '..', 'VERSION'), 'utf8').trim();
  const urls = precacheList(walk(out).map((f) => relative(out, f)));
  if (!urls.includes(OFFLINE)) {
    console.error(`service-worker: ${OFFLINE} is missing from the export`);
    process.exit(1);
  }
  writeFileSync(join(out, 'sw.js'), worker(version, urls));
  console.log(`service-worker: sw.js for v${version} with ${urls.length} precached files`);
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) main();
