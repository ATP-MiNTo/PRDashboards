import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { renderWithClient } from '@/test-utils';

const api = vi.hoisted(() => ({
  getAccounts: vi.fn(),
  getSetup: vi.fn(),
  getRepos: vi.fn(),
  getSettings: vi.fn(),
  putSettings: vi.fn(),
  disconnectAccount: vi.fn(),
  checkAccount: vi.fn(),
  putGithubPat: vi.fn(),
  revokeAllSessions: vi.fn(),
}));

vi.mock('@/api/client', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/api/client')>()),
  ...api,
}));

import { AccountsModal } from './AccountsModal';

const github = (over: Record<string, unknown> = {}) => ({
  connected: true,
  login: 'minatpt',
  method: 'app',
  needsReconnect: false,
  warning: null,
  ...over,
});
const jira = (over: Record<string, unknown> = {}) => ({
  connected: true,
  needsReconnect: false,
  sites: [{ id: 's1', name: 'acme', url: 'https://acme.atlassian.net' }],
  selectedSiteId: 's1',
  ...over,
});

const settings = {
  polling: { githubSec: 60, jiraSec: 300 },
  theme: 'auto',
  defaultMethodForNewRepos: 'squash',
  deleteBranchAfterMerge: false,
  jiraSiteId: 's1',
  excludedStatuses: [],
};

beforeEach(() => {
  Object.values(api).forEach((f) => f.mockReset());
  api.getAccounts.mockResolvedValue({ github: github(), jira: jira() });
  api.getSetup.mockResolvedValue({
    github: { configured: true, callbackUrl: '' },
    atlassian: { configured: true, callbackUrl: '' },
  });
  api.getRepos.mockResolvedValue({
    repos: [
      { id: 'R1', nameWithOwner: 'acme/web' },
      { id: 'R2', nameWithOwner: 'acme/api' },
    ],
  });
  api.getSettings.mockResolvedValue(settings);
  api.putSettings.mockImplementation((s: unknown) => Promise.resolve(s));
  api.disconnectAccount.mockResolvedValue(undefined);
  api.checkAccount.mockResolvedValue({ ok: true });
  api.putGithubPat.mockResolvedValue({});
  api.revokeAllSessions.mockResolvedValue(undefined);
});

async function open(onClose = vi.fn()) {
  renderWithClient(<AccountsModal onClose={onClose} />);
  const dialog = await screen.findByRole('dialog', { name: 'Accounts' });
  await within(dialog).findByRole('region', { name: 'GitHub' });
  return { dialog, onClose };
}

