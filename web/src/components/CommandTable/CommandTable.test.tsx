import { act, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { pr, snapshot, ticket } from '@/test-fixtures';
import { renderWithClient } from '@/test-utils';
import type { Snapshot } from '@/api/client';

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
}));
const renders = vi.hoisted(() => new Map<string, number>());

vi.mock('@/api/client', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/api/client')>()),
  ...api,
}));

vi.mock('./CiCell', () => ({
  CiCell: ({ pr }: { pr: { id: string } }) => {
    renders.set(pr.id, (renders.get(pr.id) ?? 0) + 1);
    return <span>ci of {pr.id}</span>;
  },
}));

class FakeEventSource {
  static last: FakeEventSource | undefined;
  readonly listeners = new Map<string, (e: MessageEvent<string>) => void>();
  closed = false;
  constructor(readonly url: string) {
    FakeEventSource.last = this;
  }
  addEventListener(name: string, fn: (e: MessageEvent<string>) => void) {
    this.listeners.set(name, fn);
  }
  close() {
    this.closed = true;
  }
  emit(name: string, data: unknown) {
    this.listeners.get(name)?.({ data: JSON.stringify(data) } as MessageEvent<string>);
  }
}

import { CommandTable } from './CommandTable';

const settings = {
  polling: { githubSec: 60, jiraSec: 300 },
  theme: 'auto',
  defaultMethodForNewRepos: 'squash',
  deleteBranchAfterMerge: false,
  jiraSiteId: null,
  excludedStatuses: ['Done'],
};

const rows = () => [
  pr('a', 1, {
    title: 'Add login',
    tickets: [ticket('INP-1')],
    stack: { id: 'a', position: 1, size: 2, parentId: null, parentNumber: null },
  }),
  pr('b', 2, {
    title: 'SSO on top',
    stack: { id: 'a', position: 2, size: 2, parentId: 'a', parentNumber: 1 },
  }),
  pr('c', 3, { title: 'Plain fix', isDraft: true }),
  pr('d', 4, {
    title: 'Conflicted',
    mergeable: 'CONFLICTING',
    committedAt: '2026-01-01T00:00:00Z',
  }),
];

beforeEach(() => {
  Object.values(api).forEach((f) => f.mockReset());
  renders.clear();
  api.getSettings.mockResolvedValue(settings);
  api.putSettings.mockImplementation((s: unknown) => Promise.resolve(s));
  api.syncNow.mockResolvedValue({ running: true });
  api.getSnapshot.mockResolvedValue(snapshot(rows()));
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
  vi.stubGlobal('EventSource', FakeEventSource);
  document.documentElement.removeAttribute('data-mode');
  document.documentElement.removeAttribute('data-theme');
});

