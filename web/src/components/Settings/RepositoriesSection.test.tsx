import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { RepoSettings } from '@/api/client';
import { pr, snapshot } from '@/test-fixtures';
import { renderWithClient } from '@/test-utils';

const api = vi.hoisted(() => ({
  getRepos: vi.fn(),
  putRepo: vi.fn(),
  getSnapshot: vi.fn(),
  getSettings: vi.fn(),
  putSettings: vi.fn(),
  getAccounts: vi.fn(),
}));

vi.mock('@/api/client', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/api/client')>()),
  ...api,
}));

import { RepositoriesSection } from './RepositoriesSection';

const repo = (id: string, name: string, over: Partial<RepoSettings> = {}): RepoSettings => ({
  id,
  nameWithOwner: name,
  isNew: false,
  openPrs: 2,
  approvalsRequired: 0,
  defaultMethod: 'squash',
  inheritMethod: true,
  deleteBranch: 'default',
  branchRules: [
    { pattern: 'main', method: 'merge' },
    { pattern: 'production', method: 'merge' },
  ],
  ...over,
});

beforeEach(() => {
  api.getRepos.mockResolvedValue({
    repos: [repo('R1', 'acme/web'), repo('R2', 'acme/api', { approvalsRequired: 2 })],
  });
  api.putRepo.mockImplementation((_id: string, u: unknown) => Promise.resolve(u));
  api.getSnapshot.mockResolvedValue(snapshot([pr('a', 1)]));
  api.getSettings.mockResolvedValue({
    polling: { githubSec: 60, jiraSec: 300 },
    theme: 'auto',
    defaultMethodForNewRepos: 'squash',
    deleteBranchAfterMerge: false,
    multiPrTickets: 'all',
    jiraSiteId: null,
    excludedStatuses: ['Done'],
  });
  api.putSettings.mockImplementation((s: unknown) => Promise.resolve(s));
  api.getAccounts.mockResolvedValue({
    github: { connected: true, login: 'me', method: 'app', needsReconnect: false },
    jira: { connected: false, sites: [], selectedSiteId: null, needsReconnect: false },
  });
});

function renderSection(off: string[] = [], onOff = vi.fn()) {
  renderWithClient(<RepositoriesSection off={new Set(off)} onOff={onOff} />);
  return onOff;
}

const fold = () => screen.findByRole('button', { name: /^Repositories setting/ });