describe('AccountsModal', () => {
  it('shows the GitHub App connection with Reconnect, Check and Disconnect and the repository table', async () => {
    const { dialog } = await open();
    const card = within(within(dialog).getByRole('region', { name: 'GitHub' }));
    expect(card.getByText('GitHub · minatpt')).toBeInTheDocument();
    expect(card.getByText('Connected')).toBeInTheDocument();
    expect(card.getByRole('link', { name: 'Reconnect GitHub' })).toHaveAttribute(
      'href',
      '/oauth/github/start',
    );
    expect(card.queryByRole('button', { name: 'Replace token' })).toBeNull();
    expect(card.getByRole('button', { name: 'Check GitHub' })).toBeInTheDocument();
    expect(card.getByRole('button', { name: 'Disconnect GitHub' })).toBeInTheDocument();
    const table = card.getByRole('table');
    expect(
      within(table)
        .getAllByRole('columnheader')
        .map((h) => h.textContent),
    ).toEqual(['Repository', 'Source', 'Why']);
    expect(within(table).getByText('acme/web')).toBeInTheDocument();
    expect(within(table).getAllByText('GitHub App')).toHaveLength(2);
  });

  it('replaces a token without ever showing the stored one', async () => {
    api.getAccounts.mockResolvedValue({ github: github({ method: 'pat' }), jira: jira() });
    const { dialog } = await open();
    expect(within(dialog).queryByRole('link', { name: 'Reconnect GitHub' })).toBeNull();
    expect(within(dialog).getByText('Classic token', { selector: 'b' })).toBeInTheDocument();
    fireEvent.click(within(dialog).getByRole('button', { name: 'Replace token' }));
    const field = within(dialog).getByLabelText('Personal access token');
    expect(field).toHaveValue('');
    expect(field).toHaveAttribute('type', 'password');
    fireEvent.change(field, { target: { value: 'ghp_new' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Validate and save' }));
    await waitFor(() => expect(api.putGithubPat).toHaveBeenCalledWith('ghp_new'));
    await waitFor(() =>
      expect(within(dialog).queryByLabelText('Personal access token')).toBeNull(),
    );
    expect(document.body.textContent).not.toContain('ghp_new');
  });

  it('checks and disconnects GitHub and Jira', async () => {
    const { dialog } = await open();
    fireEvent.click(within(dialog).getByRole('button', { name: 'Check GitHub' }));
    await waitFor(() => expect(api.checkAccount).toHaveBeenCalledWith('github'));
    expect(await within(dialog).findByText(/Checked just now/)).toBeInTheDocument();
    fireEvent.click(within(dialog).getByRole('button', { name: 'Disconnect GitHub' }));
    await waitFor(() => expect(api.disconnectAccount).toHaveBeenCalledWith('github'));
    fireEvent.click(within(dialog).getByRole('button', { name: 'Disconnect Jira' }));
    await waitFor(() => expect(api.disconnectAccount).toHaveBeenCalledWith('atlassian'));
  });

  it('shows Jira with its site and the Reconnect link', async () => {
    const { dialog } = await open();
    const card = within(within(dialog).getByRole('region', { name: 'Jira' }));
    expect(card.getByText('Jira · acme.atlassian.net')).toBeInTheDocument();
    expect(card.getByRole('link', { name: 'Reconnect' })).toHaveAttribute(
      'href',
      '/oauth/atlassian/start',
    );
    expect(card.queryByRole('combobox', { name: 'Jira site' })).toBeNull();
  });

  it('lets a Jira site be picked when there are several', async () => {
    api.getAccounts.mockResolvedValue({
      github: github(),
      jira: jira({
        sites: [
          { id: 's1', name: 'acme', url: 'https://acme.atlassian.net' },
          { id: 's2', name: 'beta', url: 'https://beta.atlassian.net' },
        ],
      }),
    });
    const { dialog } = await open();
    fireEvent.change(await within(dialog).findByRole('combobox', { name: 'Jira site' }), {
      target: { value: 's2' },
    });
    await waitFor(() =>
      expect(api.putSettings).toHaveBeenCalledWith({ ...settings, jiraSiteId: 's2' }),
    );
  });

  it('offers to connect what is not connected, or to set it up first', async () => {
    api.getAccounts.mockResolvedValue({
      github: { connected: false, login: null, method: null, warning: null },
      jira: { connected: false, needsReconnect: false, sites: [], selectedSiteId: null },
    });
    api.getSetup.mockResolvedValue({
      github: { configured: true, callbackUrl: '' },
      atlassian: { configured: false, callbackUrl: '' },
    });
    renderWithClient(<AccountsModal onClose={() => {}} />);
    expect(await screen.findByRole('link', { name: 'Connect GitHub' })).toHaveAttribute(
      'href',
      '/oauth/github/start',
    );
    expect(screen.getByRole('link', { name: 'Set up' })).toHaveAttribute('href', '/setup/');
    expect(screen.queryByRole('table')).toBeNull();
  });

  it('says a connection needs reconnecting', async () => {
    api.getAccounts.mockResolvedValue({
      github: github({ needsReconnect: true }),
      jira: jira({ needsReconnect: true }),
    });
    const { dialog } = await open();
    expect(within(dialog).getByText('Needs reconnect')).toBeInTheDocument();
    expect(within(dialog).getByText(/The Jira authorization expired/)).toBeInTheDocument();
  });

  it('signs out every browser', async () => {
    const { dialog } = await open();
    expect(within(dialog).getByRole('region', { name: 'Browsers' })).toBeInTheDocument();
    fireEvent.click(within(dialog).getByRole('button', { name: 'Sign out everywhere' }));
    await waitFor(() => expect(api.revokeAllSessions).toHaveBeenCalledTimes(1));
    expect(await within(dialog).findByText(/Signed out everywhere/)).toBeInTheDocument();
    expect(within(dialog).getByRole('button', { name: 'Sign out everywhere' })).toBeDisabled();
  });

  it('closes with Close and with Escape', async () => {
    const { dialog, onClose } = await open();
    fireEvent.click(within(dialog).getByRole('button', { name: 'Close' }));
    expect(onClose).toHaveBeenCalledTimes(1);
    fireEvent.keyDown(dialog, { key: 'Escape' });
    expect(onClose).toHaveBeenCalledTimes(2);
  });
});
