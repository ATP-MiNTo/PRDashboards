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

  it('words the organization block for SSO and token policies', () => {
    expect(describeProblem('org_blocked')).toBe(
      'An organization requires SAML SSO or blocks this kind of token. Authorize it for the organization in GitHub.',
    );
  });

  it('shows the server detail of a missing permission as text', () => {
    expect(
      describeProblem(
        'missing_permission',
        'The token needs the GitHub permission contents: write',
      ),
    ).toBe('The token needs the GitHub permission contents: write');
    expect(describeProblem('missing_permission')).toBe(
      'The token lacks a GitHub permission this action needs.',
    );
  });

  it('shows the reason of a merge GitHub failed, and a fixed text for a pending one', () => {
    expect(describeProblem('merge_failed', 'GitHub did not merge: 2 approvals are required')).toBe(
      'GitHub did not merge: 2 approvals are required',
    );
    expect(describeProblem('merge_failed')).toBe('GitHub did not merge this pull request.');
    expect(describeProblem('merge_pending', 'x')).toBe(
      'GitHub is still merging. Refresh in a moment to see the result.',
    );
  });

  it('ignores the detail of other codes', () => {
    expect(describeProblem('upstream_error', '<b>x</b>')).toBe(
      'The provider could not be reached. Try again.',
    );
  });

  it('falls back for unknown or missing codes', () => {
    for (const code of ['nope', undefined, '']) {
      expect(describeProblem(code)).toBe('Something went wrong. Try again.');
    }
  });
});
