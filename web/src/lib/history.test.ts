import { describe, expect, it } from 'vitest';
import type { MergeResult, MergeStep } from '@/api/client';
import { pr, ticket } from '@/test-fixtures';
import { historyEntry, historyUrl, matchesHistory, mergedWhen, ticketText } from './history';
import { searchTerms } from './search';

const step = (over: Partial<MergeStep>): MergeStep => ({
  kind: 'transition',
  ticket: null,
  state: 'ok',
  message: null,
  toStatus: null,
  retryId: null,
  ...over,
});
const result = (steps: MergeStep[]): MergeResult => ({
  prId: 'PR_1',
  method: 'squash',
  sha: 'abc',
  steps,
});
const merged = step({ kind: 'merge' });

describe('historyEntry', () => {
  const p = pr('PR_1', 490, {
    title: 'Invoice export',
    author: 'tomas',
    additions: 74,
    deletions: 20,
    tickets: [
      ticket('INP-1', { status: 'In Review' }),
      ticket('INP-2', { status: 'To Do' }),
      ticket('INP-3', { status: 'QA' }),
    ],
  });

  it('carries what the row shows from the pull request as it was', () => {
    const e = historyEntry(p, result([merged]), 1000);
    expect(e).toMatchObject({
      prId: 'PR_1',
      title: 'Invoice export',
      repo: 'acme/web',
      number: 490,
      author: 'tomas',
      additions: 74,
      deletions: 20,
      queued: false,
      at: 1000,
      otherFailed: false,
    });
  });

  it('says where each ticket went from the steps, and that the others were left', () => {
    const e = historyEntry(
      p,
      result([
        merged,
        step({ ticket: 'INP-1', toStatus: 'Done' }),
        step({ ticket: 'INP-2', state: 'failed', retryId: 7, message: 'no' }),
        step({ ticket: 'INP-3', state: 'skipped' }),
      ]),
      0,
    );
    expect(e.tickets.map(ticketText)).toEqual([
      'INP-1 moved to Done',
      'INP-2 did not move',
      'INP-3 left in QA',
    ]);
    expect(e.tickets[1]?.retryId).toBe(7);
    expect(e.problems).toEqual(['INP-2: no']);
    expect(e.tickets[0]?.url).toBe('https://acme.atlassian.net/browse/INP-1');
  });

  it('lists a ticket without a step as left, with or without a status', () => {
    const q = pr('PR_2', 2, { tickets: [ticket('INP-9', { status: null })] });
    expect(historyEntry(q, result([merged]), 0).tickets.map(ticketText)).toEqual([
      'INP-9 left as it was',
    ]);
  });

  it('knows a merge GitHub only queued, and a failure that is not a ticket', () => {
    const e = historyEntry(
      p,
      result([
        step({ kind: 'merge', state: 'queued' }),
        step({ kind: 'delete_branch', state: 'failed', message: 'Branch is protected' }),
        step({ ticket: 'INP-1', state: 'queued', toStatus: 'Done' }),
      ]),
      0,
    );
    expect(e.queued).toBe(true);
    expect(e.otherFailed).toBe(true);
    expect(e.problems).toEqual(['Branch is protected']);
    expect(e.tickets[0] && ticketText(e.tickets[0])).toBe('INP-1 will move to Done');
  });

  it('lists a moved ticket the snapshot did not have', () => {
    const e = historyEntry(
      pr('PR_3', 3),
      result([merged, step({ ticket: 'OPS-5', toStatus: 'Done' })]),
      0,
    );
    expect(e.tickets).toEqual([
      { key: 'OPS-5', kind: 'moved', status: 'Done', url: null, retryId: null },
    ]);
  });

  it('builds the GitHub link from the repository and number', () => {
    expect(historyUrl(historyEntry(p, result([merged]), 0))).toBe(
      'https://github.com/acme/web/pull/490',
    );
  });
});

describe('matchesHistory', () => {
  const e = historyEntry(
    pr('PR_1', 490, { title: 'Invoice export', tickets: [ticket('INP-104')] }),
    result([merged]),
    0,
  );
  it('matches the title, the repository, the number and the ticket keys, every word', () => {
    expect(matchesHistory(e, searchTerms(''))).toBe(true);
    expect(matchesHistory(e, searchTerms('invoice'))).toBe(true);
    expect(matchesHistory(e, searchTerms('acme/web #490'))).toBe(true);
    expect(matchesHistory(e, searchTerms('inp-104 export'))).toBe(true);
    expect(matchesHistory(e, searchTerms('invoice payments'))).toBe(false);
  });
});

describe('mergedWhen', () => {
  const now = 10 * 24 * 3_600_000;
  it('says it in the words of the design', () => {
    expect(mergedWhen(now - 20_000, now)).toBe('just now');
    expect(mergedWhen(now - 5 * 60_000, now)).toBe('5m ago');
    expect(mergedWhen(now - 2 * 3_600_000, now)).toBe('2h ago');
    expect(mergedWhen(now - 30 * 3_600_000, now)).toBe('Yesterday');
    expect(mergedWhen(now - 3 * 24 * 3_600_000, now)).toBe('3d ago');
    expect(mergedWhen(now + 5000, now)).toBe('just now');
  });
});
