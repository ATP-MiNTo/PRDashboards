import { act, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { SnapshotPr } from '@/api/client';
import { pr, snapshot, ticket } from '@/test-fixtures';
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
import { GHOST_MS } from './List';

const settings = {
  polling: { githubSec: 60, jiraSec: 300 },
  theme: 'auto',
  defaultMethodForNewRepos: 'squash',
  deleteBranchAfterMerge: false,
  jiraSiteId: null,
  excludedStatuses: ['Done'],
};

const day = (d: number) => `2026-11-${String(d).padStart(2, '0')}T10:00:00Z`;

function many(n: number): SnapshotPr[] {
  return Array.from({ length: n }, (_, i) =>
    pr(`p${i + 1}`, i + 1, {
      title: `Item ${String(i + 1).padStart(2, '0')}`,
      updatedAt: day(1 + (i % 28)),
    }),
  );
}

const rowOrder = () =>
  [...document.querySelectorAll('article[data-pr]')].map((r) => r.getAttribute('data-pr'));

function load(prs: SnapshotPr[]) {
  api.getSnapshot.mockResolvedValue(snapshot(prs));
}

beforeEach(() => {
  Object.values(api).forEach((f) => f.mockReset());
  localStorage.clear();
  api.getSettings.mockResolvedValue(settings);
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
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('sorting', () => {
  const prs = () => [
    pr('a', 1, { title: 'Mid', updatedAt: day(20), additions: 10, deletions: 0 }),
    pr('b', 2, { title: 'Zed', updatedAt: day(10), additions: 100, deletions: 20 }),
    pr('c', 3, { title: 'Add', updatedAt: day(22), additions: 1, deletions: 1 }),
  ];

  it('starts with the most recently updated first', async () => {
    load(prs());
    renderWithClient(<CommandTable />);
    await screen.findByText('Zed');
    expect(rowOrder()).toEqual(['c', 'a', 'b']);
  });

  it('sorts by the chosen option and reverses with the arrow button', async () => {
    load(prs());
    renderWithClient(<CommandTable />);
    await screen.findByText('Zed');
    fireEvent.change(screen.getByRole('combobox', { name: 'Sort' }), {
      target: { value: 'title' },
    });
    expect(rowOrder()).toEqual(['c', 'a', 'b']);
    const reverse = screen.getByRole('button', { name: 'Reverse sort order' });
    expect(reverse).toHaveAttribute('aria-pressed', 'false');
    fireEvent.click(reverse);
    expect(rowOrder()).toEqual(['b', 'a', 'c']);
    expect(reverse).toHaveAttribute('aria-pressed', 'true');
    fireEvent.change(screen.getByRole('combobox', { name: 'Sort' }), {
      target: { value: 'size' },
    });
    // Still reversed: the smallest change first.
    expect(rowOrder()).toEqual(['c', 'a', 'b']);
  });

  it('keeps a stack together and in order', async () => {
    load([
      pr('x', 9, { title: 'Single', updatedAt: day(15) }),
      pr('s1', 1, {
        title: 'Base',
        updatedAt: day(5),
        stack: { id: 's1', position: 1, size: 2, parentId: null, parentNumber: null },
      }),
      pr('s2', 2, {
        title: 'Top',
        updatedAt: day(25),
        stack: { id: 's1', position: 2, size: 2, parentId: 's1', parentNumber: 1 },
      }),
    ]);
    renderWithClient(<CommandTable />);
    await screen.findByText('Single');
    expect(rowOrder()).toEqual(['s1', 's2', 'x']);
  });
});

const mixed = () => [
  pr('a1', 1, { title: 'Assigned one', roles: ['assigned'] }),
  pr('a2', 2, { title: 'Assigned two', roles: ['assigned'] }),
  pr('o1', 3, { title: 'Opened one', roles: ['authored'] }),
  pr('b1', 4, { title: 'Both one', roles: ['assigned', 'authored'] }),
];

describe('show switch', () => {
  const sw = () => screen.getByRole('button', { name: /^Show, now/ });

  it('sits before the Hidden PRs switch and cycles Everything, Assigned to me, Opened by me', async () => {
    load(mixed());
    renderWithClient(<CommandTable />);
    await screen.findByText('Assigned one');
    const hidden = screen.getByRole('button', { name: /^Hidden PRs, now/ });
    expect(sw().compareDocumentPosition(hidden) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(sw()).toHaveAccessibleName(/now Everything, 4/);
    expect(rowOrder()).toEqual(['a1', 'a2', 'o1', 'b1']);

    fireEvent.click(sw());
    expect(sw()).toHaveAccessibleName(/now Assigned to me, 3/);
    expect(rowOrder()).toEqual(['a1', 'a2', 'b1']);
    expect(localStorage.getItem('prd-show')).toBe('assigned');

    fireEvent.click(sw());
    expect(sw()).toHaveAccessibleName(/now Opened by me, 2/);
    expect(rowOrder()).toEqual(['o1', 'b1']);
    expect(localStorage.getItem('prd-show')).toBe('authored');

    fireEvent.click(sw());
    expect(sw()).toHaveAccessibleName(/now Everything, 4/);
  });

  it('counts the whole snapshot, not the status filter or the hidden pull requests', async () => {
    localStorage.setItem('prd-hidden', JSON.stringify(['a1']));
    load([...mixed(), pr('d1', 5, { isDraft: true, roles: ['authored'] })]);
    renderWithClient(<CommandTable />);
    await screen.findByText('Assigned two');
    fireEvent.click(screen.getByRole('button', { name: /Ready to merge/ }));
    expect(sw()).toHaveAccessibleName(/now Everything, 5/);
    fireEvent.click(sw());
    fireEvent.click(sw());
    expect(sw()).toHaveAccessibleName(/now Opened by me, 3/);
  });

  it('restores the saved state and ignores a bad one', async () => {
    localStorage.setItem('prd-show', 'authored');
    load(mixed());
    const first = renderWithClient(<CommandTable />);
    await screen.findByText('Opened one');
    expect(sw()).toHaveAccessibleName(/now Opened by me/);
    expect(rowOrder()).toEqual(['o1', 'b1']);
    first.unmount();

    localStorage.setItem('prd-show', 'nope');
    renderWithClient(<CommandTable />);
    await screen.findByText('Opened one');
    expect(sw()).toHaveAccessibleName(/now Everything/);
  });

  it('lists a pull request without roles under every state', async () => {
    const old = { ...pr('old', 9, { title: 'Older snapshot' }), roles: undefined };
    load([old as unknown as SnapshotPr]);
    renderWithClient(<CommandTable />);
    await screen.findByText('Older snapshot');
    fireEvent.click(sw());
    expect(rowOrder()).toEqual(['old']);
    fireEvent.click(sw());
    expect(rowOrder()).toEqual(['old']);
  });

  it('goes back to the first page when it changes', async () => {
    load(many(25));
    renderWithClient(<CommandTable />);
    await screen.findByText('Item 25');
    fireEvent.click(screen.getByRole('button', { name: 'Page 2' }));
    fireEvent.click(sw());
    expect(screen.getByRole('button', { name: 'Page 1' })).toHaveAttribute('aria-current', 'page');
  });

  it('has a hint on the label that opens on keyboard focus and explains each state', async () => {
    load(mixed());
    renderWithClient(<CommandTable />);
    await screen.findByText('Assigned one');
    const label = screen.getByText('Show', { selector: 'span[tabindex]' });
    act(() => label.focus());
    const tip = screen.getByRole('tooltip');
    expect(tip).toHaveTextContent(/Everything.*Assigned to me.*Opened by me/);
    expect(label).toHaveAttribute('aria-describedby', tip.id);
  });
});

describe('role cues', () => {
  it('gives each card the class of its role, and none of them a style attribute', async () => {
    load(mixed());
    renderWithClient(<CommandTable />);
    await screen.findByText('Assigned one');
    const cls = (id: string) => document.querySelector(`article[data-pr="${id}"]`)!.className;
    expect(cls('a1')).toMatch(/role_assigned/);
    expect(cls('o1')).toMatch(/role_authored/);
    expect(cls('b1')).toMatch(/role_both/);
    expect(cls('o1')).not.toMatch(/role_assigned|role_both/);
    expect(document.querySelector('article [style]')).toBeNull();
  });

  it('shows a legend with Assigned, Opened by me and Stack at the end of the toolbar', async () => {
    load(mixed());
    renderWithClient(<CommandTable />);
    await screen.findByText('Assigned one');
    const legend = screen.getByRole('list', { name: 'Legend' });
    expect(
      within(legend)
        .getAllByRole('listitem')
        .map((li) => li.textContent),
    ).toEqual(['Assigned', 'Opened by me', 'Stack']);
    expect(legend.parentElement!.lastElementChild).toBe(legend);
    expect(legend.querySelector('[style]')).toBeNull();
  });
});

describe('hidden pull requests', () => {
  it('has a Hide checkbox on each row, named after the pull request', async () => {
    load(many(3));
    renderWithClient(<CommandTable />);
    await screen.findByText('Item 01');
    expect(screen.getAllByRole('checkbox', { name: /^Hide #/ })).toHaveLength(3);
    expect(screen.getByRole('checkbox', { name: 'Hide #2' })).not.toBeChecked();
  });

  it('cycles Visible, All, Hidden with a count, and saves the mode', async () => {
    localStorage.setItem('prd-hidden', JSON.stringify(['p2']));
    load(many(3));
    renderWithClient(<CommandTable />);
    await screen.findByText('Item 01');
    const sw = () => screen.getByRole('button', { name: /^Hidden PRs, now/ });
    expect(sw()).toHaveAccessibleName(/now Visible, 2/);
    expect(rowOrder()).not.toContain('p2');

    fireEvent.click(sw());
    expect(sw()).toHaveAccessibleName(/now All, 3/);
    expect(rowOrder()).toContain('p2');
    expect(document.querySelector('article[data-pr="p2"]')!.className).toMatch(/dim/);
    expect(document.querySelector('article[data-pr="p1"]')!.className).not.toMatch(/dim/);
    expect(localStorage.getItem('prd-hidef')).toBe('all');

    fireEvent.click(sw());
    expect(sw()).toHaveAccessibleName(/now Hidden, 1/);
    expect(rowOrder()).toEqual(['p2']);
    expect(localStorage.getItem('prd-hidef')).toBe('hidden');
  });

  it('restores the saved mode', async () => {
    localStorage.setItem('prd-hidef', 'hidden');
    localStorage.setItem('prd-hidden', JSON.stringify(['p3']));
    load(many(3));
    renderWithClient(<CommandTable />);
    await screen.findByText('Item 03');
    expect(rowOrder()).toEqual(['p3']);
  });

  it('ignores a bad saved mode and bad saved ids', async () => {
    localStorage.setItem('prd-hidef', 'nope');
    localStorage.setItem('prd-hidden', '{not json');
    load(many(2));
    renderWithClient(<CommandTable />);
    await screen.findByText('Item 01');
    expect(screen.getByRole('button', { name: /^Hidden PRs, now Visible, 2/ })).toBeInTheDocument();
  });

  it('prunes ids that left the snapshot without writing on render, and on the next change', async () => {
    const saved = JSON.stringify(['p1', 'gone']);
    localStorage.setItem('prd-hidden', saved);
    load(many(3));
    renderWithClient(<CommandTable />);
    await screen.findByText('Item 02');
    expect(screen.getByRole('button', { name: /now Visible, 2/ })).toBeInTheDocument();
    expect(localStorage.getItem('prd-hidden')).toBe(saved);
    fireEvent.click(screen.getByRole('button', { name: /^Hidden PRs, now/ }));
    fireEvent.click(screen.getByRole('checkbox', { name: 'Hide #3' }));
    expect(JSON.parse(localStorage.getItem('prd-hidden')!)).toEqual(['p1', 'p3']);
  });

  it('hides and shows a row and saves the set', async () => {
    load(many(3));
    renderWithClient(<CommandTable />);
    await screen.findByText('Item 01');
    fireEvent.click(screen.getByRole('button', { name: /^Hidden PRs, now/ }));
    const box = screen.getByRole('checkbox', { name: 'Hide #2' });
    fireEvent.click(box);
    expect(box).toBeChecked();
    expect(JSON.parse(localStorage.getItem('prd-hidden')!)).toEqual(['p2']);
    fireEvent.click(box);
    expect(box).not.toBeChecked();
    expect(JSON.parse(localStorage.getItem('prd-hidden')!)).toEqual([]);
  });

  describe('fade out', () => {
    afterEach(() => vi.useRealTimers());

    it('keeps the row, fading, until the animation ends, then removes it', async () => {
      load(many(2));
      renderWithClient(<CommandTable />);
      await screen.findByText('Item 01');
      vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] });
      fireEvent.click(screen.getByRole('checkbox', { name: 'Hide #1' }));
      const row = document.querySelector('article[data-pr="p1"]')!;
      expect(row.className).toMatch(/ghost/);
      expect(screen.getByRole('checkbox', { name: 'Hide #1' })).toBeChecked();
      expect(localStorage.getItem('prd-hidden')).toBeNull();
      act(() => void vi.advanceTimersByTime(GHOST_MS + 10));
      expect(document.querySelector('article[data-pr="p1"]')).toBeNull();
      expect(JSON.parse(localStorage.getItem('prd-hidden')!)).toEqual(['p1']);
    });

    it('applies two quick hides without losing the first', async () => {
      load(many(3));
      renderWithClient(<CommandTable />);
      await screen.findByText('Item 01');
      vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] });
      fireEvent.click(screen.getByRole('checkbox', { name: 'Hide #1' }));
      fireEvent.click(screen.getByRole('checkbox', { name: 'Hide #2' }));
      act(() => void vi.advanceTimersByTime(GHOST_MS + 10));
      expect(rowOrder()).toEqual(['p3']);
      expect(JSON.parse(localStorage.getItem('prd-hidden')!).sort()).toEqual(['p1', 'p2']);
    });
  });

  it('opens the hint on keyboard focus and closes it with Escape', async () => {
    load(many(1));
    renderWithClient(<CommandTable />);
    await screen.findByText('Item 01');
    const label = screen.getByText('Hidden PRs', { selector: 'span[tabindex]' });
    fireEvent.focus(label);
    const tip = screen.getByRole('tooltip');
    expect(tip).toHaveTextContent('Visible');
    expect(tip).toHaveTextContent('All');
    expect(tip).toHaveTextContent('Hidden');
    expect(label).toHaveAttribute('aria-describedby', tip.id);
    fireEvent.keyDown(label, { key: 'Escape' });
    expect(screen.queryByRole('tooltip')).toBeNull();
  });
});

describe('search', () => {
  const search = () => screen.getByPlaceholderText('Title, repo, PR or Jira key');

  it('filters by title, repository, PR number and Jira key, and says when nothing matches', async () => {
    load([
      pr('a', 11, { title: 'Login form', tickets: [ticket('INP-7')] }),
      pr('b', 12, {
        title: 'Billing page',
        repo: {
          id: 'R2',
          nameWithOwner: 'acme/billing-api',
          isNew: false,
          squashAllowed: true,
          mergeAllowed: true,
          rebaseAllowed: true,
        },
      }),
      pr('c', 13, { title: 'Export CSV' }),
    ]);
    renderWithClient(<CommandTable />);
    await screen.findByText('Billing page');
    const type = (value: string) => fireEvent.change(search(), { target: { value } });

    type('login');
    expect(rowOrder()).toEqual(['a']);
    type('BILLING-API');
    expect(rowOrder()).toEqual(['b']);
    type('#13');
    expect(rowOrder()).toEqual(['c']);
    type('inp-7');
    expect(rowOrder()).toEqual(['a']);
    type('nothing like this');
    expect(rowOrder()).toEqual([]);
    expect(screen.getByText('Nothing here')).toBeInTheDocument();
    expect(
      screen.getByText('No open pull requests match these switches and filters.'),
    ).toBeInTheDocument();
    type('');
    expect(rowOrder()).toEqual(['a', 'b', 'c']);
  });

  it('shares the query with the field of narrow screens', async () => {
    load(many(3));
    renderWithClient(<CommandTable />);
    await screen.findByText('Item 03');
    fireEvent.change(screen.getByPlaceholderText('Search pull requests'), {
      target: { value: 'item 02' },
    });
    expect(rowOrder()).toEqual(['p2']);
    expect(search()).toHaveValue('item 02');
  });

  it('keeps the counts of the stat strip and goes back to the first page', async () => {
    load(many(25));
    renderWithClient(<CommandTable />);
    await screen.findByText('Item 25');
    fireEvent.click(screen.getByRole('button', { name: 'Page 3' }));
    fireEvent.change(search(), { target: { value: 'item 1' } });
    expect(screen.getByRole('button', { name: 'Page 1' })).toHaveAttribute('aria-current', 'page');
    expect(screen.getByText('Showing 1 to 10 of 12')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /^Showing/ })).toHaveTextContent('Showing12');
  });
});

