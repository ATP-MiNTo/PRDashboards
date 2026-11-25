import { fireEvent, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError, type Accounts, type Settings, type Setup } from '@/api/client';
import { renderWithClient } from '@/test-utils';
import { AccountsPanel } from './AccountsPanel';

const api = vi.hoisted(() => ({
  getAccounts: vi.fn(),
  getSetup: vi.fn(),
  getSettings: vi.fn(),
  putSettings: vi.fn(),
  disconnectAccount: vi.fn(),
  checkAccount: vi.fn(),
  revokeAllSessions: vi.fn(),
}));

vi.mock('@/api/client', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/api/client')>()),
  ...api,
}));

const settings: Settings = {
  polling: { githubSec: 60, jiraSec: 300 },
  theme: 'auto',
  defaultMethodForNewRepos: 'squash',
  deleteBranchAfterMerge: false,
  jiraSiteId: null,
  excludedStatuses: ['Done'],
};
const setup = (g: boolean, a: boolean): Setup => ({
  github: { configured: g, callbackUrl: 'http://127.0.0.1:4317/oauth/github/callback' },
  atlassian: { configured: a, callbackUrl: 'http://127.0.0.1:4317/oauth/atlassian/callback' },
});
const accounts = (over: Partial<Accounts> = {}): Accounts => ({
  github: { connected: false },
  jira: { connected: false, sites: [], selectedSiteId: null, needsReconnect: false },
  ...over,
});
const sites = [
  { id: 'a', name: 'Acme', url: 'https://acme.atlassian.net' },
  { id: 'b', name: 'Beta', url: 'https://beta.atlassian.net' },
];

beforeEach(() => {
  Object.values(api).forEach((m) => m.mockReset());
  api.getSetup.mockResolvedValue(setup(true, true));
  api.getSettings.mockResolvedValue(settings);
});

