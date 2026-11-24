import { inflateSync } from 'node:zlib';
import { describe, expect, it } from 'vitest';
import { icon, manifest, token } from './pwa-assets.mjs';
import { NEVER, OFFLINE, precacheList, urlOf, worker } from './service-worker.mjs';

describe('pwa assets', () => {
  it('reads tokens from the light block only', () => {
    const css =
      ':root {\n  --tb-bg: #FBFAFF;\n}\n:root[data-mode="dark"] {\n  --tb-bg: #1d1b27;\n}';
    expect(token(css, 'tb-bg')).toBe('#fbfaff');
    expect(() => token(css, 'nope')).toThrow();
  });

  it('draws valid PNGs of the requested size', () => {
    const buf = icon(64, '#6aa6d8', '#ffffff', false);
    expect([...buf.subarray(0, 8)]).toEqual([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]);
    expect(buf.readUInt32BE(16)).toBe(64);
    expect(buf.readUInt32BE(20)).toBe(64);
    const idat = buf.indexOf('IDAT');
    const len = buf.readUInt32BE(idat - 4);
    const raw = inflateSync(buf.subarray(idat + 4, idat + 4 + len));
    expect(raw.length).toBe((64 * 4 + 1) * 64);
    expect(raw[1 + 3]).toBe(0); // rounded corner is transparent
  });

  it('fills the whole square when maskable', () => {
    const buf = icon(32, '#6aa6d8', '#ffffff', true);
    const idat = buf.indexOf('IDAT');
    const raw = inflateSync(buf.subarray(idat + 4, idat + 4 + buf.readUInt32BE(idat - 4)));
    expect(raw[1 + 3]).toBe(255);
  });

  it('describes the installable app', () => {
    const m = manifest('#fbfaff', '#eaf3fc');
    expect(m).toMatchObject({
      name: 'PR Dashboards',
      short_name: 'PR Dash',
      start_url: '/',
      scope: '/',
      display: 'standalone',
      theme_color: '#fbfaff',
      background_color: '#eaf3fc',
    });
    expect(m.icons).toHaveLength(3);
  });
});

describe('service worker', () => {
  it('precaches static assets only', () => {
    const files = [
      'index.html',
      'accounts/index.html',
      'auth/launch/index.html',
      'oauth/finish/index.html',
      'offline/index.html',
      '_next/static/chunks/a.js',
      '_next/static/chunks/a.js.map',
      'icons/icon-192.png',
      'manifest.webmanifest',
      'csp.json',
      '404.html',
      '__next._full.txt',
    ];
    expect(precacheList(files)).toEqual([
      '/',
      '/_next/static/chunks/a.js',
      '/accounts/',
      '/icons/icon-192.png',
      '/manifest.webmanifest',
      '/offline/',
    ]);
    expect(urlOf('api/index.html')).toBe('/api/');
  });

  it('versions the cache, skips private paths and answers navigations offline', () => {
    const src = worker('1.2.3', ['/', OFFLINE]);
    expect(src).toContain("'prdash-v1.2.3'");
    expect(src).toContain('caches.delete');
    expect(src).toContain("request.mode === 'navigate'");
    for (const p of ['/api/', '/events', '/oauth/', '/auth/launch/']) expect(NEVER).toContain(p);
    expect(() => new Function(src)).not.toThrow();
  });
});
