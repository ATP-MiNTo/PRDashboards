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
  approvalsRequired: 1,
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
});
