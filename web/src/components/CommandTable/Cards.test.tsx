import { act, fireEvent, screen, within } from '@testing-library/react';
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
import { GHOST_MS, PULSE_MS } from './List';

const settings = {
  polling: { githubSec: 60, jiraSec: 300 },
  theme: 'auto',
  defaultMethodForNewRepos: 'squash',
  deleteBranchAfterMerge: false,
  multiPrTickets: 'all',
  jiraSiteId: null,
  excludedStatuses: ['Done'],
};

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
  vi.useFakeTimers({ toFake: ['Date'] });
  vi.setSystemTime(new Date('2026-11-24T12:00:00Z'));
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

const card = (id: string) => document.querySelector<HTMLElement>(`article[data-pr="${id}"]`)!;

async function show(prs: SnapshotPr[]) {
  api.getSnapshot.mockResolvedValue(snapshot(prs));
  renderWithClient(<CommandTable />);
  await screen.findByRole('heading', { level: 2, name: prs[0]!.title });
}

describe('card', () => {
  it('has an avatar with two letters, the title as a link, and the meta line', async () => {
    await show([pr('a', 7, { title: 'Add login', author: 'priya' })]);
    const c = card('a');
    expect(within(c).getByText('PR')).toHaveAttribute('aria-hidden', 'true');
    expect(within(c).getByRole('heading', { level: 2 })).toHaveTextContent('Add login');
    expect(within(c).getByRole('link', { name: 'Add login' })).toHaveAttribute(
      'href',
      'https://github.com/acme/web/pull/7',
    );
    const meta = c.querySelector('[class*="meta"]')!;
    expect(meta.textContent).toContain('acme/web #7');
    expect(meta.textContent).toContain('· priya');
  });

  it('says when it was updated and when the last commit came, in short ages', async () => {
    await show([
      pr('a', 1, { updatedAt: '2026-11-24T11:30:00Z', committedAt: '2026-11-24T10:00:00Z' }),
    ]);
    expect(within(card('a')).getByTitle('PR updated 30m ago')).toHaveTextContent('30m');
    expect(within(card('a')).getByTitle('Latest commit 2h ago')).toHaveTextContent('2h');
  });

  it('leaves out what the snapshot does not know', async () => {
    await show([
      pr('a', 1, {
        author: null,
        updatedAt: null,
        committedAt: null,
        additions: null,
        deletions: null,
      }),
    ]);
    const c = card('a');
    expect(within(c).getByText('?')).toBeInTheDocument();
    expect(c.textContent).not.toContain('·');
    expect(c.querySelector('[title*="lines added"]')).toBeNull();
    expect(c.querySelector('[title^="PR updated"]')).toBeNull();
    expect(c.querySelector('[title^="Latest commit"]')).toBeNull();
  });

  it('shows the lines added and removed', async () => {
    await show([pr('a', 1, { additions: 120, deletions: 4 })]);
    const diff = within(card('a')).getByTitle('120 lines added, 4 removed');
    expect(diff).toHaveTextContent('+120 −4');
  });

  it('keeps the Draft, New repo and Last data cues', async () => {
    const base = pr('a', 1, { isDraft: true });
    await show([{ ...base, repo: { ...base.repo, isNew: true } }]);
    const c = card('a');
    expect(within(c).getByText('Draft', { selector: 'span' })).toBeInTheDocument();
    expect(within(c).getByText('New repo')).toBeInTheDocument();
  });

  it('puts its Jira tickets on the card', async () => {
    await show([pr('a', 1, { tickets: [ticket('INP-7')] })]);
    expect(within(card('a')).getByRole('link', { name: 'INP-7 · In Review' })).toBeInTheDocument();
  });

  it('has no style attribute, anywhere in the list', async () => {
    await show([pr('a', 1), pr('b', 2, { roles: ['reviewer'] })]);
    expect(document.querySelector('main [style]')).toBeNull();
  });
});