describe('CommandTable', () => {
  it('shows the columns in order, one row per pull request, and the merge reason', async () => {
    renderWithClient(<CommandTable />);
    expect(await screen.findByText('Add login')).toBeInTheDocument();
    const heads = screen.getAllByRole('columnheader').map((h) => h.textContent);
    expect(heads).toEqual([
      'Repo / PR',
      'Ticket',
      'Last commit',
      'Review',
      'CI',
      'Method',
      'Merge',
    ]);
    expect(screen.getByRole('link', { name: 'Add login' })).toHaveAttribute(
      'href',
      'https://github.com/acme/web/pull/1',
    );
    expect(screen.getByRole('link', { name: 'INP-1' })).toBeInTheDocument();
    const child = screen.getByText('SSO on top').closest('tr')!;
    expect(within(child).getByText('Merge after #1')).toBeInTheDocument();
    expect(within(child).getByRole('button', { name: 'Merge' })).toBeDisabled();
    expect(
      within(screen.getByText('Plain fix').closest('tr')!).getByText('Draft', { selector: 'div' }),
    ).toBeInTheDocument();
    expect(screen.getByText('Merge conflicts')).toBeInTheDocument();
    expect(screen.getByText('Stack of 2')).toBeInTheDocument();
  });

  it('counts the stat strip and filters with the chips', async () => {
    renderWithClient(<CommandTable />);
    await screen.findByText('Add login');
    const stats = document.querySelector('dl')!;
    expect(stats.textContent).toBe('Open4Ready1Blocked2Draft1Stale1');
    fireEvent.click(screen.getByRole('button', { name: 'Draft' }));
    expect(screen.queryByText('Add login')).toBeNull();
    expect(screen.getByText('Plain fix')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Stale' }));
    expect(screen.getByText('Conflicted')).toBeInTheDocument();
    expect(screen.queryByText('Plain fix')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'All' }));
    expect(screen.getByText('Add login')).toBeInTheDocument();
  });

  it('shows an Authorize link for a blocked organization while other rows load', async () => {
    api.getSnapshot.mockResolvedValue(
      snapshot(rows(), {
        sources: {
          ...snapshot([]).sources,
          blockedOrgs: [{ org: 'acme-corp' }, { org: '../evil' }, { org: '' }],
        },
      }),
    );
    renderWithClient(<CommandTable />);
    expect(await screen.findByText('Add login')).toBeInTheDocument();
    const alerts = screen.getAllByRole('alert');
    expect(alerts).toHaveLength(3);
    const authorize = screen.getAllByRole('link', { name: 'Authorize' });
    expect(authorize).toHaveLength(1);
    expect(authorize[0]).toHaveAttribute('href', 'https://github.com/orgs/acme-corp/sso');
    expect(alerts[0]).toHaveTextContent('acme-corp has not authorized this token');
  });

  it('shows source problems and warnings', async () => {
    const base = snapshot([]);
    api.getSnapshot.mockResolvedValue(
      snapshot([], {
        sources: {
          github: { ...base.sources.github, state: 'rate_limited' },
          jira: {
            ...base.sources.jira,
            state: 'unreachable',
            error: 'Jira unreachable, retry the lookup',
          },
          blockedOrgs: [],
        },
        warnings: [{ code: 'github_search_cap', message: 'GitHub returns at most 1,000 results.' }],
      }),
    );
    renderWithClient(<CommandTable />);
    expect(await screen.findByText(/rate limit reached/)).toBeInTheDocument();
    expect(screen.getByText('Jira unreachable, retry the lookup')).toBeInTheDocument();
    expect(screen.getByText('GitHub returns at most 1,000 results.')).toBeInTheDocument();
    expect(screen.getByText('No pull requests match.')).toBeInTheDocument();
  });

  it('offers to connect GitHub when it is disconnected', async () => {
    const base = snapshot([]);
    api.getSnapshot.mockResolvedValue(
      snapshot([], {
        sources: { ...base.sources, github: { ...base.sources.github, state: 'disconnected' } },
      }),
    );
    renderWithClient(<CommandTable />);
    expect(await screen.findByRole('link', { name: 'Connect GitHub' })).toHaveAttribute(
      'href',
      expect.stringMatching(/^\/accounts\/?$/),
    );
  });

  it('syncs on demand and follows the sync status events', async () => {
    renderWithClient(<CommandTable />);
    await screen.findByText('Add login');
    expect(screen.queryByRole('button', { name: 'Sync' })).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Refresh' }));
    await waitFor(() => expect(api.syncNow).toHaveBeenCalledTimes(1));
    act(() => {
      FakeEventSource.last!.emit('sync-status', {
        running: true,
        githubAt: null,
        jiraAt: null,
        nextGithubAt: null,
        nextJiraAt: null,
      });
    });
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Refresh' })).toHaveAttribute('aria-busy', 'true'),
    );
    act(() => {
      FakeEventSource.last!.emit('sync-status', {
        running: false,
        githubAt: '2026-11-24T11:59:00Z',
        jiraAt: null,
        nextGithubAt: new Date(Date.now() + 90_000).toISOString(),
        nextJiraAt: null,
      });
    });
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Refresh' })).not.toHaveAttribute('aria-busy'),
    );
    // The sync details are in the popover of the refresh button.
    fireEvent.focus(screen.getByRole('button', { name: 'Refresh' }));
    expect(screen.getByRole('tooltip')).toHaveTextContent(/Next sync in 1m/);
  });

  it('refetches the snapshot on a snapshot event and ignores malformed status events', async () => {
    renderWithClient(<CommandTable />);
    await screen.findByText('Add login');
    expect(FakeEventSource.last!.url).toBe('/events');
    api.getSnapshot.mockResolvedValue(
      snapshot([pr('z', 9, { title: 'Brand new' })], { version: 2 }),
    );
    act(() => {
      FakeEventSource.last!.emit('sync-status', { nope: true });
      FakeEventSource.last!.listeners.get('sync-status')?.({ data: '{' } as MessageEvent<string>);
      FakeEventSource.last!.emit('snapshot', { version: 2 });
    });
    expect(await screen.findByText('Brand new')).toBeInTheDocument();
    expect(screen.queryByText('Add login')).toBeNull();
  });

  it('closes the event stream on unmount', async () => {
    const { unmount } = renderWithClient(<CommandTable />);
    await screen.findByText('Add login');
    const source = FakeEventSource.last!;
    unmount();
    expect(source.closed).toBe(true);
  });

  it('applies the stored theme and has no theme control in the top bar', async () => {
    api.getSettings.mockResolvedValue({ ...settings, theme: 'dark' });
    renderWithClient(<CommandTable />);
    await screen.findByText('Add login');
    await waitFor(() => expect(document.documentElement.dataset.mode).toBe('dark'));
    expect(document.documentElement.dataset.theme).toBe('night');
    expect(screen.queryByRole('group', { name: 'Theme' })).toBeNull();
    expect(screen.queryByRole('checkbox', { name: 'Dark mode' })).toBeNull();
    expect(screen.queryByRole('link', { name: 'Accounts' })).toBeNull();
  });

  it('re-renders only the row whose pull request changed', async () => {
    renderWithClient(<CommandTable />);
    await screen.findByText('Add login');
    await waitFor(() => expect(renders.get('d')).toBeGreaterThan(0));
    const before = new Map(renders);

    const next: Snapshot = snapshot(
      rows().map((r) => (r.id === 'c' ? { ...r, title: 'Plain fix, reworded' } : r)),
      { version: 2 },
    );
    api.getSnapshot.mockResolvedValue(next);
    act(() => FakeEventSource.last!.emit('snapshot', { version: 2 }));
    await screen.findByText('Plain fix, reworded');

    expect(renders.get('c')).toBe((before.get('c') ?? 0) + 1);
    for (const id of ['a', 'b', 'd']) expect(renders.get(id)).toBe(before.get(id));
  });

  it('shows the resolved merge method in the Method column', async () => {
    api.getSnapshot.mockResolvedValue(
      snapshot([
        pr('a', 1, {
          title: 'Add login',
          mergeMethod: {
            method: 'merge',
            default: 'squash',
            source: 'rule: main',
            allowed: ['merge'],
            warning: 'Repo disallows squash merge, using merge commit',
          },
        }),
      ]),
    );
    renderWithClient(<CommandTable />);
    const row = (await screen.findByText('Add login')).closest('tr')!;
    expect(within(row).getByRole('combobox', { name: 'Merge method' })).toHaveTextContent(
      'Merge commit (default)',
    );
    expect(within(row).getByText('Branch rule main')).toBeInTheDocument();
    expect(
      within(row).getByText('Repo disallows squash merge, using merge commit'),
    ).toBeInTheDocument();
  });

  it('opens the merge panel under a ready row and closes it again', async () => {
    renderWithClient(<CommandTable />);
    const row = (await screen.findByText('Add login')).closest('tr')!;
    const button = within(row).getByRole('button', { name: 'Merge' });
    fireEvent.click(button);
    expect(button).toHaveAttribute('aria-expanded', 'true');
    expect(screen.getByRole('group', { name: 'Merge #1' })).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(screen.queryByRole('group', { name: 'Merge #1' })).toBeNull();
  });

  it('opens the settings sidebar from the burger and closes it with Escape', async () => {
    renderWithClient(<CommandTable />);
    await screen.findByText('Add login');
    expect(screen.queryByRole('button', { name: 'Settings' })).toBeNull();
    const burger = screen.getByRole('button', { name: 'Settings sidebar' });
    expect(burger).toHaveAttribute('aria-expanded', 'false');
    fireEvent.click(burger);
    expect(burger).toHaveAttribute('aria-expanded', 'true');
    const dialog = screen.getByRole('dialog', { name: 'Settings' });
    expect(
      await within(dialog).findByRole('heading', { name: /^Repositories setting/ }),
    ).toBeInTheDocument();
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(screen.queryByRole('dialog')).toBeNull();
    expect(burger).toHaveFocus();
  });

  it('opens the account panel from Manage accounts in the sidebar', async () => {
    renderWithClient(<CommandTable />);
    await screen.findByText('Add login');
    expect(screen.queryByRole('button', { name: 'Account' })).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Settings sidebar' }));
    const manage = await screen.findByRole('button', { name: 'Manage accounts' });
    manage.focus();
    fireEvent.click(manage);
    const dialog = screen.getByRole('dialog', { name: 'Account' });
    expect(
      await within(dialog).findByRole('heading', { name: 'Connected accounts' }),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Close account' }));
    expect(screen.queryByRole('dialog', { name: 'Account' })).toBeNull();
    expect(manage).toHaveFocus();

    fireEvent.click(manage);
    fireEvent.click(screen.getByTestId('side-panel-scrim'));
    expect(screen.queryByRole('dialog', { name: 'Account' })).toBeNull();
    expect(screen.getByRole('dialog', { name: 'Settings' })).toBeInTheDocument();
    expect(screen.queryByRole('heading', { name: 'Connected accounts' })).toBeNull();
  });

  it('shows the connected accounts at the bottom of the sidebar', async () => {
    api.getAccounts.mockResolvedValue({
      github: { connected: true, login: 'minatpt', method: 'app' },
      jira: {
        connected: true,
        needsReconnect: false,
        sites: [{ id: 's1', name: 'acme', url: 'https://acme.atlassian.net' }],
        selectedSiteId: 's1',
      },
    });
    renderWithClient(<CommandTable />);
    await screen.findByText('Add login');
    fireEvent.click(screen.getByRole('button', { name: 'Settings sidebar' }));
    const accounts = await screen.findByRole('region', { name: 'Accounts' });
    expect(await within(accounts).findByText('minatpt')).toBeInTheDocument();
    expect(within(accounts).getByText('GitHub App')).toBeInTheDocument();
    expect(within(accounts).getByText('acme')).toBeInTheDocument();
    expect(within(accounts).getByText('Atlassian')).toBeInTheDocument();
  });

  it('has no Sync or Account button in the header', async () => {
    renderWithClient(<CommandTable />);
    await screen.findByText('Add login');
    expect(screen.queryByRole('button', { name: 'Sync' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Account' })).toBeNull();
    expect(screen.getByRole('button', { name: 'Refresh' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /^Theme:/ })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'History' })).toBeNull();
  });

  it('switches the theme from the header button', async () => {
    api.getSettings.mockResolvedValue({ ...settings, theme: 'light' });
    renderWithClient(<CommandTable />);
    await screen.findByText('Add login');
    await waitFor(() => expect(document.documentElement.dataset.mode).toBe('light'));
    fireEvent.click(await screen.findByRole('button', { name: 'Theme: light' }));
    await waitFor(() =>
      expect(api.putSettings).toHaveBeenLastCalledWith({ ...settings, theme: 'dark' }),
    );
  });
});
