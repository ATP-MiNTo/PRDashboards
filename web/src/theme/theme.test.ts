import { describe, expect, it } from 'vitest';
import { autoMode, isThemeSetting, msUntilSwitch, resolveMode } from './theme';

const at = (h: number, m = 0) => new Date(2026, 10, 24, h, m, 0, 0);

describe('theme', () => {
  it.each([
    [0, 'dark'],
    [6, 'dark'],
    [7, 'light'],
    [12, 'light'],
    [18, 'light'],
    [19, 'dark'],
    [23, 'dark'],
  ] as const)('Auto at %i:00 is %s', (h, mode) => {
    expect(autoMode(at(h))).toBe(mode);
    expect(resolveMode('auto', at(h))).toBe(mode);
  });

  it('keeps an explicit choice at any hour', () => {
    expect(resolveMode('dark', at(12))).toBe('dark');
    expect(resolveMode('light', at(23))).toBe('light');
  });

  it('counts the time to the next switch', () => {
    expect(msUntilSwitch(at(18, 30))).toBe(30 * 60_000);
    expect(msUntilSwitch(at(6, 0))).toBe(60 * 60_000);
    expect(msUntilSwitch(at(23, 0))).toBe(8 * 60 * 60_000);
  });

  it('validates a stored value', () => {
    expect(isThemeSetting('auto')).toBe(true);
    expect(isThemeSetting('sepia')).toBe(false);
  });
});