describe('AccountsPanel', () => {
  it('offers Connect links to the OAuth start routes when disconnected', async () => {
    api.getAccounts.mockResolvedValue(accounts());
    renderWithClient(<AccountsPanel />);
    expect(await screen.findByRole('link', { name: 'Connect GitHub' })).toHaveAttribute(
      'href',
      '/oauth/github/start',
    );
    expect(screen.getByRole('link', { name: 'Connect Jira' })).toHaveAttribute(
      'href',
      '/oauth/atlassian/start',
    );
  });

  it('sends people to setup when the app is not configured', async () => {
    api.getSetup.mockResolvedValue(setup(false, false));
    api.getAccounts.mockResolvedValue(accounts());
    renderWithClient(<AccountsPanel />);
    const links = await screen.findAllByRole('link', { name: 'Set up' });
    expect(links).toHaveLength(2);
    links.forEach((l) => expect(l).toHaveAttribute('href', '/setup/'));
  });

  it('shows GitHub as connected with the method and a warning', async () => {
    api.getAccounts.mockResolvedValue(
      accounts({
        github: { connected: true, login: 'octocat', method: 'pat', warning: 'Only granted repos' },
      }),
    );
    renderWithClient(<AccountsPanel />);
    expect(await screen.findByText('Connected as octocat (access token)')).toBeInTheDocument();
    expect(screen.getByText('Only granted repos')).toBeInTheDocument();
  });

  it('labels each GitHub sign-in method', async () => {
    for (const [method, label] of [
      ['app', 'GitHub App'],
      ['oauth', 'OAuth'],
      ['pat', 'access token'],
    ] as const) {
      api.getAccounts.mockResolvedValue(
        accounts({ github: { connected: true, login: 'octocat', method } }),
      );
      const { unmount } = renderWithClient(<AccountsPanel />);
      expect(await screen.findByText(`Connected as octocat (${label})`)).toBeInTheDocument();
      expect(screen.queryByRole('link', { name: 'Reconnect GitHub' })).toBeNull();
      unmount();
    }
  });

  it('shows Reconnect when the GitHub App authorization needs it', async () => {
    api.getAccounts.mockResolvedValue(
      accounts({
        github: { connected: true, login: 'octocat', method: 'app', needsReconnect: true },
      }),
    );
    renderWithClient(<AccountsPanel />);
    expect(await screen.findByText(/GitHub authorization expired/)).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Reconnect GitHub' })).toHaveAttribute(
      'href',
      '/oauth/github/start',
    );
  });

  it('disconnects and refetches', async () => {
    api.getAccounts.mockResolvedValueOnce(
      accounts({ github: { connected: true, login: 'o', method: 'oauth' } }),
    );
    api.getAccounts.mockResolvedValue(accounts());
    api.disconnectAccount.mockResolvedValue(undefined);
    renderWithClient(<AccountsPanel />);
    fireEvent.click(await screen.findByRole('button', { name: 'Disconnect GitHub' }));
    await waitFor(() => expect(api.disconnectAccount).toHaveBeenCalledWith('github'));
    expect(await screen.findByRole('link', { name: 'Connect GitHub' })).toBeInTheDocument();
  });

  it('shows Reconnect when Jira needs it', async () => {
    api.getAccounts.mockResolvedValue(
      accounts({ jira: { connected: false, sites, selectedSiteId: null, needsReconnect: true } }),
    );
    renderWithClient(<AccountsPanel />);
    expect(await screen.findByText(/authorization expired/)).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Reconnect Jira' })).toHaveAttribute(
      'href',
      '/oauth/atlassian/start',
    );
    expect(screen.queryByRole('link', { name: 'Connect Jira' })).toBeNull();
  });

  it('picks a Jira site by saving it in the settings', async () => {
    api.getAccounts.mockResolvedValue(
      accounts({ jira: { connected: true, sites, selectedSiteId: null, needsReconnect: false } }),
    );
    api.putSettings.mockResolvedValue({ ...settings, jiraSiteId: 'b' });
    renderWithClient(<AccountsPanel />);
    const select = await screen.findByLabelText('Jira site');
    await waitFor(() => expect(select).not.toBeDisabled());
    fireEvent.change(select, { target: { value: 'b' } });
    await waitFor(() =>
      expect(api.putSettings).toHaveBeenCalledWith({ ...settings, jiraSiteId: 'b' }),
    );
  });

  it('names the only site and has no picker', async () => {
    api.getAccounts.mockResolvedValue(
      accounts({
        jira: {
          connected: true,
          sites: sites.slice(0, 1),
          selectedSiteId: 'a',
          needsReconnect: false,
        },
      }),
    );
    renderWithClient(<AccountsPanel />);
    expect(await screen.findByText('Acme')).toBeInTheDocument();
    expect(screen.queryByLabelText('Jira site')).toBeNull();
  });

  it('checks the Jira connection', async () => {
    api.getAccounts.mockResolvedValue(
      accounts({
        jira: {
          connected: true,
          sites: sites.slice(0, 1),
          selectedSiteId: 'a',
          needsReconnect: false,
        },
      }),
    );
    api.checkAccount.mockResolvedValue(accounts());
    renderWithClient(<AccountsPanel />);
    fireEvent.click(await screen.findByRole('button', { name: 'Check Jira' }));
    await waitFor(() => expect(api.checkAccount).toHaveBeenCalledWith('atlassian'));
  });

  it('shows a readable message for a failed action, never the code', async () => {
    api.getAccounts.mockResolvedValue(
      accounts({ github: { connected: true, login: 'o', method: 'pat' } }),
    );
    api.checkAccount.mockRejectedValue(new ApiError(422, 'invalid_token'));
    renderWithClient(<AccountsPanel />);
    fireEvent.click(await screen.findByRole('button', { name: 'Check GitHub' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('GitHub rejected the token.');
    expect(screen.queryByText(/invalid_token/)).toBeNull();
  });

  it('signs out all browsers', async () => {
    api.getAccounts.mockResolvedValue(accounts());
    api.revokeAllSessions.mockResolvedValue(undefined);
    renderWithClient(<AccountsPanel />);
    fireEvent.click(await screen.findByRole('button', { name: 'Sign out all browsers' }));
    expect(await screen.findByText(/Signed out of all browsers/)).toBeInTheDocument();
  });

  it('shows an error when the accounts cannot be loaded', async () => {
    api.getAccounts.mockRejectedValue(new TypeError('network'));
    renderWithClient(<AccountsPanel />);
    expect(await screen.findByRole('alert')).toHaveTextContent('Could not load the accounts.');
  });
});