describe('RepositoriesSection', () => {
  it('is folded by default and shows how many repositories the list shows', async () => {
    renderSection(['R2']);
    const head = await fold();
    expect(head).toHaveAttribute('aria-expanded', 'false');
    expect(await screen.findByText('1/2')).toBeInTheDocument();
    expect(screen.queryByRole('list', { name: 'Repositories' })).toBeNull();
    fireEvent.click(head);
    expect(head).toHaveAttribute('aria-expanded', 'true');
    expect(await screen.findByRole('list', { name: 'Repositories' })).toBeInTheDocument();
    fireEvent.click(head);
    expect(screen.queryByRole('list', { name: 'Repositories' })).toBeNull();
  });

  it('lists a checkbox per repository with the VIA badge, and a gear that is blue when it is custom', async () => {
    renderSection();
    fireEvent.click(await fold());
    const web = await screen.findByRole('checkbox', { name: 'Show acme/web' });
    expect(web).toBeChecked();
    expect(screen.getAllByText('GitHub App')).toHaveLength(2);
    expect(screen.getByRole('button', { name: 'Settings for acme/web' })).toHaveAttribute(
      'title',
      'Uses defaults',
    );
    // Two approvals make acme/api custom.
    expect(screen.getByRole('button', { name: 'Settings for acme/api' })).toHaveAttribute(
      'title',
      'Custom settings',
    );
  });

  it('keeps the badge and the gear of every row with 25 very long names, each name in a one-line cut box', async () => {
    const long =
      'a-very-long-organization-name/some-extremely-long-repository-name-for-the-platform';
    api.getRepos.mockResolvedValue({
      repos: Array.from({ length: 25 }, (_, i) => repo(`R${i}`, `${long}-${i}`)),
    });
    renderSection();
    fireEvent.click(await fold());
    const list = await screen.findByRole('list', { name: 'Repositories' });
    const rows = within(list).getAllByRole('listitem');
    expect(rows).toHaveLength(25);
    for (const row of rows) {
      expect(within(row).getByText('GitHub App')).toBeInTheDocument();
      expect(within(row).getByRole('button', { name: /^Settings for / })).toBeInTheDocument();
    }
    expect(screen.getByRole('button', { name: 'Select all' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Clear' })).toBeInTheDocument();
    // The name is the Marquee box (a block that is cut with an ellipsis), the one thing in a row that may shrink.
    const name = within(rows[0] as HTMLElement).getByText(`${long}-0`);
    expect(name.className).toMatch(/mq/);
  });

  // jsdom has no layout, so the cause of the 2.4.0 cropping is pinned in the stylesheet itself: an
  // `auto` grid track is as wide as the longest unbreakable line (a repository name) and pushed the
  // content out of the sidebar's hidden overflow. The real measure is e2e/sidebar.spec.ts.
  it('keeps the sidebar content in one shrinkable column', () => {
    const css = readFileSync(
      join(process.cwd(), 'src/components/Settings/Settings.module.css'),
      'utf8',
    );
    for (const selector of ['.body', '.section']) {
      const block = new RegExp(`${selector.replace('.', '\\.')} \\{[^}]*\\}`).exec(css)?.[0] ?? '';
      expect(block, selector).toContain('grid-template-columns: minmax(0, 1fr)');
    }
  });

  it('shows and hides repositories one by one, with Select all and Clear', async () => {
    const onOff = renderSection(['R2']);
    fireEvent.click(await fold());
    fireEvent.click(await screen.findByRole('checkbox', { name: 'Show acme/web' }));
    expect(onOff).toHaveBeenLastCalledWith(new Set(['R2', 'R1']));
    fireEvent.click(screen.getByRole('checkbox', { name: 'Show acme/api' }));
    expect(onOff).toHaveBeenLastCalledWith(new Set());
    fireEvent.click(screen.getByRole('button', { name: 'Select all' }));
    expect(onOff).toHaveBeenLastCalledWith(new Set());
    fireEvent.click(screen.getByRole('button', { name: 'Clear' }));
    expect(onOff).toHaveBeenLastCalledWith(new Set(['R1', 'R2']));
  });

  it('disables Select all when everything is shown and Clear when nothing is', async () => {
    renderSection();
    fireEvent.click(await fold());
    expect(await screen.findByRole('button', { name: 'Select all' })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Clear' })).toBeEnabled();
  });

  it('saves the default merge method and the default for deleting branches', async () => {
    renderSection();
    fireEvent.click(await fold());
    fireEvent.change(await screen.findByRole('combobox', { name: 'Default merge method' }), {
      target: { value: 'rebase' },
    });
    await waitFor(() => expect(api.putSettings).toHaveBeenCalledTimes(1));
    expect(api.putSettings.mock.calls[0]?.[0]).toMatchObject({
      defaultMethodForNewRepos: 'rebase',
    });
    fireEvent.click(screen.getByRole('button', { name: /^Delete branch after merge, default/ }));
    await waitFor(() => expect(api.putSettings).toHaveBeenCalledTimes(2));
    expect(api.putSettings.mock.calls[1]?.[0]).toMatchObject({ deleteBranchAfterMerge: true });
  });

  it('shows and saves the global setting for tickets shared by several PRs', async () => {
    renderSection();
    fireEvent.click(await fold());
    const select = await screen.findByRole('combobox', { name: 'Multi-PR tickets move when' });
    expect(select).toHaveValue('all');
    expect(
      within(select)
        .getAllByRole('option')
        .map((o) => o.textContent),
    ).toEqual(['All PRs are merged', 'First PR is merged']);
    fireEvent.change(select, { target: { value: 'any' } });
    await waitFor(() => expect(api.putSettings).toHaveBeenCalledTimes(1));
    expect(api.putSettings.mock.calls[0]?.[0]).toMatchObject({
      multiPrTickets: 'any',
      defaultMethodForNewRepos: 'squash',
      deleteBranchAfterMerge: false,
    });
  });

  it('shows the stored setting and says so when saving fails', async () => {
    api.getSettings.mockResolvedValue({
      polling: { githubSec: 60, jiraSec: 300 },
      theme: 'auto',
      defaultMethodForNewRepos: 'squash',
      deleteBranchAfterMerge: false,
      multiPrTickets: 'any',
      jiraSiteId: null,
      excludedStatuses: ['Done'],
    });
    api.putSettings.mockRejectedValue(new Error('no'));
    renderSection();
    fireEvent.click(await fold());
    const select = await screen.findByRole('combobox', { name: 'Multi-PR tickets move when' });
    expect(select).toHaveValue('any');
    fireEvent.change(select, { target: { value: 'all' } });
    expect(await screen.findByText('Could not save the settings.')).toBeInTheDocument();
  });

  it('opens the dialog of a repository from its gear', async () => {
    renderSection();
    fireEvent.click(await fold());
    fireEvent.click(await screen.findByRole('button', { name: 'Settings for acme/web' }));
    const dialog = await screen.findByRole('dialog', { name: 'acme/web' });
    expect(within(dialog).getByRole('button', { name: 'Done' })).toBeInTheDocument();
    fireEvent.click(within(dialog).getByRole('button', { name: 'Done' }));
    expect(screen.queryByRole('dialog')).toBeNull();
  });

  it('says so when the repositories cannot be loaded or there are none', async () => {
    api.getRepos.mockResolvedValue({ repos: [] });
    renderSection();
    fireEvent.click(await fold());
    expect(await screen.findByText('No repositories yet.')).toBeInTheDocument();
  });

  describe('with several tokens', () => {
    const tok = (id: string, name: string, over: Record<string, unknown> = {}) => ({
      id,
      name,
      method: 'pat',
      fineGrained: true,
      login: 'minto',
      warning: null,
      needsReconnect: false,
      ...over,
    });
    const withTokens = () => {
      api.getAccounts.mockResolvedValue({
        github: { connected: true, login: 'me', method: 'pat', needsReconnect: false },
        githubTokens: [
          tok('aaaaaaaa', 'Work'),
          tok('bbbbbbbb', 'Personal', { fineGrained: false }),
        ],
        githubTokenLimit: 10,
        jira: { connected: false, sites: [], selectedSiteId: null, needsReconnect: false },
      });
      const base = pr('a', 1).repo;
      api.getSnapshot.mockResolvedValue(
        snapshot([
          pr('a', 1, {
            repo: {
              ...base,
              id: 'R1',
              tokenId: 'aaaaaaaa',
              tokenName: 'Work',
              viewerPermission: 'WRITE',
            },
          }),
          pr('b', 2, {
            repo: {
              ...base,
              id: 'R2',
              nameWithOwner: 'acme/api',
              tokenId: 'bbbbbbbb',
              tokenName: 'Personal',
              viewerPermission: 'READ',
            },
          }),
        ]),
      );
    };

    it('names the token on each badge, so two tokens differ by text and not only by colour', async () => {
      withTokens();
      renderSection();
      fireEvent.click(await fold());
      const list = await screen.findByRole('list', { name: 'Repositories' });
      const [web, api2] = within(list).getAllByRole('listitem') as [HTMLElement, HTMLElement];
      expect(await within(web).findByText('Work')).toBeInTheDocument();
      expect(within(api2).getByText('Personal')).toBeInTheDocument();
      expect(within(web).queryByText('Token')).toBeNull();
    });

    it('explains the token in a hint a keyboard can reach, with the role', async () => {
      withTokens();
      renderSection();
      fireEvent.click(await fold());
      const tip = await screen.findByRole('button', { name: 'About the token of acme/web' });
      expect(tip).toHaveAccessibleDescription(
        'Fetched with Work (fine-grained token, minto). Your role here: Write. A fine-grained token works for one owner only.',
      );
      fireEvent.focus(tip);
      expect(document.body.textContent).toContain('Fetched with Work');
    });

    it('says in the hint that a read-only role cannot merge, and what to do', async () => {
      withTokens();
      renderSection();
      fireEvent.click(await fold());
      const tip = await screen.findByRole('button', { name: 'About the token of acme/api' });
      expect(tip).toHaveAccessibleDescription(
        "Fetched with Personal (classic token, minto). Your role here: Read. Read-only: Merge and Ready are not available here. Add a token from this repository's owner in Accounts.",
      );
    });

    it('keeps the generic label, and no hint, for a repository without a token in the snapshot', async () => {
      withTokens();
      api.getRepos.mockResolvedValue({
        repos: [repo('R1', 'acme/web'), repo('R7', 'acme/quiet')],
      });
      renderSection();
      fireEvent.click(await fold());
      const quiet = (await screen.findByRole('checkbox', { name: 'Show acme/quiet' })).closest(
        'li',
      ) as HTMLElement;
      expect(within(quiet).getByText('Token')).toBeInTheDocument();
      expect(within(quiet).queryByRole('button', { name: /About the token/ })).toBeNull();
    });
  });
});
