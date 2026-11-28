import { act, fireEvent, screen } from '@testing-library/react';
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError, type MergeResult, type SnapshotPr } from '@/api/client';
import { pr, snapshot } from '@/test-fixtures';
import { renderWithClient } from '@/test-utils';

const api = vi.hoisted(() => ({
  getSnapshot: vi.fn(),
  getSettings: vi.fn(),
  putSettings: vi.fn(),
  syncNow: vi.fn(),
  getRepos: vi.fn(),
  getActivity: vi.fn(),
  getRetries: vi.fn(),
  getAccounts: vi.fn(),
  getSetup: vi.fn(),
  mergePr: vi.fn(),
}));

vi.mock('@/api/client', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/api/client')>()),
  ...api,
}));

class FakeEventSource {
  addEventListener() {}
  close() {}
}

import { CommandTable } from './CommandTable';
import { LAUNCH_MS, LAUNCH_REDUCED_MS } from './List';

const step = (over: Partial<MergeResult['steps'][number]> = {}) => ({
  kind: 'merge' as const,
  ticket: null,
  state: 'ok' as const,
  message: null,
  toStatus: null,
  retryId: null,
  ...over,
});
const merged = (steps = [step()]): MergeResult => ({
  prId: 'p1',
  method: 'squash',
  sha: 'x',
  steps,
});

const two = (): SnapshotPr[] => [pr('p1', 1, { title: 'First' }), pr('p2', 2, { title: 'Second' })];
const card = (id: string) => document.querySelector<HTMLElement>(`article[data-pr="${id}"]`);

const tick = (ms: number) => act(() => vi.advanceTimersByTimeAsync(ms));

/** Opens the dialog of the first card, confirms and merges. The server answers with `result`. */
async function mergeFirst() {
  fireEvent.click(screen.getByRole('button', { name: 'Merge' }));
  // The dialog is a separate chunk: give it a moment to load.
  await tick(50);
  fireEvent.click(screen.getByRole('checkbox', { name: 'Merge without moving any ticket' }));
  fireEvent.click(screen.getByRole('button', { name: 'Merge without ticket' }));
  await tick(50);
}

// The merge dialog is a separate chunk. Loading the module up front keeps the fake timers out of it.
beforeAll(() => import('./MergeDialog'));

beforeEach(async () => {
  Object.values(api).forEach((f) => f.mockReset());
  localStorage.clear();
  api.getSettings.mockResolvedValue({
    polling: { githubSec: 60, jiraSec: 300 },
    theme: 'auto',
    defaultMethodForNewRepos: 'squash',
    deleteBranchAfterMerge: false,
    multiPrTickets: 'all',
    jiraSiteId: null,
    excludedStatuses: [],
  });
  api.getRepos.mockResolvedValue({ repos: [] });
  api.getActivity.mockResolvedValue({ items: [], nextCursor: null });
  api.getRetries.mockResolvedValue({ retries: [] });
  api.getAccounts.mockResolvedValue({
    github: { connected: false, login: null, method: null, warning: null },
    jira: { connected: false, needsReconnect: false, sites: [], selectedSiteId: null },
  });
  api.getSetup.mockResolvedValue({
    github: { configured: true, callbackUrl: '' },
    atlassian: { configured: true, callbackUrl: '' },
  });
  api.getSnapshot.mockResolvedValue(snapshot([two()[0]!]));
  api.mergePr.mockResolvedValue(merged());
  vi.stubGlobal('EventSource', FakeEventSource);
  renderWithClient(<CommandTable />);
  await screen.findByRole('heading', { level: 2, name: 'First' });
  vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'Date'] });
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe('merge launch', () => {
  it('plays "Merged" on the card after the server accepted, keeps the card while the snapshot already dropped it, then removes it and toasts', async () => {
    api.getSnapshot.mockResolvedValue(snapshot([]));
    await mergeFirst();
    expect(api.mergePr).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole('dialog')).toBeNull();
    expect(screen.getByRole('status', { name: 'Merged' })).toBeInTheDocument();
    expect(card('p1')).not.toBeNull();
    expect(screen.queryByText('#1 merged')).toBeNull();

    await tick(LAUNCH_MS - 100);
    expect(card('p1')).not.toBeNull();
    await tick(200);
    expect(card('p1')).toBeNull();
    expect(screen.getByText('#1 merged')).toBeInTheDocument();
  });

  it('does not start before the server answered: the request is sent at once', async () => {
    let finish: (r: MergeResult) => void = () => {};
    api.mergePr.mockReturnValue(new Promise<MergeResult>((r) => (finish = r)));
    await mergeFirst();
    expect(api.mergePr).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole('status', { name: 'Merged' })).toBeNull();
    finish(merged());
    await tick(50);
    expect(screen.getByRole('status', { name: 'Merged' })).toBeInTheDocument();
  });

  it('is shorter under reduced motion', async () => {
    vi.stubGlobal('matchMedia', (query: string) => ({
      matches: query.includes('prefers-reduced-motion'),
      media: query,
      addEventListener() {},
      removeEventListener() {},
    }));
    api.getSnapshot.mockResolvedValue(snapshot([]));
    await mergeFirst();
    await tick(LAUNCH_REDUCED_MS - 100);
    expect(card('p1')).not.toBeNull();
    await tick(200);
    expect(card('p1')).toBeNull();
    expect(screen.getByText('#1 merged')).toBeInTheDocument();
    expect(LAUNCH_REDUCED_MS).toBeLessThan(LAUNCH_MS);
  });

  it('sums up a failed ticket move in an error toast that says where to retry', async () => {
    api.mergePr.mockResolvedValue(
      merged([step(), step({ kind: 'transition', ticket: 'INP-1', state: 'failed', retryId: 3 })]),
    );
    await mergeFirst();
    await tick(LAUNCH_MS + 100);
    expect(
      screen.getByText(/#1 merged, INP-1 did not move. Retry it from History/),
    ).toBeInTheDocument();
  });

  it('only toasts for a pull request that was added to the merge queue, without a launch', async () => {
    api.mergePr.mockResolvedValue(merged([step({ state: 'queued' })]));
    await mergeFirst();
    expect(screen.queryByRole('status', { name: 'Merged' })).toBeNull();
    expect(screen.getByText('#1 added to the merge queue')).toBeInTheDocument();
    expect(card('p1')).not.toBeNull();
  });

  it('plays nothing when the merge is refused, and the dialog stays with the reason', async () => {
    api.mergePr.mockRejectedValue(new ApiError(409, 'not_mergeable'));
    await mergeFirst();
    expect(screen.queryByRole('status', { name: 'Merged' })).toBeNull();
    expect(screen.getByRole('dialog')).toBeInTheDocument();
    expect(screen.getByRole('alert')).toHaveTextContent('cannot be merged right now');
  });
});
