import { describe, expect, it } from 'vitest';
import { ageShort, initials } from './card';

describe('initials', () => {
  it.each([
    ['priya', 'PR'],
    ['a', 'A'],
    ['dev-ops', 'DE'],
    ['', '?'],
    [null, '?'],
  ])('turns %j into %s', (author, out) => {
    expect(initials(author)).toBe(out);
  });

  it('does not cut a character in half', () => {
    expect(initials('😀😀x')).toBe('😀😀');
  });
});

describe('ageShort', () => {
  const now = Date.parse('2026-11-24T12:00:00Z');
  const ago = (ms: number) => new Date(now - ms).toISOString();
  const MIN = 60_000;

  it.each([
    [0, '0m'],
    [8 * MIN, '8m'],
    [59 * MIN, '59m'],
    [60 * MIN, '1h'],
    [3 * 60 * MIN, '3h'],
    [23 * 60 * MIN, '23h'],
    [24 * 60 * MIN, '1d'],
    [50 * 24 * 60 * MIN, '50d'],
  ])('says %d ms ago as %s', (ms, out) => {
    expect(ageShort(ago(ms), now)).toBe(out);
  });

  it('never says a negative age for a clock that is a little behind', () => {
    expect(ageShort(ago(-5 * MIN), now)).toBe('0m');
  });

  it('is empty when the time is missing or not a time', () => {
    expect(ageShort(null, now)).toBe('');
    expect(ageShort('', now)).toBe('');
    expect(ageShort('not a date', now)).toBe('');
  });
});
