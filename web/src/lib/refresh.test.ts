import { describe, expect, it } from 'vitest';
import type { Snapshot } from '@/api/client';
import { pr, snapshot, ticket } from '@/test-fixtures';
import { changedCount, refreshToast } from './refresh';

const at = new Date(2026, 10, 24, 9, 5).toISOString();
const before = snapshot([pr('a', 1), pr('b', 2, { tickets: [ticket('INP-1')] })], {
  generatedAt: at,
});
const NOW = Date.parse('2026-11-24T12:00:00Z');

describe('changedCount', () => {
  it('is zero for the same pull requests', () => {
    expect(
      changedCount(before, snapshot([pr('a', 1), pr('b', 2, { tickets: [ticket('INP-1')] })])),
    ).toBe(0);
  });

  it.each([
    ['a new head commit', snapshot([pr('a', 1, { headSha: 'def' }), before.prs[1]!])],
    [
      'a CI change',
      snapshot([
        before.prs[0]!,
        { ...before.prs[1]!, ci: { state: 'failing', total: 1, checks: [] } },
      ]),
    ],
    [
      'a ticket status',
      snapshot([before.prs[0]!, pr('b', 2, { tickets: [ticket('INP-1', { status: 'Done' })] })]),
    ],
    [
      'a review decision',
      snapshot([pr('a', 1, { reviewDecision: 'CHANGES_REQUESTED' }), before.prs[1]!]),
    ],
  ])('counts %s once', (_name, after) => {
    expect(changedCount(before, after)).toBe(1);
  });

  it('counts a pull request that appeared and one that left', () => {
    expect(changedCount(before, snapshot([before.prs[0]!, pr('c', 3)]))).toBe(2);
    expect(changedCount(before, snapshot([]))).toBe(2);
  });
});

describe('refreshToast', () => {
  it('says nothing changed, and since when', () => {
    expect(refreshToast(before, { ...before, version: 2 }, NOW)).toEqual({
      message: 'Refreshed. Nothing changed since 09:05.',
      kind: 'info',
    });
  });

  it('counts the changed pull requests', () => {
    const one = snapshot([pr('a', 1, { headSha: 'x' }), before.prs[1]!]);
    expect(refreshToast(before, one, NOW)).toEqual({
      message: 'Refreshed. 1 pull request changed.',
      kind: 'success',
    });
    expect(refreshToast(before, snapshot([]), NOW).message).toBe(
      'Refreshed. 2 pull requests changed.',
    );
  });

  it('names a GitHub problem before anything else', () => {
    const after: Snapshot = {
      ...before,
      sources: {
        ...before.sources,
        github: {
          state: 'rate_limited',
          syncedAt: at,
          error: null,
          resetAt: new Date(NOW + 90_000).toISOString(),
        },
      },
    };
    expect(refreshToast(before, after, NOW)).toEqual({
      message: 'Refreshed. GitHub rate limited, resets in 1m 30s.',
      kind: 'error',
    });
  });

  it('names a Jira problem, but not a Jira that is not connected', () => {
    const jira = (state: 'unreachable' | 'disconnected'): Snapshot => ({
      ...before,
      sources: { ...before.sources, jira: { state, syncedAt: null, error: null, resetAt: null } },
    });
    expect(refreshToast(before, jira('unreachable'), NOW)).toEqual({
      message: 'Refreshed. Jira unreachable.',
      kind: 'error',
    });
    expect(refreshToast(before, jira('disconnected'), NOW).kind).toBe('info');
  });
});
