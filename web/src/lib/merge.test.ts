import { describe, expect, it } from 'vitest';
import type { MergeStep, MethodResolution } from '@/api/client';
import { effectiveMethod, methodFrom, methodWord, mergeSummary } from './merge';

const step = (over: Partial<MergeStep>): MergeStep => ({
  kind: 'merge',
  ticket: null,
  state: 'ok',
  message: null,
  toStatus: null,
  retryId: null,
  ...over,
});

describe('methodFrom', () => {
  it.each([
    ['rule: main', 'branch rule main'],
    ['repo default', 'the repo settings'],
    ['new repo default', 'the defaults'],
    ['Repo allows only squash', 'repo allows only squash'],
  ])('%s -> %s', (source, text) => expect(methodFrom(source)).toBe(text));
});

describe('methodWord', () => {
  it('says the method in lower case', () => {
    expect(methodWord('squash')).toBe('squash');
    expect(methodWord('merge')).toBe('merge commit');
    expect(methodWord('rebase')).toBe('rebase');
  });
});

describe('effectiveMethod', () => {
  const r: MethodResolution = {
    method: 'squash',
    default: 'squash',
    source: 'repo default',
    allowed: ['squash', 'rebase'],
    warning: null,
  };
  it('keeps the resolved method without a choice, and the choice while it is allowed', () => {
    expect(effectiveMethod(r, null)).toBe('squash');
    expect(effectiveMethod(r, 'rebase')).toBe('rebase');
    expect(effectiveMethod(r, 'merge')).toBe('squash');
  });
});

describe('mergeSummary', () => {
  it('sums up a merge that moved a ticket', () => {
    expect(
      mergeSummary(490, [
        step({}),
        step({ kind: 'transition', ticket: 'INP-104', toStatus: 'Done' }),
      ]),
    ).toEqual({ text: '#490 merged, INP-104 moved to Done', failed: false, merged: true });
  });

  it('says a pull request was only queued', () => {
    expect(mergeSummary(5, [step({ state: 'queued' })])).toEqual({
      text: '#5 added to the merge queue',
      failed: false,
      merged: false,
    });
  });

  it('says what failed and where to retry', () => {
    const s = mergeSummary(7, [
      step({}),
      step({ kind: 'transition', ticket: 'INP-1', state: 'failed' }),
      step({ kind: 'delete_branch', state: 'failed' }),
    ]);
    expect(s.failed).toBe(true);
    expect(s.text).toBe(
      '#7 merged, INP-1 did not move, the branch was not deleted. Retry it from History',
    );
  });

  it('is only the head for a merge with nothing else to say', () => {
    expect(mergeSummary(3, [step({}), step({ kind: 'delete_branch' })]).text).toBe('#3 merged');
  });
});
