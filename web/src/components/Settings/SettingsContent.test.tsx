import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { pr, snapshot } from '@/test-fixtures';
import { renderWithClient } from '@/test-utils';

const api = vi.hoisted(() => ({
  getSettings: vi.fn(),
  putSettings: vi.fn(),
  getAccounts: vi.fn(),
  getSetup: vi.fn(),
  getRepos: vi.fn(),
  getSnapshot: vi.fn(),
  getActivity: vi.fn(),
  getRetries: vi.fn(),
  getRules: vi.fn(),
  getJiraOptions: vi.fn(),
  putRules: vi.fn(),
  previewRule: vi.fn(),
  revokeAllSessions: vi.fn(),
  retryTransition: vi.fn(),
  dismissRetry: vi.fn(),
}));

vi.mock('@/api/client', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/api/client')>()),
  ...api,
}));

import { SettingsContent } from './SettingsContent';

const settings = {
  polling: { githubSec: 60, jiraSec: 300 },
  theme: 'auto',
  defaultMethodForNewRepos: 'squash',
  deleteBranchAfterMerge: false,
  jiraSiteId: null,
  excludedStatuses: ['Done'],
};

beforeEach(() => {
  api.getSettings.mockResolvedValue(settings);
  api.putSettings.mockImplementation((s: unknown) => Promise.resolve(s));
  api.getAccounts.mockResolvedValue({
    github: { connected: false, login: null, method: null, warning: null },
    jira: { connected: false, needsReconnect: false, sites: [], selectedSiteId: null },
  });
  api.getSetup.mockResolvedValue({
    github: { configured: true, callbackUrl: '' },
    atlassian: { configured: true, callbackUrl: '' },
  });
  api.getRepos.mockResolvedValue({ repos: [] });
  api.getSnapshot.mockResolvedValue(snapshot([pr('a', 1)]));
  api.getActivity.mockResolvedValue({ items: [], nextCursor: null });
  api.getRetries.mockResolvedValue({ retries: [] });
  api.getRules.mockResolvedValue({ rules: [] });
  api.getJiraOptions.mockResolvedValue({ statuses: [], projects: [] });
});

const lastSaved = () => api.putSettings.mock.calls.at(-1)?.[0];