describe('stack group', () => {
  const member = (id: string, n: number, size = 3) =>
    pr(id, n, {
      title: `Step ${n}`,
      tickets: n === 1 ? [ticket('INP-1')] : [],
      stack: {
        id: 'S',
        position: n,
        size,
        parentId: n === 1 ? null : 's' + (n - 1),
        parentNumber: n === 1 ? null : n - 1,
      },
    });

  it('says how many pull requests there are and numbers each step', async () => {
    await show([member('s1', 1), member('s2', 2), member('s3', 3)]);
    const group = screen.getByRole('region', { name: 'Stack' });
    expect(within(group).getByText('3 PRs, merge in order')).toBeInTheDocument();
    const banner = group.firstElementChild as HTMLElement;
    expect(banner.textContent).toContain('STACK');
    expect(within(banner).getByRole('link', { name: 'INP-1 · In Review' })).toBeInTheDocument();
    const steps = [...banner.querySelectorAll('[class*="stackStep_"]')].map((e) => e.textContent);
    expect(steps).toEqual(['1', '2', '3']);
  });

  it('says how many of the stack are shown when the list holds only some', async () => {
    await show([member('s1', 1), member('s2', 2), member('s3', 3)]);
    fireEvent.change(screen.getByPlaceholderText('Title, repo, PR or Jira key'), {
      target: { value: 'step 2' },
    });
    const group = screen.getByRole('region', { name: 'Stack' });
    expect(within(group).getByText('1 of 3 shown')).toBeInTheDocument();
  });

  it('has no banner for a pull request that is not in a stack', async () => {
    await show([pr('a', 1)]);
    expect(screen.queryByRole('region', { name: 'Stack' })).toBeNull();
  });
});

describe('hide animations', () => {
  const two = () => [pr('p1', 1, { title: 'One' }), pr('p2', 2, { title: 'Two' })];
  const cls = (id: string) => card(id).className;

  async function allMode() {
    localStorage.setItem('prd-hidef', 'all');
    localStorage.setItem('prd-hidden', JSON.stringify(['p2']));
    await show(two());
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'Date'] });
  }

  it('dims a card that is hidden while the All mode keeps it in the list', async () => {
    await allMode();
    expect(cls('p2')).toMatch(/dim/);
    fireEvent.click(screen.getByRole('checkbox', { name: 'Hide #1' }));
    expect(cls('p1')).toMatch(/ghostDim/);
    expect(cls('p1')).not.toMatch(/ghostOut/);
    act(() => void vi.advanceTimersByTime(PULSE_MS + 10));
    expect(cls('p1')).not.toMatch(/ghost/);
    expect(cls('p1')).toMatch(/dim/);
    expect(JSON.parse(localStorage.getItem('prd-hidden')!).sort()).toEqual(['p1', 'p2']);
  });

  it('brings a card back with one appear animation when it is shown again', async () => {
    await allMode();
    fireEvent.click(screen.getByRole('checkbox', { name: 'Hide #2' }));
    expect(cls('p2')).toMatch(/ghostIn/);
    act(() => void vi.advanceTimersByTime(PULSE_MS + 10));
    expect(cls('p2')).not.toMatch(/ghost|dim/);
  });

  it('fades a card out before the Visible mode removes it', async () => {
    await show(two());
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'Date'] });
    fireEvent.click(screen.getByRole('checkbox', { name: 'Hide #1' }));
    expect(cls('p1')).toMatch(/ghostOut/);
    act(() => void vi.advanceTimersByTime(GHOST_MS - 10));
    expect(card('p1')).not.toBeNull();
    act(() => void vi.advanceTimersByTime(20));
    expect(document.querySelector('article[data-pr="p1"]')).toBeNull();
  });

  it('plays nothing under reduced motion', async () => {
    vi.stubGlobal(
      'matchMedia',
      vi.fn().mockReturnValue({ matches: true, addEventListener() {}, removeEventListener() {} }),
    );
    await show(two());
    fireEvent.click(screen.getByRole('checkbox', { name: 'Hide #1' }));
    expect(document.querySelector('article[data-pr="p1"]')).toBeNull();
    expect(JSON.parse(localStorage.getItem('prd-hidden')!)).toEqual(['p1']);
  });
});

describe('list messages', () => {
  it('says "Nothing here" in its own surface when no pull request matches', async () => {
    await show([pr('a', 1, { title: 'Only one' })]);
    fireEvent.change(screen.getByPlaceholderText('Title, repo, PR or Jira key'), {
      target: { value: 'zzz' },
    });
    expect(screen.getByText('Nothing here')).toBeInTheDocument();
    expect(
      screen.getByText('No open pull requests match these switches and filters.'),
    ).toBeInTheDocument();
    expect(document.querySelector('article')).toBeNull();
  });

  it('says so, as an alert, when the dashboard cannot load', async () => {
    api.getSnapshot.mockRejectedValue(new Error('down'));
    renderWithClient(<CommandTable />);
    expect(await screen.findByRole('alert')).toHaveTextContent('Could not load the dashboard.');
  });
});
