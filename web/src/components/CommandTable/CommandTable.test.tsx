import { act, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { NOW, pr, snapshot, ticket } from '@/test-fixtures';
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
  getRules: vi.fn(),
  getJiraOptions: vi.fn(),
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
  api.getRules.mockResolvedValue({ rules: [] });
  api.getJiraOptions.mockResolvedValue({ statuses: [], projects: [] });
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
  localStorage.clear();
});

describe('CommandTable', () => {
  it('shows one card per pull request, with its controls and the merge reason', async () => {
    renderWithClient(<CommandTable />);
    expect(await screen.findByText('Add login')).toBeInTheDocument();
    expect(document.querySelectorAll('article[data-pr]')).toHaveLength(4);
    expect(document.querySelector('table')).toBeNull();
    expect(screen.getByRole('heading', { level: 2, name: 'Add login' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Add login' })).toHaveAttribute(
      'href',
      'https://github.com/acme/web/pull/1',
    );
    const own = screen.getByText('Add login').closest('article')!;
    expect(within(own).getByRole('link', { name: 'INP-1 · In Review' })).toBeInTheDocument();
    const child = screen.getByText('SSO on top').closest('article')!;
    const blocked = within(child).getByRole('button', { name: 'Merge' });
    expect(blocked).toHaveAttribute('aria-disabled', 'true');
    expect(blocked).toHaveAccessibleDescription('Merge after #1');
    expect(within(child).getByRole('checkbox', { name: 'Hide #2' })).toBeInTheDocument();
    const draft = screen.getByText('Plain fix').closest('article')!;
    expect(within(draft).getByText('Draft', { selector: 'span' })).toBeInTheDocument();
    expect(within(draft).getByRole('button', { name: 'Ready' })).toBeInTheDocument();
    expect(within(draft).queryByRole('button', { name: 'Merge' })).toBeNull();
    expect(screen.getByText('Merge conflicts')).toBeInTheDocument();
  });

  it('draws a stack as one group with a banner above its cards', async () => {
    renderWithClient(<CommandTable />);
    await screen.findByText('Add login');
    const group = screen.getByRole('region', { name: 'Stack' });
    expect(within(group).getByText('STACK')).toBeInTheDocument();
    expect(within(group).getByText('2 PRs, merge in order')).toBeInTheDocument();
    expect(within(group).getAllByText('INP-1 · In Review').length).toBeGreaterThan(0);
    expect(group.querySelectorAll('article[data-pr]')).toHaveLength(2);
    expect(within(group).getByText('Stack 1/2')).toBeInTheDocument();
    expect(within(group).getByText('Stack 2/2')).toBeInTheDocument();
  });

  describe('stat tiles', () => {
    // The Stale count reads the clock: pin it to just after the fixtures' last commits.
    beforeEach(() => {
      vi.useFakeTimers({ toFake: ['Date'] });
      vi.setSystemTime(NOW);
    });
    afterEach(() => vi.useRealTimers());

    it('counts the stat tiles and filters with them', async () => {
      renderWithClient(<CommandTable />);
      await screen.findByText('Add login');
      const tiles = screen.getByRole('group', { name: 'Summary' });
      expect(
        within(tiles)
          .getAllByRole('button')
          .map((b) => b.textContent),
      ).toEqual([
        'Showing4',
        'Ready to merge1',
        'Running CI0',
        'Blocked2',
        'Draft1',
        'Stale1',
        'Stacks1',
      ]);
      const tile = (name: RegExp) => within(tiles).getByRole('button', { name });
      expect(tile(/^Showing/)).toHaveAttribute('aria-pressed', 'true');
      fireEvent.click(tile(/^Stacks/));
      expect(tile(/^Stacks/)).toHaveAttribute('aria-pressed', 'true');
      expect(screen.getByText('Add login')).toBeInTheDocument();
      expect(screen.queryByText('Plain fix')).toBeNull();
      fireEvent.click(tile(/^Ready to merge/));
      expect(screen.queryByText('Conflicted')).toBeNull();
      expect(screen.queryByText('Plain fix')).toBeNull();
      expect(screen.queryByText('SSO on top')).toBeNull();
      expect(screen.getByText('Add login')).toBeInTheDocument();
      fireEvent.click(tile(/^Running CI/));
      expect(screen.getByText('Nothing here')).toBeInTheDocument();
      fireEvent.click(tile(/^Showing/));
      expect(screen.getByText('Plain fix')).toBeInTheDocument();
    });

    it('tints only the Blocked tile red, in the strip, and keeps it pressed when it is the filter', async () => {
      renderWithClient(<CommandTable />);
      await screen.findByText('Add login');
      const tiles = screen.getByRole('group', { name: 'Summary' });
      const tile = (name: RegExp) => within(tiles).getByRole('button', { name });
      expect(tile(/^Blocked/).className).toMatch(/statBlocked/);
      for (const other of [/^Showing/, /^Ready/, /^Running/, /^Draft/, /^Stale/, /^Stacks/]) {
        expect(tile(other).className).not.toMatch(/statBlocked/);
      }
      expect(tile(/^Blocked/).className).not.toMatch(/statOn/);
      fireEvent.click(tile(/^Blocked/));
      expect(tile(/^Blocked/)).toHaveAttribute('aria-pressed', 'true');
      expect(tile(/^Blocked/).className).toMatch(/statBlocked.*statOn/);
      expect(tiles.querySelector('[style]')).toBeNull();
    });

    it('filters to the blocked, draft and stale pull requests', async () => {
      renderWithClient(<CommandTable />);
      await screen.findByText('Add login');
      const tiles = screen.getByRole('group', { name: 'Summary' });
      const tile = (name: RegExp) => within(tiles).getByRole('button', { name });
      const shown = () =>
        ['Add login', 'SSO on top', 'Plain fix', 'Conflicted'].filter(
          (t) => screen.queryByText(t) !== null,
        );
      fireEvent.click(tile(/^Blocked/));
      expect(tile(/^Blocked/)).toHaveAttribute('aria-pressed', 'true');
      expect(shown()).toEqual(['SSO on top', 'Conflicted']);
      fireEvent.click(tile(/^Draft/));
      expect(shown()).toEqual(['Plain fix']);
      fireEvent.click(tile(/^Stale/));
      expect(shown()).toEqual(['Conflicted']);
      // The numbers stay whole while one of them is the filter.
      expect(tile(/^Blocked/)).toHaveTextContent('2');
      fireEvent.click(tile(/^Showing/));
      expect(shown()).toHaveLength(4);
    });

    describe('in the header once the strip has scrolled away', () => {
      let observe: (entry: Partial<IntersectionObserverEntry>) => void;
      beforeEach(() => {
        vi.stubGlobal(
          'IntersectionObserver',
          class {
            constructor(cb: IntersectionObserverCallback) {
              observe = (entry) =>
                act(() => cb([entry as IntersectionObserverEntry], this as never));
            }
            observe() {}
            disconnect() {}
          },
        );
      });
      afterEach(() => vi.unstubAllGlobals());

      // Hidden, it has no accessible name to query by, so it is found by its label.
      const compact = () =>
        document.querySelector<HTMLElement>('[aria-label="Summary, compact"]') as HTMLElement;
      const strip = (bottom: number, isIntersecting: boolean) =>
        observe({ isIntersecting, boundingClientRect: { bottom } as DOMRectReadOnly });

      it('is inert and hidden while the strip is in view, and shows once it is not', async () => {
        renderWithClient(<CommandTable />);
        await screen.findByText('Add login');
        expect(compact()).toHaveAttribute('aria-hidden', 'true');
        expect(compact()).toHaveAttribute('inert');
        expect(screen.queryByRole('group', { name: 'Summary, compact' })).toBeNull();
        strip(300, true);
        expect(compact()).toHaveAttribute('aria-hidden', 'true');
        // Below the fold, not scrolled past: still hidden.
        strip(2000, false);
        expect(compact()).toHaveAttribute('aria-hidden', 'true');
        strip(20, false);
        expect(compact()).not.toHaveAttribute('aria-hidden');
        expect(compact()).not.toHaveAttribute('inert');
        const names = within(compact())
          .getAllByRole('button')
          .map((b) => b.getAttribute('aria-label'));
        expect(names).toEqual([
          'Showing: 4',
          'Ready to merge: 1',
          'Running CI: 0',
          'Blocked: 2',
          'Draft: 1',
          'Stale: 1',
          'Stacks: 1',
        ]);
        strip(300, true);
        expect(compact()).toHaveAttribute('aria-hidden', 'true');
      });

      it('tints the Blocked button red like the tile, and keeps it pressed when it is on', async () => {
        renderWithClient(<CommandTable />);
        await screen.findByText('Add login');
        strip(20, false);
        const blocked = () => within(compact()).getByRole('button', { name: 'Blocked: 2' });
        expect(blocked().className).toMatch(/csBlocked/);
        const others = within(compact())
          .getAllByRole('button')
          .filter((b) => b !== blocked());
        expect(others).toHaveLength(6);
        for (const b of others) expect(b.className).not.toMatch(/csBlocked/);
        fireEvent.click(blocked());
        expect(blocked()).toHaveAttribute('aria-pressed', 'true');
        expect(blocked().className).toMatch(/csBlocked.*csOn/);
      });

      it('applies the same filters as the tiles, and shows which one is on', async () => {
        renderWithClient(<CommandTable />);
        await screen.findByText('Add login');
        strip(20, false);
        fireEvent.click(within(compact()).getByRole('button', { name: 'Draft: 1' }));
        expect(screen.getByText('Plain fix')).toBeInTheDocument();
        expect(screen.queryByText('Add login')).toBeNull();
        expect(within(compact()).getByRole('button', { name: 'Draft: 1' })).toHaveAttribute(
          'aria-pressed',
          'true',
        );
        expect(
          within(screen.getByRole('group', { name: 'Summary' })).getByRole('button', {
            name: /^Draft/,
          }),
        ).toHaveAttribute('aria-pressed', 'true');
        // "Showing" counts the filtered list, and lifts the filter.
        fireEvent.click(within(compact()).getByRole('button', { name: 'Showing: 1' }));
        expect(screen.getByText('Add login')).toBeInTheDocument();
      });
    });
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
    expect(screen.getByText('Nothing here')).toBeInTheDocument();
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

  it('shows the resolved merge method and where it comes from in the merge dialog', async () => {
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
    const row = (await screen.findByText('Add login')).closest('article')!;
    expect(within(row).queryByRole('combobox', { name: 'Merge method' })).toBeNull();
    fireEvent.click(within(row).getByRole('button', { name: 'Merge' }));
    const dialog = screen.getByRole('dialog', { name: 'Merge acme/web #1?' });
    expect(within(dialog).getByRole('button', { name: 'Merge method' })).toHaveTextContent(
      'merge commit',
    );
    expect(within(dialog).getByText(/from branch rule main/)).toBeInTheDocument();
    expect(
      within(dialog).getByText('Repo disallows squash merge, using merge commit'),
    ).toBeInTheDocument();
  });

  it('opens the merge dialog from a ready card and closes it again', async () => {
    renderWithClient(<CommandTable />);
    const row = (await screen.findByText('Add login')).closest('article')!;
    fireEvent.click(within(row).getByRole('button', { name: 'Merge' }));
    const dialog = screen.getByRole('dialog', { name: 'Merge acme/web #1?' });
    expect(within(dialog).getByText('Add login')).toBeInTheDocument();
    fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }));
    expect(screen.queryByRole('dialog', { name: 'Merge acme/web #1?' })).toBeNull();
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

  it('opens the status sync rules dialog from the sidebar and gives the focus back on close', async () => {
    renderWithClient(<CommandTable />);
    await screen.findByText('Add login');
    fireEvent.click(screen.getByRole('button', { name: 'Settings sidebar' }));
    const button = await screen.findByRole('button', { name: 'Status sync rules' });
    expect(screen.queryByRole('dialog', { name: 'Status sync rules' })).toBeNull();
    button.focus();
    fireEvent.click(button);
    const dialog = await screen.findByRole('dialog', { name: 'Status sync rules' });
    expect(await within(dialog).findByText('No rules yet.')).toBeInTheDocument();
    fireEvent.click(within(dialog).getByRole('button', { name: 'Close' }));
    expect(screen.queryByRole('dialog', { name: 'Status sync rules' })).toBeNull();
    expect(screen.getByRole('dialog', { name: 'Settings' })).toBeInTheDocument();
    expect(button).toHaveFocus();
  });

  it('opens the Accounts dialog from Manage accounts in the sidebar', async () => {
    renderWithClient(<CommandTable />);
    await screen.findByText('Add login');
    expect(screen.queryByRole('button', { name: 'Account' })).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Settings sidebar' }));
    const manage = await screen.findByRole('button', { name: 'Manage accounts' });
    manage.focus();
    fireEvent.click(manage);
    const dialog = await screen.findByRole('dialog', { name: 'Accounts' });
    expect(await within(dialog).findByRole('region', { name: 'GitHub' })).toBeInTheDocument();
    expect(within(dialog).getByRole('region', { name: 'Jira' })).toBeInTheDocument();
    fireEvent.click(within(dialog).getByRole('button', { name: 'Close' }));
    expect(screen.queryByRole('dialog', { name: 'Accounts' })).toBeNull();
    expect(screen.getByRole('dialog', { name: 'Settings' })).toBeInTheDocument();
    expect(manage).toHaveFocus();

    fireEvent.click(manage);
    fireEvent.click(await screen.findByTestId('modal-scrim'));
    expect(screen.queryByRole('dialog', { name: 'Accounts' })).toBeNull();
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

  it('shows the GitHub API usage of the last refresh in the accounts eyebrow row', async () => {
    api.getSnapshot.mockResolvedValue(
      snapshot(rows(), {
        sources: {
          github: {
            state: 'ok',
            syncedAt: '2026-11-24T11:59:00Z',
            error: null,
            resetAt: null,
            rateLimit: { used: 4180, limit: 5000, resetAt: '2026-11-24T13:00:00Z' },
          },
          jira: { state: 'ok', syncedAt: '2026-11-24T11:59:00Z', error: null, resetAt: null },
          blockedOrgs: [],
        },
      }),
    );
    renderWithClient(<CommandTable />);
    await screen.findByText('Add login');
    fireEvent.click(screen.getByRole('button', { name: 'Settings sidebar' }));
    const accounts = await screen.findByRole('region', { name: 'Accounts' });
    const usage = await within(accounts).findByText('API 4180/5000');
    expect(usage).toHaveAttribute('aria-hidden', 'true');
    expect(within(accounts).getByText('GitHub API usage 4180 of 5000')).toBeInTheDocument();
    expect(within(accounts).getByText('Accounts').parentElement).toContainElement(usage);
  });

  it('shows no API usage while the snapshot has no rate limit', async () => {
    renderWithClient(<CommandTable />);
    await screen.findByText('Add login');
    fireEvent.click(screen.getByRole('button', { name: 'Settings sidebar' }));
    const accounts = await screen.findByRole('region', { name: 'Accounts' });
    await within(accounts).findByRole('button', { name: 'Manage accounts' });
    expect(within(accounts).queryByText(/^API /)).toBeNull();
    expect(within(accounts).queryByText(/GitHub API usage/)).toBeNull();
  });

  it('has no Sync or Account button in the header', async () => {
    renderWithClient(<CommandTable />);
    await screen.findByText('Add login');
    expect(screen.queryByRole('button', { name: 'Sync' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Account' })).toBeNull();
    expect(screen.getByRole('button', { name: 'Refresh' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /^Theme:/ })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'History' })).toHaveAttribute(
      'aria-pressed',
      'false',
    );
  });

  it('swaps the list for the History view and back, and the header search filters it', async () => {
    renderWithClient(<CommandTable />);
    await screen.findByText('Add login');
    const button = screen.getByRole('button', { name: 'History' });
    fireEvent.click(button);
    expect(button).toHaveAttribute('aria-pressed', 'true');
    const view = screen.getByRole('region', { name: 'Merged pull requests' });
    expect(within(view).getByText(/Nothing merged here yet/)).toBeInTheDocument();
    expect(screen.queryByText('Add login')).toBeNull();
    expect(screen.queryByRole('group', { name: 'Summary' })).toBeNull();
    expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('PRDashboards · History');
    fireEvent.click(button);
    expect(button).toHaveAttribute('aria-pressed', 'false');
    expect(screen.getByText('Add login')).toBeInTheDocument();
    expect(screen.queryByRole('region', { name: 'Merged pull requests' })).toBeNull();
  });

  it('tints the whole page while the History view is open (data-view on <html>)', async () => {
    const { unmount } = renderWithClient(<CommandTable />);
    await screen.findByText('Add login');
    const root = document.documentElement;
    expect(root.dataset.view).toBeUndefined();
    const button = screen.getByRole('button', { name: 'History' });
    fireEvent.click(button);
    expect(root.dataset.view).toBe('history');
    fireEvent.click(button);
    expect(root.dataset.view).toBeUndefined();
    fireEvent.click(button);
    expect(root.dataset.view).toBe('history');
    unmount();
    expect(root.dataset.view).toBeUndefined();
  });

  it('leaves out the pull requests of a repository switched off in the sidebar', async () => {
    localStorage.setItem('prd-repos-off', JSON.stringify(['R1']));
    renderWithClient(<CommandTable />);
    expect(await screen.findByText('Nothing here')).toBeInTheDocument();
    expect(document.querySelectorAll('article[data-pr]')).toHaveLength(0);
    localStorage.removeItem('prd-repos-off');
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
