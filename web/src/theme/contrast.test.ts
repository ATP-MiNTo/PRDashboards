import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';

type Rgb = [number, number, number];

/* The light theme is the `:root` block of globals.css. Its text tones are literal hex values, read
   from the file so the test follows the tokens. The surfaces behind them are the daisyUI `winter`
   values the tokens are built on (base-100, base-200 and base-content). */
const css = readFileSync(resolve(__dirname, '../app/globals.css'), 'utf8');
const lightBlock = css.slice(css.indexOf(':root {'), css.indexOf(":root[data-mode='dark']"));

function token(name: string): Rgb {
  const m = new RegExp(`--${name}:\\s*(#[0-9a-fA-F]{6})\\s*;`).exec(lightBlock);
  if (!m) throw new Error(`light token --${name} is not a hex literal`);
  return hex(m[1]!);
}

function hex(s: string): Rgb {
  return [1, 3, 5].map((i) => parseInt(s.slice(i, i + 2), 16) / 255) as Rgb;
}

const toLinear = (c: number) => (c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4);
const toGamma = (c: number) => (c <= 0.0031308 ? c * 12.92 : 1.055 * c ** (1 / 2.4) - 0.055);

function luminance(rgb: Rgb): number {
  const [r, g, b] = rgb.map(toLinear) as Rgb;
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

function contrast(a: Rgb, b: Rgb): number {
  const x = luminance(a);
  const y = luminance(b);
  return (Math.max(x, y) + 0.05) / (Math.min(x, y) + 0.05);
}

function toOklab(rgb: Rgb): Rgb {
  const [r, g, b] = rgb.map(toLinear) as Rgb;
  const l = Math.cbrt(0.4122214708 * r + 0.5363325363 * g + 0.0514459929 * b);
  const m = Math.cbrt(0.2119034982 * r + 0.6806995451 * g + 0.1073969566 * b);
  const s = Math.cbrt(0.0883024619 * r + 0.2817188376 * g + 0.6299787005 * b);
  return [
    0.2104542553 * l + 0.793617785 * m - 0.0040720468 * s,
    1.9779984951 * l - 2.428592205 * m + 0.4505937099 * s,
    0.0259040371 * l + 0.7827717662 * m - 0.808675766 * s,
  ];
}

function fromOklab([L, a, b]: Rgb): Rgb {
  const l = (L + 0.3963377774 * a + 0.2158037573 * b) ** 3;
  const m = (L - 0.1055613458 * a - 0.0638541728 * b) ** 3;
  const s = (L - 0.0894841775 * a - 1.291485548 * b) ** 3;
  return [
    4.0767416621 * l - 3.3077115913 * m + 0.2309699292 * s,
    -1.2684380046 * l + 2.6097574011 * m - 0.3413193965 * s,
    -0.0041960863 * l - 0.7034186147 * m + 1.707614701 * s,
  ].map(toGamma) as Rgb;
}

/* color-mix(in oklab, a p%, b) */
function mix(a: Rgb, p: number, b: Rgb): Rgb {
  const x = toOklab(a);
  const y = toOklab(b);
  return fromOklab(x.map((v, i) => v * p + y[i]! * (1 - p)) as Rgb);
}

function over(fg: Rgb, alpha: number, bg: Rgb): Rgb {
  return fg.map((v, i) => v * alpha + bg[i]! * (1 - alpha)) as Rgb;
}

const white: Rgb = [1, 1, 1];
const winterBase200 = hex('#F2F7FF');
const winterInk = hex('#394E6A');

/* The surfaces text sits on: the surface, the page, the two card tints, and the chip fill. */
const cardA = mix(token('rc-a'), 0.28, white);
const cardR = mix(token('rc-r'), 0.28, white);
const stackBg = mix(token('stack'), 0.18, white);
const surfaces: Record<string, Rgb> = {
  surface: white,
  page: winterBase200,
  'card assigned': cardA,
  'card review': cardR,
  'ticket badge on review card': over(winterInk, 0.05, cardR),
  'stack banner': stackBg,
};

describe('light theme contrast', () => {
  it('computes the WCAG ratio', () => {
    expect(contrast([0, 0, 0], white)).toBeCloseTo(21, 5);
    expect(contrast(white, white)).toBeCloseTo(1, 5);
  });

  const texts = ['ticket-fg', 'ci-ok', 'ci-warn', 'ci-err', 'diff-add', 'diff-del'];
  for (const name of texts) {
    it(`--${name} has 4.5:1 on every surface`, () => {
      for (const [surface, bg] of Object.entries(surfaces)) {
        expect(contrast(token(name), bg), `${name} on ${surface}`).toBeGreaterThanOrEqual(4.5);
      }
    });
  }

  it('white text on the stack chip (--stack) has 4.5:1', () => {
    expect(contrast(white, token('stack'))).toBeGreaterThanOrEqual(4.5);
  });

  it('white text on the Merge button has 4.5:1, also on hover', () => {
    expect(contrast(white, token('merge'))).toBeGreaterThanOrEqual(4.5);
    expect(contrast(white, token('merge-hover'))).toBeGreaterThanOrEqual(4.5);
  });

  it('keeps the dark overrides of the shared tokens', () => {
    const dark = css.slice(css.indexOf(":root[data-mode='dark']"));
    for (const name of ['diff-add', 'diff-del', 'ticket-fg', 'ci-ok', 'ci-warn', 'ci-err']) {
      expect(dark).toContain(`--${name}: color-mix(`);
    }
  });
});
