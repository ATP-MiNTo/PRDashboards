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
    renderWithClient(<SettingsContent onOpenRules={() => {}} />);
    const page = document.body;
    expect(within(page).queryByRole('heading', { name: 'Connected accounts' })).toBeNull();
    for (const name of [
      /^Repositories setting/,
      'Delete branch after merge',
      'Theme',
      'Sessions',
    ]) {
      expect(await within(page).findByRole('heading', { name })).toBeInTheDocument();
    }
    expect(within(page).getByRole('region', { name: 'Polling' })).toBeInTheDocument();
    // The rules, Exclude statuses, Multi-PR tickets and Activity live in the rules dialog now.
    for (const gone of ['Status sync rules', 'Multi-PR tickets', 'Activity']) {
      expect(within(page).queryByRole('heading', { name: gone })).toBeNull();
    }
    expect(within(page).queryByRole('group', { name: 'Exclude statuses' })).toBeNull();
  });

  it('orders the sections: repositories, polling, delete branch, sessions, theme', async () => {
    renderWithClient(<SettingsContent onOpenRules={() => {}} />);
    const page = document.body;
    await within(page).findByRole('heading', { name: 'Sessions' });
    await within(page).findByRole('region', { name: 'Polling' });
    const names = within(page)
      .getAllByRole('heading', { level: 3 })
      .map((h) => h.textContent?.replace(/\d+\/\d+$/, ''));
    expect(names).toEqual([
      'Repositories setting',
      'Delete branch after merge',
      'Sessions',
      'Theme',
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
    renderWithClient(<SettingsContent onOpenRules={() => {}} />);
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
      renderWithClient(<SettingsContent onOpenRules={() => {}} />);
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
      renderWithClient(<SettingsContent onOpenRules={() => {}} />);
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
      renderWithClient(<SettingsContent onOpenRules={() => {}} />);
      const j = await jira();
      const notPrevented = fireEvent.wheel(j, { deltaY: -100 });
      expect(notPrevented).toBe(false);
      expect(j).toHaveValue('900');
      await waitFor(() =>
        expect(lastSaved()).toEqual({ ...settings, polling: { githubSec: 60, jiraSec: 900 } }),
      );
    });

    it('saves both fields in one request when both were stepped', async () => {
      renderWithClient(<SettingsContent onOpenRules={() => {}} />);
      fireEvent.keyDown(await github(), { key: 'ArrowUp' });
      fireEvent.keyDown(await jira(), { key: 'ArrowDown' });
      await waitFor(() =>
        expect(lastSaved()).toEqual({ ...settings, polling: { githubSec: 75, jiraSec: 285 } }),
      );
      expect(api.putSettings).toHaveBeenCalledTimes(1);
    });

    it('stops at the ends of the range and ignores other keys', async () => {
      renderWithClient(<SettingsContent onOpenRules={() => {}} />);
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
      renderWithClient(<SettingsContent onOpenRules={() => {}} />);
      fireEvent.keyDown(await github(), { key: 'ArrowUp' });
      await waitFor(() => expect(api.putSettings).toHaveBeenCalled());
      expect(await screen.findByRole('alert')).toHaveTextContent('Could not save the settings.');
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
      renderWithClient(<SettingsContent onOpenRules={() => {}} />);
      expect(await auto()).not.toBeChecked();
      expect(screen.queryByRole('checkbox', { name: 'Dark mode' })).toBeNull();
    });

    it('turns Auto on and saves it', async () => {
      withTheme('dark');
      renderWithClient(<SettingsContent onOpenRules={() => {}} />);
      fireEvent.click(await auto());
      await waitFor(() => expect(lastSaved()).toEqual({ ...settings, theme: 'auto' }));
    });

    it('is checked in Auto, and turns Auto off by saving the mode in effect', async () => {
      withTheme('auto');
      renderWithClient(<SettingsContent onOpenRules={() => {}} />);
      const box = await auto();
      await waitFor(() => expect(box).toBeChecked());
      await waitFor(() => expect(document.documentElement.dataset.mode).toBeTruthy());
      const shown = document.documentElement.dataset.mode;
      fireEvent.click(box);
      await waitFor(() => expect(lastSaved()).toEqual({ ...settings, theme: shown }));
    });

    it('is disabled until the settings load', async () => {
      api.getSettings.mockReturnValue(new Promise(() => {}));
      renderWithClient(<SettingsContent onOpenRules={() => {}} />);
      expect(await screen.findByRole('checkbox', { name: /^Auto/ })).toBeDisabled();
    });
  });

  it('signs out every browser', async () => {
    api.revokeAllSessions.mockResolvedValue(undefined);
    renderWithClient(<SettingsContent onOpenRules={() => {}} />);
    fireEvent.click(await screen.findByRole('button', { name: 'Sign out all browsers' }));
    await waitFor(() => expect(api.revokeAllSessions).toHaveBeenCalledTimes(1));
    expect(await screen.findByText(/Signed out everywhere/)).toBeInTheDocument();
  });
});

describe('Settings content on mount', () => {
  it('opens the rules dialog from its button and asks nothing from Jira or the rules on mount', async () => {
    const open = vi.fn();
    renderWithClient(<SettingsContent onOpenRules={open} />);
    const button = await screen.findByRole('button', { name: 'Status sync rules' });
    expect(button).toHaveAttribute('aria-haspopup', 'dialog');
    expect(api.getJiraOptions).not.toHaveBeenCalled();
    expect(api.getRules).not.toHaveBeenCalled();
    expect(api.getActivity).not.toHaveBeenCalled();
    fireEvent.click(button);
    expect(open).toHaveBeenCalledTimes(1);
  });
});
