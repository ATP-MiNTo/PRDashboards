import { QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react';
import type { ReactNode } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { makeQueryClient } from '@/app/providers';
import { ApiError, type RepoSettings } from '@/api/client';

const api = vi.hoisted(() => ({
  approvePr: vi.fn(),
  requestChangesPr: vi.fn(),
  markPrReady: vi.fn(),
  putRepo: vi.fn(),
  getSnapshot: vi.fn(),
  getRepos: vi.fn(),
  putGithubPat: vi.fn(),
  putSetupClient: vi.fn(),
  putSettings: vi.fn(),
  revokeAllSessions: vi.fn(),
  deleteGithubToken: vi.fn(),
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
  useRemoveToken,
  useRequestChanges,
  useRevokeAll,
  useSaveApprovals,
  useSaveClient,
  useSavePat,
  useSaveSettings,
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

describe('account and settings actions', () => {
  async function run<T>(
    use: () => { mutateAsync: (v: never) => Promise<unknown> },
    vars: T,
    seed?: (c: ReturnType<typeof setup>['client']) => void,
  ) {
    const { client, spy, wrapper } = setup();
    seed?.(client);
    const { result } = renderHook(use, { wrapper });
    await act(() => result.current.mutateAsync(vars as never));
    return { client, spy };
  }

  it('invalidates only the keys the change touches, never the sync status or the session', async () => {
    api.putGithubPat.mockResolvedValue({});
    const { spy } = await run(useSavePat, 'ghp_x');
    const got = invalidated(spy);
    expect(got).toEqual(
      [keys.accounts, keys.settings, keys.snapshot, keys.repos, keys.assigned].map((k) =>
        JSON.stringify(k),
      ),
    );
    expect(got).not.toContain(JSON.stringify(keys.syncStatus));
    expect(got).not.toContain(JSON.stringify(keys.session));
  });

  it('saves the client credentials and refreshes the setup and the accounts only', async () => {
    api.putSetupClient.mockResolvedValue(undefined);
    const { spy } = await run(useSaveClient, {
      provider: 'github',
      clientId: 'a',
      clientSecret: 'b',
    });
    expect(invalidated(spy)).toEqual([JSON.stringify(keys.setup), JSON.stringify(keys.accounts)]);
  });

  it('keeps the sync status on a settings save, and refetches nothing for revoking sessions', async () => {
    api.putSettings.mockResolvedValue({});
    api.revokeAllSessions.mockResolvedValue(undefined);
    const status = { running: false } as never;
    const seeded = (c: ReturnType<typeof setup>['client']) =>
      c.setQueryData(keys.syncStatus, status);
    const saved = await run(useSaveSettings, {}, seeded);
    expect(invalidated(saved.spy)).not.toContain(JSON.stringify(keys.syncStatus));
    expect(saved.client.getQueryData(keys.syncStatus)).toBe(status);
    expect(api.getSnapshot).not.toHaveBeenCalled();
    const revoked = await run(useRevokeAll, undefined);
    expect(invalidated(revoked.spy)).toEqual([]);
  });

  it('a removed token refreshes the snapshot but leaves the sync status alone', async () => {
    api.deleteGithubToken.mockResolvedValue(undefined);
    const { client } = await run(useRemoveToken, 't1', (c) =>
      c.setQueryData(keys.syncStatus, { running: true }),
    );
    expect(client.getQueryData(keys.syncStatus)).toEqual({ running: true });
  });
});
