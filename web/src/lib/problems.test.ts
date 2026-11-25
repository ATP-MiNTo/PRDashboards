import { describe, expect, it } from 'vitest';
import { describeProblem } from './problems';

describe('describeProblem', () => {
  it('maps known codes to sentences', () => {
    expect(describeProblem('invalid_token')).toBe('GitHub rejected the token.');
    expect(describeProblem('unknown_site')).toMatch(/not available/);
  });

  it('names both providers when an app is not set up', () => {
    expect(describeProblem('provider_not_configured')).toBe(
      'Set up the GitHub App or the Atlassian app first.',
    );
  });

  it('falls back for unknown or missing codes', () => {
    for (const code of ['nope', undefined, '']) {
      expect(describeProblem(code)).toBe('Something went wrong. Try again.');
    }
  });
});
