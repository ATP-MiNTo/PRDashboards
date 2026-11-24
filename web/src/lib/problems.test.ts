import { describe, expect, it } from 'vitest';
import { describeProblem } from './problems';

describe('describeProblem', () => {
  it('maps known codes to sentences', () => {
    expect(describeProblem('invalid_token')).toBe('GitHub rejected the token.');
    expect(describeProblem('unknown_site')).toMatch(/not available/);
  });

  it('falls back for unknown or missing codes', () => {
    for (const code of ['nope', undefined, '']) {
      expect(describeProblem(code)).toBe('Something went wrong. Try again.');
    }
  });
});
