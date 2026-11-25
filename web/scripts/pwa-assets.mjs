// Generates public/manifest.webmanifest and public/icons/*.png before the web build. The colours
// come from the theme tokens in globals.css, and the icons are drawn here as PNG, so the build
// needs no image tooling.
import { mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { pathToFileURL } from 'node:url';
import { deflateSync } from 'node:zlib';

/** Reads a token from the first (light) :root block of the stylesheet. */
export function token(css, name) {
  const root = css.slice(0, css.indexOf('}'));
  const m = new RegExp(`--${name}:\\s*(#[0-9a-fA-F]{6})\\s*;`).exec(root);
  if (!m) throw new Error(`pwa-assets: token --${name} not found`);
  return m[1].toLowerCase();
}

const rgb = (hex) => [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16));

const CRC = new Int32Array(256).map((_, n) => {
  let c = n;
  for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
  return c;
});

function crc32(buf) {
  let c = -1;
  for (const b of buf) c = CRC[(c ^ b) & 0xff] ^ (c >>> 8);
  return (c ^ -1) >>> 0;
}

function chunk(type, data) {
  const out = Buffer.alloc(12 + data.length);
  out.writeUInt32BE(data.length, 0);
  out.write(type, 4, 'ascii');
  data.copy(out, 8);
  out.writeUInt32BE(crc32(out.subarray(4, 8 + data.length)), 8 + data.length);
  return out;
}

/** Encodes RGBA pixels (size x size) as a PNG. */
export function png(size, pixels) {
  const head = Buffer.alloc(13);
  head.writeUInt32BE(size, 0);
  head.writeUInt32BE(size, 4);
  head.set([8, 6, 0, 0, 0], 8);
  const stride = size * 4;
  const raw = Buffer.alloc((stride + 1) * size);
  for (let y = 0; y < size; y++)
    pixels.copy(raw, y * (stride + 1) + 1, y * stride, (y + 1) * stride);
  return Buffer.concat([
    Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
    chunk('IHDR', head),
    chunk('IDAT', deflateSync(raw, { level: 9 })),
    chunk('IEND', Buffer.alloc(0)),
  ]);
}

const clamp = (v) => Math.min(1, Math.max(0, v));
const segment = (x, y, a, b) => {
  const [dx, dy] = [b[0] - a[0], b[1] - a[1]];
  const t = clamp(((x - a[0]) * dx + (y - a[1]) * dy) / (dx * dx + dy * dy));
  return Math.hypot(x - a[0] - t * dx, y - a[1] - t * dy);
};

/**
 * Draws a branch-and-merge mark: a stem with two nodes and a side node. maskable fills the whole
 * square and keeps the mark inside the central 80%, others have rounded corners.
 */
export function icon(size, bg, fg, maskable) {
  const px = Buffer.alloc(size * size * 4);
  const scale = maskable ? 0.72 : 0.9;
  const at = (u, v) => [size * (0.5 + (u - 0.5) * scale), size * (0.5 + (v - 0.5) * scale)];
  const top = at(0.34, 0.24);
  const bottom = at(0.34, 0.76);
  const side = at(0.68, 0.5);
  const half = (size * scale) / 2 / 5; // line half width
  const node = half * 2.1;
  const corner = maskable ? 0 : size * 0.2;
  const [br, bgc, bb] = rgb(bg);
  const [fr, fgc, fb] = rgb(fg);
  for (let y = 0; y < size; y++) {
    for (let x = 0; x < size; x++) {
      const [cx, cy] = [x + 0.5, y + 0.5];
      const ex = Math.max(corner - cx, cx - (size - corner), 0);
      const ey = Math.max(corner - cy, cy - (size - corner), 0);
      const inside = clamp(corner - Math.hypot(ex, ey) + 0.5);
      const d = Math.min(
        segment(cx, cy, top, bottom) - half,
        segment(cx, cy, bottom, side) - half,
        Math.hypot(cx - top[0], cy - top[1]) - node,
        Math.hypot(cx - bottom[0], cy - bottom[1]) - node,
        Math.hypot(cx - side[0], cy - side[1]) - node,
      );
      const mark = clamp(0.5 - d);
      const o = (y * size + x) * 4;
      px[o] = Math.round(br + (fr - br) * mark);
      px[o + 1] = Math.round(bgc + (fgc - bgc) * mark);
      px[o + 2] = Math.round(bb + (fb - bb) * mark);
      px[o + 3] = Math.round(255 * (maskable ? 1 : inside));
    }
  }
  return png(size, px);
}

export function manifest(themeColor, backgroundColor) {
  const icons = [
    { src: '/icons/icon-192.png', sizes: '192x192', type: 'image/png', purpose: 'any' },
    { src: '/icons/icon-512.png', sizes: '512x512', type: 'image/png', purpose: 'any' },
    { src: '/icons/maskable-512.png', sizes: '512x512', type: 'image/png', purpose: 'maskable' },
  ];
  return {
    name: 'PR Dashboards',
    short_name: 'PR Dash',
    description: 'Open pull requests assigned or opened by you, linked to Jira.',
    start_url: '/',
    scope: '/',
    display: 'standalone',
    theme_color: themeColor,
    background_color: backgroundColor,
    icons,
  };
}

function main() {
  const web = resolve(import.meta.dirname, '..');
  const css = readFileSync(join(web, 'src', 'app', 'globals.css'), 'utf8');
  const [bar, page, accent] = ['tb-bg', 'page-bg', 'accent-bd'].map((n) => token(css, n));
  const pub = join(web, 'public');
  mkdirSync(join(pub, 'icons'), { recursive: true });
  writeFileSync(join(pub, 'icons', 'icon-192.png'), icon(192, accent, '#ffffff', false));
  writeFileSync(join(pub, 'icons', 'icon-512.png'), icon(512, accent, '#ffffff', false));
  writeFileSync(join(pub, 'icons', 'maskable-512.png'), icon(512, accent, '#ffffff', true));
  writeFileSync(
    join(pub, 'manifest.webmanifest'),
    `${JSON.stringify(manifest(bar, page), null, 2)}\n`,
  );
  console.log('pwa-assets: manifest and 3 icons written to public/');
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) main();
