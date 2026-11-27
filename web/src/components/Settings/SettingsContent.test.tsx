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
      /^Repositories setting/,
      'Delete branch after merge',
      'Theme',
      'Status sync rules',
      'Multi-PR tickets',
      'Sessions',
      'Activity',
    ]) {
      expect(await within(page).findByRole('heading', { name })).toBeInTheDocument();
    }
    expect(within(page).getByRole('region', { name: 'Polling' })).toBeInTheDocument();
  });

  it('orders the sections: repositories, polling, delete branch, rules, multi-PR, sessions, theme, activity', async () => {
    renderWithClient(<SettingsContent />);
    const page = document.body;
    await within(page).findByRole('heading', { name: 'Activity' });
    await within(page).findByRole('region', { name: 'Polling' });
    const names = within(page)
      .getAllByRole('heading', { level: 3 })
      .map((h) => h.textContent?.replace(/\d+\/\d+$/, ''));
    expect(names).toEqual([
      'Repositories setting',
      'Delete branch after merge',
      'Status sync rules',
      'Multi-PR tickets',
      'Sessions',
      'Theme',
      'Activity',
    ]);
    const polling = within(page).getByRole('region', { name: 'Polling' });
    const repos = names.indexOf('Repositories setting');
    expect(repos).toBe(0);
    const heading = (n: string) => within(page).getByRole('heading', { name: new RegExp('^' + n) });
    expect(
      heading('Repositories setting').compareDocumentPosition(polling) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
    expect(
      polling.compareDocumentPosition(heading('Delete branch after merge')) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
  });

  it('saves the delete-branch default', async () => {
    renderWithClient(<SettingsContent />);
    fireEvent.click(await screen.findByRole('checkbox', { name: /Delete the head branch/ }));
    await waitFor(() =>
      expect(api.putSettings).toHaveBeenLastCalledWith({
        ...settings,
        deleteBranchAfterMerge: true,
      }),
    );
  });

  describe('polling', () => {
    const github = () => screen.findByRole('spinbutton', { name: 'GitHub polling seconds' });
    const jira = () => screen.findByRole('spinbutton', { name: 'Jira polling seconds' });

    // The server keeps what was saved, so the refetch after a save returns it.
    beforeEach(() => {
      api.putSettings.mockImplementation((saved: unknown) => {
        api.getSettings.mockResolvedValue(saved);
        return Promise.resolve(saved);
      });
    });

    it('shows the two intervals in seconds as fields that are stepped, not typed into', async () => {
      renderWithClient(<SettingsContent />);
      const gh = await github();
      expect(gh).toHaveValue('60');
      expect(gh).toHaveAttribute('readonly');
      expect(gh).toHaveAttribute('aria-valuemin', '30');
      expect(gh).toHaveAttribute('aria-valuemax', '3600');
      expect(gh).toHaveAttribute('aria-valuenow', '60');
      expect(gh).toHaveAttribute('aria-valuetext', '1 minute');
      const j = await jira();
      expect(j).toHaveValue('300');
      expect(j).toHaveAttribute('aria-valuetext', '5 minutes');
      expect(screen.getByText('GitHub polling')).toBeInTheDocument();
      expect(screen.getByText('Jira polling')).toBeInTheDocument();
    });

    it('steps with the arrow keys and saves once the stepping has paused', async () => {
      renderWithClient(<SettingsContent />);
      const gh = await github();
      fireEvent.keyDown(gh, { key: 'ArrowUp' });
      fireEvent.keyDown(gh, { key: 'ArrowUp' });
      expect(gh).toHaveValue('90');
      expect(api.putSettings).not.toHaveBeenCalled();
      await waitFor(() =>
        expect(lastSaved()).toEqual({ ...settings, polling: { githubSec: 90, jiraSec: 300 } }),
      );
      expect(api.putSettings).toHaveBeenCalledTimes(1);
      fireEvent.keyDown(gh, { key: 'ArrowDown' });
      await waitFor(() =>
        expect(lastSaved()).toEqual({ ...settings, polling: { githubSec: 75, jiraSec: 300 } }),
      );
    });

    it('steps with the mouse wheel: up is longer, down is shorter', async () => {
      renderWithClient(<SettingsContent />);
      const j = await jira();
      const notPrevented = fireEvent.wheel(j, { deltaY: -100 });
      expect(notPrevented).toBe(false);
      expect(j).toHaveValue('900');
      await waitFor(() =>
        expect(lastSaved()).toEqual({ ...settings, polling: { githubSec: 60, jiraSec: 900 } }),
      );
    });

    it('saves both fields in one request when both were stepped', async () => {
      renderWithClient(<SettingsContent />);
      fireEvent.keyDown(await github(), { key: 'ArrowUp' });
      fireEvent.keyDown(await jira(), { key: 'ArrowDown' });
      await waitFor(() =>
        expect(lastSaved()).toEqual({ ...settings, polling: { githubSec: 75, jiraSec: 285 } }),
      );
      expect(api.putSettings).toHaveBeenCalledTimes(1);
    });

    it('stops at the ends of the range and ignores other keys', async () => {
      renderWithClient(<SettingsContent />);
      const gh = await github();
      fireEvent.keyDown(gh, { key: 'ArrowDown' });
      fireEvent.keyDown(gh, { key: 'ArrowDown' });
      fireEvent.keyDown(gh, { key: 'ArrowDown' });
      fireEvent.keyDown(gh, { key: 'a' });
      expect(gh).toHaveValue('30');
      await waitFor(() =>
        expect(lastSaved()).toEqual({ ...settings, polling: { githubSec: 30, jiraSec: 300 } }),
      );
      expect(api.putSettings).toHaveBeenCalledTimes(1);
    });

    it('says so when the save fails', async () => {
      api.putSettings.mockRejectedValue(new Error('no'));
      renderWithClient(<SettingsContent />);
      fireEvent.keyDown(await github(), { key: 'ArrowUp' });
      await waitFor(() => expect(api.putSettings).toHaveBeenCalled());
      expect(await screen.findByRole('alert')).toHaveTextContent('Could not save the settings.');
    });

    it('has the Status sync rules button, which goes to the rules and focuses them', async () => {
      const scrollIntoView = vi.fn();
      Element.prototype.scrollIntoView = scrollIntoView;
      renderWithClient(<SettingsContent />);
      const heading = await screen.findByRole('heading', { name: 'Status sync rules' });
      const button = await screen.findByRole('button', { name: 'Status sync rules' });
      fireEvent.click(button);
      expect(heading).toHaveFocus();
      expect(scrollIntoView).toHaveBeenCalledTimes(1);
    });
  });

  describe('theme', () => {
    beforeEach(() => document.documentElement.removeAttribute('data-mode'));

    const auto = async () => {
      const box = await screen.findByRole('checkbox', {
        name: /^Auto \(light from 07:00 to 19:00\)/,
      });
      await waitFor(() => expect(box).toBeEnabled());
      return box as HTMLInputElement;
    };
    const withTheme = (theme: string) => api.getSettings.mockResolvedValue({ ...settings, theme });

    it('has only the Auto checkbox: the sun and moon button is in the header', async () => {
      withTheme('dark');
      renderWithClient(<SettingsContent />);
      expect(await auto()).not.toBeChecked();
      expect(screen.queryByRole('checkbox', { name: 'Dark mode' })).toBeNull();
    });

    it('turns Auto on and saves it', async () => {
      withTheme('dark');
      renderWithClient(<SettingsContent />);
      fireEvent.click(await auto());
      await waitFor(() => expect(lastSaved()).toEqual({ ...settings, theme: 'auto' }));
    });

    it('is checked in Auto, and turns Auto off by saving the mode in effect', async () => {
      withTheme('auto');
      renderWithClient(<SettingsContent />);
      const box = await auto();
      await waitFor(() => expect(box).toBeChecked());
      await waitFor(() => expect(document.documentElement.dataset.mode).toBeTruthy());
      const shown = document.documentElement.dataset.mode;
      fireEvent.click(box);
      await waitFor(() => expect(lastSaved()).toEqual({ ...settings, theme: shown }));
    });

    it('is disabled until the settings load', async () => {
      api.getSettings.mockReturnValue(new Promise(() => {}));
      renderWithClient(<SettingsContent />);
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

describe('Settings content on mount', () => {
  it('makes no Jira options request until a rule form opens', async () => {
    renderWithClient(<SettingsContent />);
    await screen.findByRole('heading', { name: 'Activity' });
    await screen.findByText('No rules yet.');
    expect(api.getJiraOptions).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole('button', { name: 'Add rule' }));
    await waitFor(() => expect(api.getJiraOptions).toHaveBeenCalledTimes(1));
  });
});
