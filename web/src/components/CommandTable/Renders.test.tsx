import { QueryClientProvider } from '@tanstack/react-query';
import { act, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { SnapshotPr } from '@/api/client';
import { keys } from '@/api/queries';
import { makeQueryClient } from '@/app/providers';
import { pr, snapshot } from '@/test-fixtures';

const api = vi.hoisted(() => ({
  getSnapshot: vi.fn(),
  getSettings: vi.fn(),
  putSettings: vi.fn(),
  getRepos: vi.fn(),
  getActivity: vi.fn(),
  getRetries: vi.fn(),
  getAccounts: vi.fn(),
  getSetup: vi.fn(),
}));

vi.mock('@/api/client', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/api/client')>()),
  ...api,
}));

// Counters at the edges of the subtrees under test: what a render of the part costs.
const renders = vi.hoisted(() => ({
  sidebar: 0,
  refresh: 0,
  theme: 0,
  merge: new Map<number, number>(),
}));

vi.mock('../Settings/SettingsContent', () => ({
  SettingsContent: () => {
    renders.sidebar += 1;
    return null;
  },
}));
// The real buttons are memoized, so the stand-ins are too.
vi.mock('../Header/RefreshButton', async () => {
  const { memo } = await import('react');
  return {
    RefreshButton: memo(function RefreshButton() {
      renders.refresh += 1;
      return null;
    }),
  };
});
vi.mock('../Header/ThemeButton', async () => {
  const { memo } = await import('react');
  return {
    ThemeButton: memo(function ThemeButton() {
      renders.theme += 1;
      return null;
    }),
  };
});
vi.mock('./MergeCell', () => ({
  MergeCell: ({ pr }: { pr: SnapshotPr }) => {
    renders.merge.set(pr.number, (renders.merge.get(pr.number) ?? 0) + 1);
    return null;
  },
}));

class FakeEventSource {
  addEventListener() {}
  close() {}
}

import { CommandTable, STALE_TICK_MS } from './CommandTable';

const settings = {
  polling: { githubSec: 60, jiraSec: 300 },
  theme: 'light',
  defaultMethodForNewRepos: 'squash',
  deleteBranchAfterMerge: false,
  multiPrTickets: 'all',
  jiraSiteId: null,
  excludedStatuses: ['Done'],
};

const rows = () => [
  pr('a', 1, { title: 'Alpha login' }),
  pr('b', 2, { title: 'Alpha billing' }),
  pr('c', 3, { title: 'Alpha export' }),
];

async function open() {
  api.getSnapshot.mockImplementation(() => Promise.resolve(snapshot(rows())));
  const client = makeQueryClient();
  render(
    <QueryClientProvider client={client}>
      <CommandTable />
    </QueryClientProvider>,
  );
  await screen.findByText('Alpha export');
  return client;
}

const mergeRenders = () => [...renders.merge.entries()].sort(([a], [b]) => a - b);

beforeEach(() => {
  Object.values(api).forEach((f) => f.mockReset());
  localStorage.clear();
  localStorage.setItem('prd-hidef', 'all');
  api.getSettings.mockResolvedValue(settings);
  api.getRepos.mockResolvedValue({ repos: [] });
  api.getActivity.mockResolvedValue({ items: [], nextCursor: null });
  api.getRetries.mockResolvedValue({ retries: [] });
  api.getAccounts.mockResolvedValue({
    github: { connected: true, login: 'priya', method: 'pat', warning: null },
    jira: { connected: false, needsReconnect: false, sites: [], selectedSiteId: null },
  });
  api.getSetup.mockResolvedValue({
    github: { configured: true, callbackUrl: '' },
    atlassian: { configured: true, callbackUrl: '' },
  });
  vi.stubGlobal('EventSource', FakeEventSource);
  renders.sidebar = renders.refresh = renders.theme = 0;
  renders.merge.clear();
});

describe('what a change renders', () => {
  it('a keystroke in the search does not render the sidebar or the header buttons', async () => {
    await open();
    const before = { ...renders, merge: undefined };
    const search = screen.getByPlaceholderText('Title, repo, PR or Jira key');
    for (const text of ['A', 'Al', 'Alp', 'Alph']) {
      fireEvent.change(search, { target: { value: text } });
    }
    expect(search).toHaveValue('Alph');
    expect(renders.sidebar).toBe(before.sidebar);
    expect(renders.refresh).toBe(before.refresh);
    expect(renders.theme).toBe(before.theme);
  });

  it('a keystroke that leaves the same cards does not render them', async () => {
    await open();
    const before = mergeRenders();
    fireEvent.change(screen.getByPlaceholderText('Title, repo, PR or Jira key'), {
      target: { value: 'alpha' },
    });
    expect(screen.getAllByRole('heading', { level: 2 })).toHaveLength(3);
    expect(mergeRenders()).toEqual(before);
  });

  it('a snapshot identical to the one shown renders nothing again', async () => {
    const client = await open();
    const before = { sidebar: renders.sidebar, refresh: renders.refresh, merge: mergeRenders() };
    api.getSnapshot.mockClear();
    await act(() => client.invalidateQueries({ queryKey: keys.snapshot }));
    expect(api.getSnapshot).toHaveBeenCalledTimes(1);
    expect(renders.sidebar).toBe(before.sidebar);
    expect(renders.refresh).toBe(before.refresh);
    expect(mergeRenders()).toEqual(before.merge);
  });

  it('hiding one card renders that card and no other', async () => {
    await open();
    const before = new Map(renders.merge);
    fireEvent.click(screen.getByRole('checkbox', { name: 'Hide #2' }));
    expect(screen.getByRole('checkbox', { name: 'Hide #2' })).toBeChecked();
    expect(renders.merge.get(2)).toBeGreaterThan(before.get(2)!);
    expect(renders.merge.get(1)).toBe(before.get(1));
    expect(renders.merge.get(3)).toBe(before.get(3));
  });
});

describe('the clock of the Stale filter', () => {
  afterEach(() => vi.useRealTimers());

  it('ticks once an hour and renders nothing in between', async () => {
    vi.useFakeTimers({ toFake: ['Date', 'setInterval', 'clearInterval'] });
    const start = Date.parse('2026-11-24T12:00:00Z');
    vi.setSystemTime(start);
    // Ten minutes short of three days old: a minute clock would call it stale after ten minutes.
    const committedAt = new Date(start - 3 * 24 * 3_600_000 + 10 * 60_000).toISOString();
    api.getSnapshot.mockImplementation(() =>
      Promise.resolve(snapshot([pr('a', 1, { title: 'Alpha login', committedAt })])),
    );
    render(
      <QueryClientProvider client={makeQueryClient()}>
        <CommandTable />
      </QueryClientProvider>,
    );
    await screen.findByText('Alpha login');
    const stale = () =>
      screen.getByRole('group', { name: 'Summary' }).querySelector('button:nth-child(6)')!
        .textContent;
    expect(STALE_TICK_MS).toBe(3_600_000);
    expect(stale()).toBe('Stale0');
    const before = renders.sidebar;

    act(() => vi.advanceTimersByTime(30 * 60_000));
    expect(stale()).toBe('Stale0');
    expect(renders.sidebar).toBe(before);

    act(() => vi.advanceTimersByTime(30 * 60_000));
    expect(stale()).toBe('Stale1');
  });
});
