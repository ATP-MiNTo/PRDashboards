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

const show = (onOpenRules: () => void = () => {}) =>
  renderWithClient(<SettingsContent onOpenRules={onOpenRules} off={new Set()} onOff={() => {}} />);

const lastSaved = () => api.putSettings.mock.calls.at(-1)?.[0];

describe('SettingsContent', () => {
  it('has the repositories and the polling, and nothing the dialogs took over, not even a theme block', async () => {
    show();
    const page = document.body;
    expect(
      await within(page).findByRole('heading', { name: /^Repositories setting/ }),
    ).toBeInTheDocument();
    expect(await within(page).findByRole('region', { name: 'Polling' })).toBeInTheDocument();
    // They moved to the repository dialog, the accounts dialog, the rules dialog and the History view.
    for (const gone of [
      'Delete branch after merge',
      'Sessions',
      'Status sync rules',
      'Multi-PR tickets',
      'Activity',
      'Theme',
    ]) {
      expect(within(page).queryByRole('heading', { name: gone })).toBeNull();
    }
    expect(within(page).queryByRole('checkbox', { name: /^Auto/ })).toBeNull();
    expect(within(page).queryByRole('button', { name: 'Sign out all browsers' })).toBeNull();
    expect(within(page).queryByRole('group', { name: 'Exclude statuses' })).toBeNull();
  });

  it('orders the sections: repositories, then polling', async () => {
    show();
    const page = document.body;
    const polling = await within(page).findByRole('region', { name: 'Polling' });
    const names = within(page)
      .getAllByRole('heading', { level: 3 })
      .map((h) => h.textContent?.replace(/\d+\/\d+$/, ''));
    expect(names).toEqual(['Repositories setting']);
    const repos = within(page).getByRole('heading', { name: /^Repositories setting/ });
    expect(repos.compareDocumentPosition(polling) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
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
      show();
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
      show();
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
      show();
      const j = await jira();
      const notPrevented = fireEvent.wheel(j, { deltaY: -100 });
      expect(notPrevented).toBe(false);
      expect(j).toHaveValue('900');
      await waitFor(() =>
        expect(lastSaved()).toEqual({ ...settings, polling: { githubSec: 60, jiraSec: 900 } }),
      );
    });

    it('saves both fields in one request when both were stepped', async () => {
      show();
      fireEvent.keyDown(await github(), { key: 'ArrowUp' });
      fireEvent.keyDown(await jira(), { key: 'ArrowDown' });
      await waitFor(() =>
        expect(lastSaved()).toEqual({ ...settings, polling: { githubSec: 75, jiraSec: 285 } }),
      );
      expect(api.putSettings).toHaveBeenCalledTimes(1);
    });

    it('stops at the ends of the range and ignores other keys', async () => {
      show();
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
      show();
      fireEvent.keyDown(await github(), { key: 'ArrowUp' });
      await waitFor(() => expect(api.putSettings).toHaveBeenCalled());
      expect(await screen.findByRole('alert')).toHaveTextContent('Could not save the settings.');
    });
  });
});

describe('Settings content on mount', () => {
  it('opens the rules dialog from its button and asks nothing from Jira or the rules on mount', async () => {
    const open = vi.fn();
    show(open);
    const button = await screen.findByRole('button', { name: 'Status sync rules' });
    expect(button).toHaveAttribute('aria-haspopup', 'dialog');
    expect(api.getJiraOptions).not.toHaveBeenCalled();
    expect(api.getRules).not.toHaveBeenCalled();
    expect(api.getActivity).not.toHaveBeenCalled();
    fireEvent.click(button);
    expect(open).toHaveBeenCalledTimes(1);
  });
});
