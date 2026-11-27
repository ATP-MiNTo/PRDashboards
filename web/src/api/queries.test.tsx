import { QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react';
import type { ReactNode } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { makeQueryClient } from '@/app/providers';
import { ApiError, type RepoSettings, type SyncRule } from '@/api/client';

const api = vi.hoisted(() => ({
  approvePr: vi.fn(),
  requestChangesPr: vi.fn(),
  markPrReady: vi.fn(),
  putRepo: vi.fn(),
  getSnapshot: vi.fn(),
  getRepos: vi.fn(),
  previewRules: vi.fn(),
}));

vi.mock('@/api/client', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/api/client')>()),
  ...api,
}));

import {
  approvalsUpdate,
  keys,
  useApprove,
  useMarkReady,
  usePreviewRules,
  useRequestChanges,
  useSaveApprovals,
} from './queries';

const sha = '0123456789abcdef0123456789abcdef01234567';

function setup() {
  const client = makeQueryClient();
  const spy = vi.spyOn(client, 'invalidateQueries');
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
  return { client, spy, wrapper };
}

const invalidated = (spy: ReturnType<typeof setup>['spy']) =>
  spy.mock.calls.map(([f]) => JSON.stringify(f?.queryKey));

beforeEach(() => {
  Object.values(api).forEach((m) => m.mockReset());
});

describe('review action hooks', () => {
  it.each([
    ['useApprove', useApprove, 'approvePr', { expectedHeadSha: sha, body: 'LGTM' }],
    [
      'useRequestChanges',
      useRequestChanges,
      'requestChangesPr',
      { expectedHeadSha: sha, body: 'rename it' },
    ],
    ['useMarkReady', useMarkReady, 'markPrReady', { expectedHeadSha: sha }],
  ] as const)('%s calls the API and refreshes the snapshot', async (_n, hook, fn, body) => {
    const { spy, wrapper } = setup();
    api[fn].mockResolvedValue({ prId: 'PR_1', action: 'approve' });
    const { result } = renderHook(() => hook(), { wrapper });
    await act(() => result.current.mutateAsync({ id: 'PR_1', body } as never));
    expect(api[fn]).toHaveBeenCalledWith('PR_1', body);
    expect(invalidated(spy)).toEqual([JSON.stringify(keys.snapshot)]);
  });

  it('refreshes the snapshot when the server refuses, so a moved head shows', async () => {
    const { spy, wrapper } = setup();
    api.approvePr.mockRejectedValue(new ApiError(409, 'head_moved'));
    const { result } = renderHook(() => useApprove(), { wrapper });
    await act(async () => {
      await result.current
        .mutateAsync({ id: 'PR_1', body: { expectedHeadSha: sha } })
        .catch(() => {});
    });
    await waitFor(() => expect(result.current.error).toMatchObject({ code: 'head_moved' }));
    expect(invalidated(spy)).toEqual([JSON.stringify(keys.snapshot)]);
  });

  it('makes no call of its own until it is used', () => {
    const { wrapper } = setup();
    renderHook(() => useApprove(), { wrapper });
    renderHook(() => useRequestChanges(), { wrapper });
    renderHook(() => useMarkReady(), { wrapper });
    expect(api.approvePr).not.toHaveBeenCalled();
    expect(api.requestChangesPr).not.toHaveBeenCalled();
    expect(api.markPrReady).not.toHaveBeenCalled();
    expect(api.getSnapshot).not.toHaveBeenCalled();
  });
});

describe('approvals setting', () => {
  const repo: RepoSettings = {
    id: 'R1',
    nameWithOwner: 'acme/web',
    isNew: true,
    openPrs: 3,
    defaultMethod: 'rebase',
    inheritMethod: true,
    deleteBranch: 'no',
    branchRules: [{ pattern: 'main', method: 'merge' }],
    approvalsRequired: 1,
  };

  it('changes only the approvals and keeps the rest of the repository', () => {
    expect(approvalsUpdate(repo, 2)).toEqual({
      defaultMethod: 'rebase',
      inheritMethod: true,
      deleteBranch: 'no',
      branchRules: [{ pattern: 'main', method: 'merge' }],
      clearNew: false,
      approvalsRequired: 2,
    });
  });

  it('saves through PUT /api/repos/{id} and refreshes the repos and the snapshot', async () => {
    const { spy, wrapper } = setup();
    api.putRepo.mockResolvedValue({ ...repo, approvalsRequired: 2 });
    const { result } = renderHook(() => useSaveApprovals(), { wrapper });
    await act(() => result.current.mutateAsync({ repo, approvalsRequired: 2 }));
    expect(api.putRepo).toHaveBeenCalledWith('R1', approvalsUpdate(repo, 2));
    expect(invalidated(spy)).toEqual([JSON.stringify(keys.repos), JSON.stringify(keys.snapshot)]);
  });
});

describe('rule previews', () => {
  const rule = (id: string) => ({ id }) as SyncRule;

  it('asks once for all rules and pairs each answer with its rule', async () => {
    const { wrapper } = setup();
    const a = { moves: [{ ticketKey: 'INP-1', status: 'To Do', alreadyInTarget: false }], waiting: [] };
    const b = { moves: [], waiting: [] };
    api.previewRules.mockResolvedValue({ previews: [a, b] });
    const { result } = renderHook(() => usePreviewRules(), { wrapper });
    const rules = [rule('a'), rule('b')];
    const out = await act(() => result.current.mutateAsync(rules));
    expect(api.previewRules).toHaveBeenCalledTimes(1);
    expect(api.previewRules).toHaveBeenCalledWith(rules);
    expect(out).toEqual([
      { rule: rules[0], preview: a },
      { rule: rules[1], preview: b },
    ]);
  });
});