describe('paging', () => {
  it('shows 10 per page by default with the range, and moves between pages', async () => {
    load(many(25));
    renderWithClient(<CommandTable />);
    await screen.findByText('Item 25');
    expect(rowOrder()).toHaveLength(10);
    expect(screen.getByText('Showing 1 to 10 of 25')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Next page' }));
    expect(screen.getByText('Showing 11 to 20 of 25')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Page 2' })).toHaveAttribute('aria-current', 'page');
    fireEvent.click(screen.getByRole('button', { name: 'Page 3' }));
    expect(rowOrder()).toHaveLength(5);
  });

  it('changes and saves the page size, and goes back to the first page', async () => {
    load(many(25));
    renderWithClient(<CommandTable />);
    await screen.findByText('Item 25');
    fireEvent.click(screen.getByRole('button', { name: 'Page 3' }));
    fireEvent.change(screen.getByRole('combobox', { name: 'Pull requests per page' }), {
      target: { value: '20' },
    });
    expect(localStorage.getItem('prd-pagesize')).toBe('20');
    expect(rowOrder()).toHaveLength(20);
    expect(screen.getByText('Showing 1 to 20 of 25')).toBeInTheDocument();
  });

  it('restores the saved page size', async () => {
    localStorage.setItem('prd-pagesize', '15');
    load(many(25));
    renderWithClient(<CommandTable />);
    await screen.findByText('Item 25');
    expect(rowOrder()).toHaveLength(15);
    expect(screen.getByRole('combobox', { name: 'Pull requests per page' })).toHaveValue('15');
  });

  it('goes back to the first page when the sort, the filter or the hidden mode changes', async () => {
    load(many(25));
    renderWithClient(<CommandTable />);
    await screen.findByText('Item 25');
    const second = () => fireEvent.click(screen.getByRole('button', { name: 'Page 2' }));
    const onFirst = () =>
      expect(screen.getByRole('button', { name: 'Page 1' })).toHaveAttribute(
        'aria-current',
        'page',
      );

    second();
    fireEvent.click(screen.getByRole('button', { name: 'Reverse sort order' }));
    onFirst();
    second();
    fireEvent.change(screen.getByRole('combobox', { name: 'Sort' }), {
      target: { value: 'title' },
    });
    onFirst();
    second();
    fireEvent.click(screen.getByRole('button', { name: /Ready to merge/ }));
    onFirst();
    second();
    fireEvent.click(screen.getByRole('button', { name: /^Hidden PRs, now/ }));
    onFirst();
  });

  it('never splits a stack across pages', async () => {
    const prs = [
      ...many(8).map((p) => ({ ...p, updatedAt: day(28) })),
      ...[1, 2, 3, 4].map((n) =>
        pr(`s${n}`, 100 + n, {
          title: `Stacked ${n}`,
          updatedAt: day(1),
          stack: {
            id: 's1',
            position: n,
            size: 4,
            parentId: n === 1 ? null : `s${n - 1}`,
            parentNumber: n === 1 ? null : 99 + n,
          },
        }),
      ),
    ];
    load(prs);
    renderWithClient(<CommandTable />);
    await screen.findByText('Item 01');
    // The eight singles and the stack of four: the page runs over to keep the stack whole.
    expect(rowOrder()).toHaveLength(12);
    expect(screen.getByText('Showing 1 to 12 of 12')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Next page' })).toBeNull();
  });

  it('keeps the size select when there is one page, and nothing without pull requests', async () => {
    load(many(3));
    const { unmount } = renderWithClient(<CommandTable />);
    await screen.findByText('Item 03');
    expect(screen.getByRole('combobox', { name: 'Pull requests per page' })).toBeInTheDocument();
    expect(screen.queryByRole('navigation', { name: 'Pages' })).toBeNull();
    unmount();

    load([]);
    renderWithClient(<CommandTable />);
    await waitFor(() => expect(screen.getByText('Nothing here')).toBeInTheDocument());
    expect(screen.queryByRole('combobox', { name: 'Pull requests per page' })).toBeNull();
  });
});

describe('footer', () => {
  it('has the credit and the GitHub link', async () => {
    load(many(1));
    renderWithClient(<CommandTable />);
    await screen.findByText('Item 01');
    const footer = screen.getByRole('contentinfo');
    expect(footer).toHaveTextContent('by ATP-MiNTo');
    const link = within(document.body).getByRole('link', { name: 'GitHub repository' });
    expect(link).toHaveAttribute('href', 'https://github.com/ATP-MiNTo/PRDashboards');
  });
});
