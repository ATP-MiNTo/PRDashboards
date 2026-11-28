import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from '@/api/client';
import { pr, snapshot } from '@/test-fixtures';
import { renderWithClient } from '@/test-utils';

const api = vi.hoisted(() => ({
  getAccounts: vi.fn(),
  getSetup: vi.fn(),
  getRepos: vi.fn(),
  getSettings: vi.fn(),
  putSettings: vi.fn(),
  disconnectAccount: vi.fn(),
  checkAccount: vi.fn(),
  getSnapshot: vi.fn(),
  addGithubToken: vi.fn(),
  renameGithubToken: vi.fn(),
  deleteGithubToken: vi.fn(),
  checkGithubToken: vi.fn(),
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
const token = (over: Record<string, unknown> = {}) => ({
  id: 'aaaaaaaa',
  name: 'Home',
  method: 'app',
  fineGrained: false,
  login: 'minatpt',
  warning: null,
  needsReconnect: false,
  ...over,
});
const work = token({
  id: 'bbbbbbbb',
  name: 'Work',
  method: 'pat',
  fineGrained: true,
  login: 'minto',
  warning: 'Fine-grained tokens only see the repositories they were granted.',
});
const accounts = (tokens: unknown[] = [token()], over: Record<string, unknown> = {}) => ({
  github: github(),
  githubTokens: tokens,
  githubTokenLimit: 10,
  jira: jira(),
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
  multiPrTickets: 'all',
  jiraSiteId: 's1',
  excludedStatuses: [],
};

beforeEach(() => {
  Object.values(api).forEach((f) => f.mockReset());
  api.getAccounts.mockResolvedValue(accounts());
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
  api.getSnapshot.mockResolvedValue(snapshot([pr('a', 1)]));
  api.addGithubToken.mockResolvedValue(token({ id: 'cccccccc', name: 'New' }));
  api.renameGithubToken.mockImplementation(({ name }: { name: string }) =>
    Promise.resolve(token({ name })),
  );
  api.deleteGithubToken.mockResolvedValue(undefined);
  api.checkGithubToken.mockResolvedValue(accounts());
  api.revokeAllSessions.mockResolvedValue(undefined);
});

async function open(onClose = vi.fn()) {
  renderWithClient(<AccountsModal onClose={onClose} />);
  const dialog = await screen.findByRole('dialog', { name: 'Accounts' });
  await within(dialog).findByRole('region', { name: 'GitHub' });
  return { dialog, onClose };
}

const tokenRow = (dialog: HTMLElement, name: string) => {
  const list = within(dialog).getByRole('list', { name: 'Connected tokens' });
  const row = within(list)
    .getAllByRole('listitem')
    .find((li) => within(li).queryByRole('button', { name: `Rename ${name}` }));
  if (!row) throw new Error(`no row for ${name}`);
  return within(row);
};

describe('AccountsModal', () => {
  it('lists the tokens under "GitHub tokens" with the counter, the kind, the login and Primary', async () => {
    api.getAccounts.mockResolvedValue(accounts([token(), work]));
    const { dialog } = await open();
    const card = within(within(dialog).getByRole('region', { name: 'GitHub' }));
    expect(card.getByRole('heading', { name: /GitHub tokens/ })).toBeInTheDocument();
    expect(card.getByText('2 of 10')).toBeInTheDocument();
    expect(card.getByText(/Add one token per owner/)).toBeInTheDocument();
    const rows = card.getAllByRole('listitem');
    expect(rows).toHaveLength(2);
    const home = within(rows[0]!);
    expect(home.getByText('Home')).toBeInTheDocument();
    expect(home.getByText('GitHub App')).toBeInTheDocument();
    expect(home.getByText('minatpt')).toBeInTheDocument();
    expect(home.getByText('Primary')).toBeInTheDocument();
    expect(home.getByRole('link', { name: 'Reconnect Home' })).toHaveAttribute(
      'href',
      '/oauth/github/start',
    );
    const second = within(rows[1]!);
    expect(second.getByText('Fine-grained token')).toBeInTheDocument();
    expect(second.getByText('minto')).toBeInTheDocument();
    expect(second.getByText(/only see the repositories they were granted/)).toBeInTheDocument();
    expect(second.queryByText('Primary')).toBeNull();
    expect(second.queryByRole('link', { name: /Reconnect/ })).toBeNull();
    expect(card.getByRole('button', { name: 'Disconnect GitHub' })).toBeInTheDocument();
  });

  it('shows the repository table with the token name and kind of each repository', async () => {
    api.getAccounts.mockResolvedValue(accounts([token(), work]));
    api.getSnapshot.mockResolvedValue(
      snapshot([
        pr('a', 1, {
          repo: { ...pr('a', 1).repo, id: 'R1', tokenId: 'bbbbbbbb', tokenName: 'Work' },
        }),
        pr('b', 2, {
          repo: { ...pr('b', 2).repo, id: 'R2', nameWithOwner: 'acme/api', tokenName: 'Home' },
        }),
      ]),
    );
    const { dialog } = await open();
    const table = within(within(dialog).getByRole('table'));
    expect(table.getAllByRole('columnheader').map((h) => h.textContent)).toEqual([
      'Repository',
      'Source',
    ]);
    expect(await table.findByText('Work · Fine-grained token')).toBeInTheDocument();
    expect(table.getByText('Home · GitHub App')).toBeInTheDocument();
  });

  it('falls back to the way GitHub is connected for a repository the snapshot has no token for', async () => {
    const { dialog } = await open();
    const table = within(within(dialog).getByRole('table'));
    expect(table.getByText('acme/web')).toBeInTheDocument();
    expect(table.getAllByText('GitHub App')).toHaveLength(2);
  });

  it('adds a token with an optional name and never shows it again', async () => {
    const { dialog } = await open();
    const name = within(dialog).getByLabelText('Token name (optional)');
    const field = within(dialog).getByLabelText('Personal access token');
    expect(field).toHaveValue('');
    expect(field).toHaveAttribute('type', 'password');
    fireEvent.change(name, { target: { value: '  Work  ' } });
    fireEvent.change(field, { target: { value: 'ghp_new' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Validate and add' }));
    await waitFor(() =>
      expect(api.addGithubToken).toHaveBeenCalledWith({ token: 'ghp_new', name: 'Work' }),
    );
    expect(await within(dialog).findByText('Added New.')).toBeInTheDocument();
    expect(field).toHaveValue('');
    expect(name).toHaveValue('');
    expect(document.body.textContent).not.toContain('ghp_new');
  });

  it('sends no name when the name is left empty', async () => {
    const { dialog } = await open();
    fireEvent.change(within(dialog).getByLabelText('Personal access token'), {
      target: { value: 'ghp_x' },
    });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Validate and add' }));
    await waitFor(() => expect(api.addGithubToken).toHaveBeenCalledWith({ token: 'ghp_x' }));
  });

  it('refuses a name that is too long before asking the server', async () => {
    const { dialog } = await open();
    fireEvent.change(within(dialog).getByLabelText('Token name (optional)'), {
      target: { value: 'x'.repeat(41) },
    });
    fireEvent.change(within(dialog).getByLabelText('Personal access token'), {
      target: { value: 'ghp_x' },
    });
    expect(within(dialog).getByText('Use at most 40 characters.')).toBeInTheDocument();
    expect(within(dialog).getByRole('button', { name: 'Validate and add' })).toBeDisabled();
  });

  it.each([
    ['token_limit', 'You can connect up to 10 tokens.'],
    ['token_duplicate', 'That token is already connected.'],
    ['token_name_taken', 'Another token already has that name.'],
    ['invalid_token', 'GitHub rejected the token.'],
    ['insufficient_scopes', 'The token lacks scopes the app needs (repo and read:org).'],
  ])('says what went wrong when adding fails with %s', async (code, text) => {
    api.addGithubToken.mockRejectedValue(new ApiError(409, code));
    const { dialog } = await open();
    fireEvent.change(within(dialog).getByLabelText('Personal access token'), {
      target: { value: 'ghp_x' },
    });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Validate and add' }));
    expect(await within(dialog).findByRole('alert')).toHaveTextContent(text);
  });

  it('disables the add form at the limit and says so', async () => {
    const many = Array.from({ length: 10 }, (_, i) =>
      token({ id: `0000000${i}`.slice(-8), name: `Token ${i + 1}`, method: 'pat' }),
    );
    api.getAccounts.mockResolvedValue(accounts(many));
    const { dialog } = await open();
    expect(within(dialog).getByText('10 of 10')).toBeInTheDocument();
    expect(within(dialog).getByText('You can connect up to 10 tokens')).toBeInTheDocument();
    expect(within(dialog).getByLabelText('Personal access token')).toBeDisabled();
    expect(within(dialog).getByLabelText('Token name (optional)')).toBeDisabled();
    expect(within(dialog).getByRole('button', { name: 'Validate and add' })).toBeDisabled();
  });

  it('renames a token in place', async () => {
    api.getAccounts.mockResolvedValue(accounts([token(), work]));
    const { dialog } = await open();
    const row = tokenRow(dialog, 'Work');
    fireEvent.click(row.getByRole('button', { name: 'Rename Work' }));
    const field = row.getByLabelText('Name of Work');
    expect(field).toHaveValue('Work');
    fireEvent.change(field, { target: { value: ' Client ' } });
    fireEvent.click(row.getByRole('button', { name: 'Save name' }));
    await waitFor(() =>
      expect(api.renameGithubToken).toHaveBeenCalledWith({ id: 'bbbbbbbb', name: 'Client' }),
    );
    await waitFor(() => expect(within(dialog).queryByLabelText('Name of Work')).toBeNull());
  });

  it('validates the name and cancels a rename', async () => {
    const { dialog } = await open();
    const row = tokenRow(dialog, 'Home');
    fireEvent.click(row.getByRole('button', { name: 'Rename Home' }));
    const field = row.getByLabelText('Name of Home');
    fireEvent.change(field, { target: { value: '   ' } });
    expect(row.getByText('Give the token a name.')).toBeInTheDocument();
    expect(row.getByRole('button', { name: 'Save name' })).toBeDisabled();
    fireEvent.change(field, { target: { value: 'x'.repeat(41) } });
    expect(row.getByRole('button', { name: 'Save name' })).toBeDisabled();
    fireEvent.click(row.getByRole('button', { name: 'Cancel' }));
    expect(api.renameGithubToken).not.toHaveBeenCalled();
    expect(row.getByText('Home')).toBeInTheDocument();
  });

  it('cancels a rename with Escape without closing the dialog', async () => {
    const { dialog, onClose } = await open();
    const row = tokenRow(dialog, 'Home');
    fireEvent.click(row.getByRole('button', { name: 'Rename Home' }));
    fireEvent.keyDown(row.getByLabelText('Name of Home'), { key: 'Escape' });
    expect(row.queryByLabelText('Name of Home')).toBeNull();
    expect(onClose).not.toHaveBeenCalled();
  });

  it('shows token_name_taken when the server refuses the new name', async () => {
    api.renameGithubToken.mockRejectedValue(new ApiError(409, 'token_name_taken'));
    const { dialog } = await open();
    const row = tokenRow(dialog, 'Home');
    fireEvent.click(row.getByRole('button', { name: 'Rename Home' }));
    fireEvent.change(row.getByLabelText('Name of Home'), { target: { value: 'Work' } });
    fireEvent.click(row.getByRole('button', { name: 'Save name' }));
    expect(await row.findByRole('alert')).toHaveTextContent('Another token already has that name.');
    expect(row.getByLabelText('Name of Home')).toBeInTheDocument();
  });

  it('asks before it removes a token', async () => {
    api.getAccounts.mockResolvedValue(accounts([token(), work]));
    const { dialog } = await open();
    const row = tokenRow(dialog, 'Work');
    fireEvent.click(row.getByRole('button', { name: 'Remove Work' }));
    expect(row.getByText(/Remove Work\?/)).toBeInTheDocument();
    expect(row.queryByText(/disconnects GitHub/)).toBeNull();
    expect(api.deleteGithubToken).not.toHaveBeenCalled();
    fireEvent.click(row.getByRole('button', { name: 'Keep' }));
    expect(row.queryByRole('button', { name: 'Confirm remove' })).toBeNull();
    fireEvent.click(row.getByRole('button', { name: 'Remove Work' }));
    fireEvent.click(row.getByRole('button', { name: 'Confirm remove' }));
    await waitFor(() => expect(api.deleteGithubToken).toHaveBeenCalledWith('bbbbbbbb'));
  });

  it('says that removing the last token disconnects GitHub', async () => {
    const { dialog } = await open();
    const row = tokenRow(dialog, 'Home');
    fireEvent.click(row.getByRole('button', { name: 'Remove Home' }));
    expect(row.getByText(/It is your only token, so this disconnects GitHub/)).toBeInTheDocument();
  });

  it('says so when a token cannot be removed', async () => {
    api.deleteGithubToken.mockRejectedValue(new ApiError(404, 'not_found'));
    const { dialog } = await open();
    const row = tokenRow(dialog, 'Home');
    fireEvent.click(row.getByRole('button', { name: 'Remove Home' }));
    fireEvent.click(row.getByRole('button', { name: 'Confirm remove' }));
    expect(await row.findByRole('alert')).toHaveTextContent('That item no longer exists.');
  });

  it('checks one token', async () => {
    api.getAccounts.mockResolvedValue(accounts([token(), work]));
    const { dialog } = await open();
    const row = tokenRow(dialog, 'Work');
    fireEvent.click(row.getByRole('button', { name: 'Check Work' }));
    await waitFor(() => expect(api.checkGithubToken).toHaveBeenCalledWith('bbbbbbbb'));
    expect(await row.findByText(/Checked just now/)).toBeInTheDocument();
    expect(tokenRow(dialog, 'Home').queryByText(/Checked just now/)).toBeNull();
    expect(api.checkAccount).not.toHaveBeenCalled();
  });

  it('says so when a check fails', async () => {
    api.checkGithubToken.mockRejectedValue(new ApiError(502, 'upstream_error'));
    const { dialog } = await open();
    const row = tokenRow(dialog, 'Home');
    fireEvent.click(row.getByRole('button', { name: 'Check Home' }));
    expect(await row.findByRole('alert')).toHaveTextContent('The provider could not be reached.');
  });

  it('says which tokens need a reconnect and how to fix each kind', async () => {
    api.getAccounts.mockResolvedValue(
      accounts([token({ needsReconnect: true }), { ...work, needsReconnect: true }], {
        github: github({ needsReconnect: true }),
      }),
    );
    const { dialog } = await open();
    expect(within(dialog).getAllByText('Needs reconnect')).toHaveLength(2);
    const home = tokenRow(dialog, 'Home');
    expect(home.getByText(/Reconnect to continue/)).toBeInTheDocument();
    expect(home.getByRole('link', { name: 'Reconnect Home' })).toBeInTheDocument();
    const second = tokenRow(dialog, 'Work');
    expect(second.getByText(/Remove it and add a new one/)).toBeInTheDocument();
    expect(second.queryByRole('link', { name: /Reconnect/ })).toBeNull();
    expect(second.getByRole('button', { name: 'Remove Work' })).toBeInTheDocument();
  });

  it('disconnects every GitHub token and Jira, and checks Jira', async () => {
    const { dialog } = await open();
    fireEvent.click(within(dialog).getByRole('button', { name: 'Disconnect GitHub' }));
    await waitFor(() => expect(api.disconnectAccount).toHaveBeenCalledWith('github'));
    fireEvent.click(within(dialog).getByRole('button', { name: 'Check Jira' }));
    await waitFor(() => expect(api.checkAccount).toHaveBeenCalledWith('atlassian'));
    expect(await within(dialog).findByText(/Checked just now/)).toBeInTheDocument();
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
    api.getAccounts.mockResolvedValue(
      accounts([token()], {
        jira: jira({
          sites: [
            { id: 's1', name: 'acme', url: 'https://acme.atlassian.net' },
            { id: 's2', name: 'beta', url: 'https://beta.atlassian.net' },
          ],
        }),
      }),
    );
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
      githubTokens: [],
      githubTokenLimit: 10,
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
    expect(screen.queryByRole('form', { name: 'Add token' })).toBeNull();
  });

  it('says a connection needs reconnecting', async () => {
    api.getAccounts.mockResolvedValue(
      accounts([token({ needsReconnect: true })], {
        github: github({ needsReconnect: true }),
        jira: jira({ needsReconnect: true }),
      }),
    );
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