describe('SettingsContent', () => {
  it('has every section', async () => {
    renderWithClient(<SettingsContent />);
    const page = document.body;
    expect(within(page).queryByRole('heading', { name: 'Connected accounts' })).toBeNull();
    for (const name of [
      'Repositories',
      'Delete branch after merge',
      'Polling intervals',
      'Theme',
      'Status sync rules',
      'Multi-PR tickets',
      'Sessions',
      'Activity',
    ]) {
      expect(await within(page).findByRole('heading', { name })).toBeInTheDocument();
    }
  });

  it('orders the sections: repositories, preferences, rules, multi-PR, sessions, theme, activity', async () => {
    renderWithClient(<SettingsContent />);
    const page = document.body;
    await within(page).findByRole('heading', { name: 'Activity' });
    await within(page).findByRole('heading', { name: 'Polling intervals' });
    const names = within(page)
      .getAllByRole('heading', { level: 3 })
      .map((h) => h.textContent);
    expect(names).toEqual([
      'Repositories',
      'Delete branch after merge',
      'Polling intervals',
      'Status sync rules',
      'Multi-PR tickets',
      'Sessions',
      'Theme',
      'Activity',
    ]);
  });

  it('puts both polling selects on one line with a hidden separator', async () => {
    renderWithClient(<SettingsContent />);
    const github = await screen.findByLabelText('GitHub');
    const jira = screen.getByLabelText('Jira');
    expect(github.closest('div')).toBe(jira.closest('div'));
    const line = github.closest('div')!;
    const sep = within(line).getByText('|');
    expect(sep).toHaveAttribute('aria-hidden', 'true');
    expect(github.compareDocumentPosition(sep) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(sep.compareDocumentPosition(jira) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it('saves the delete-branch default and the polling intervals', async () => {
    renderWithClient(<SettingsContent />);
    fireEvent.click(await screen.findByRole('checkbox', { name: /Delete the head branch/ }));
    await waitFor(() =>
      expect(api.putSettings).toHaveBeenLastCalledWith({
        ...settings,
        deleteBranchAfterMerge: true,
      }),
    );
    fireEvent.change(screen.getByLabelText('GitHub'), { target: { value: '900' } });
    await waitFor(() =>
      expect(lastSaved()).toEqual({ ...settings, polling: { githubSec: 900, jiraSec: 300 } }),
    );
  });

  describe('theme', () => {
    beforeEach(() => document.documentElement.removeAttribute('data-mode'));

    const ready = async (name: string | RegExp) => {
      const box = await screen.findByRole('checkbox', { name });
      await waitFor(() => expect(box).toBeEnabled());
      return box as HTMLInputElement;
    };
    const darkMode = () => ready('Dark mode');
    const auto = () => ready(/^Auto \(light from 07:00 to 19:00\)/);
    const withTheme = (theme: string) => api.getSettings.mockResolvedValue({ ...settings, theme });

    it('reflects the stored theme in the swap', async () => {
      withTheme('dark');
      renderWithClient(<SettingsContent />);
      const swap = await darkMode();
      await waitFor(() => expect(swap).toBeChecked());
      expect(await auto()).not.toBeChecked();
    });

    it('saves an explicit dark from a light start', async () => {
      withTheme('light');
      renderWithClient(<SettingsContent />);
      const swap = await darkMode();
      await waitFor(() => expect(document.documentElement.dataset.mode).toBe('light'));
      expect(swap).not.toBeChecked();
      fireEvent.click(swap);
      await waitFor(() => expect(lastSaved()).toEqual({ ...settings, theme: 'dark' }));
    });

    it('saves an explicit light from a dark start', async () => {
      withTheme('dark');
      renderWithClient(<SettingsContent />);
      fireEvent.click(await darkMode());
      await waitFor(() => expect(lastSaved()).toEqual({ ...settings, theme: 'light' }));
    });

    it('turns Auto on and saves it', async () => {
      withTheme('dark');
      renderWithClient(<SettingsContent />);
      fireEvent.click(await auto());
      await waitFor(() => expect(lastSaved()).toEqual({ ...settings, theme: 'auto' }));
    });

    it('turns Auto off by saving the mode in effect', async () => {
      withTheme('auto');
      renderWithClient(<SettingsContent />);
      const swap = await darkMode();
      const box = await auto();
      await waitFor(() => expect(box).toBeChecked());
      await waitFor(() => expect(document.documentElement.dataset.mode).toBeTruthy());
      const shown = swap.checked ? 'dark' : 'light';
      expect(document.documentElement.dataset.mode).toBe(shown);
      fireEvent.click(box);
      await waitFor(() => expect(lastSaved()).toEqual({ ...settings, theme: shown }));
    });

    it('disables both controls until the settings load', async () => {
      api.getSettings.mockReturnValue(new Promise(() => {}));
      renderWithClient(<SettingsContent />);
      expect(await screen.findByRole('checkbox', { name: 'Dark mode' })).toBeDisabled();
      expect(await screen.findByRole('checkbox', { name: /^Auto/ })).toBeDisabled();
    });
  });

  it('lists the activity and loads the next page', async () => {
    const item = (id: number, summary: string) => ({
      id,
      createdAt: '2026-11-24T11:00:00Z',
      kind: 'transition',
      ticketKey: 'INP-1',
      prId: 'a',
      triggeredBy: 'manual',
      result: id === 2 ? 'failed' : 'ok',
      summary,
      error: id === 2 ? 'Jira answered 503' : null,
    });
    api.getActivity
      .mockResolvedValueOnce({
        items: [item(3, 'Moved INP-1 to Done'), item(2, 'Move INP-2 failed')],
        nextCursor: 2,
      })
      .mockResolvedValueOnce({ items: [item(1, 'Merged #1')], nextCursor: null });
    renderWithClient(<SettingsContent />);
    const log = await screen.findByRole('list', { name: 'Activity' });
    expect(await within(log).findByText('Move INP-2 failed', { exact: false })).toBeInTheDocument();
    expect(within(log).getByText('Jira answered 503')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Load more' }));
    expect(await within(log).findByText('Merged #1', { exact: false })).toBeInTheDocument();
    expect(api.getActivity).toHaveBeenNthCalledWith(1, undefined);
    expect(api.getActivity).toHaveBeenNthCalledWith(2, 2);
    expect(screen.queryByRole('button', { name: 'Load more' })).toBeNull();
  });

  it('lists the Jira moves to retry with retry and dismiss', async () => {
    api.getRetries.mockResolvedValue({
      retries: [
        {
          id: 7,
          prId: 'a',
          ticketKey: 'INP-1',
          transitionId: '31',
          toStatus: 'Done',
          lastError: 'Jira answered 503',
          createdAt: '2026-11-24T11:00:00Z',
        },
      ],
    });
    api.retryTransition.mockResolvedValue({
      step: {
        kind: 'transition',
        ticket: 'INP-1',
        state: 'ok',
        message: null,
        toStatus: 'Done',
        retryId: null,
      },
    });
    api.dismissRetry.mockResolvedValue(undefined);
    renderWithClient(<SettingsContent />);
    fireEvent.click(await screen.findByRole('button', { name: 'Retry the move of INP-1' }));
    await waitFor(() => expect(api.retryTransition).toHaveBeenCalled());
    expect(api.retryTransition.mock.calls[0]?.[0]).toBe(7);
    fireEvent.click(screen.getByRole('button', { name: 'Dismiss the move of INP-1' }));
    await waitFor(() => expect(api.dismissRetry).toHaveBeenCalled());
    expect(api.dismissRetry.mock.calls[0]?.[0]).toBe(7);
  });

  it('signs out every browser', async () => {
    api.revokeAllSessions.mockResolvedValue(undefined);
    renderWithClient(<SettingsContent />);
    fireEvent.click(await screen.findByRole('button', { name: 'Sign out all browsers' }));
    await waitFor(() => expect(api.revokeAllSessions).toHaveBeenCalledTimes(1));
    expect(await screen.findByText(/Signed out everywhere/)).toBeInTheDocument();
  });

  it('shows a rule move that needs input in the activity', async () => {
    api.getActivity.mockResolvedValue({
      items: [
        {
          id: 1,
          kind: 'transition',
          result: 'needs_input',
          summary: 'INP-7 to Done',
          error: null,
          triggeredBy: 'rule:r1',
          createdAt: '2026-11-24T11:00:00Z',
        },
      ],
      nextCursor: null,
    });
    renderWithClient(<SettingsContent />);
    expect(await screen.findByText('needs input')).toBeInTheDocument();
    expect(screen.getByText(/by a status sync rule/)).toBeInTheDocument();
    expect(screen.getByText(/Jira asks for fields on this move/)).toBeInTheDocument();
  });
});
