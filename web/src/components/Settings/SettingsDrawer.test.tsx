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
  retryTransition: vi.fn(),
  dismissRetry: vi.fn(),
}));

vi.mock('@/api/client', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/api/client')>()),
  ...api,
}));

import { SettingsDrawer } from './SettingsDrawer';

const settings = {
  polling: { githubSec: 60, jiraSec: 300 },
  theme: 'auto',
  defaultMethodForNewRepos: 'squash',
  deleteBranchAfterMerge: false,
  jiraSiteId: null,
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
});

const lastSaved = () => api.putSettings.mock.calls.at(-1)?.[0];

describe('SettingsDrawer', () => {
  it('renders nothing while closed', () => {
    renderWithClient(<SettingsDrawer open={false} onClose={vi.fn()} />);
    expect(screen.queryByRole('dialog')).toBeNull();
  });

  it('is a modal dialog with every section and the later-release placeholders', async () => {
    renderWithClient(<SettingsDrawer open onClose={vi.fn()} />);
    const dialog = screen.getByRole('dialog', { name: 'Settings' });
    expect(dialog).toHaveAttribute('aria-modal', 'true');
    for (const name of [
      'Connected accounts',
      'Repositories',
      'Delete branch after merge',
      'Polling intervals',
      'Theme',
      'Status sync rules',
      'Multi-PR tickets',
      'Activity',
    ]) {
      expect(await within(dialog).findByRole('heading', { name })).toBeInTheDocument();
    }
  });

  it('moves focus into the dialog and gives it back on close', () => {
    const opener = document.createElement('button');
    document.body.append(opener);
    opener.focus();
    const { rerender } = renderWithClient(<SettingsDrawer open onClose={vi.fn()} />);
    expect(screen.getByRole('dialog')).toHaveFocus();
    rerender(<SettingsDrawer open={false} onClose={vi.fn()} />);
    expect(opener).toHaveFocus();
    opener.remove();
  });

  it('closes on Escape and on the scrim', () => {
    const onClose = vi.fn();
    renderWithClient(<SettingsDrawer open onClose={onClose} />);
    fireEvent.keyDown(screen.getByRole('dialog'), { key: 'Escape' });
    expect(onClose).toHaveBeenCalledTimes(1);
    fireEvent.click(screen.getByTestId('settings-scrim'));
    expect(onClose).toHaveBeenCalledTimes(2);
    fireEvent.click(screen.getByRole('button', { name: 'Close settings' }));
    expect(onClose).toHaveBeenCalledTimes(3);
  });

  it('keeps Tab inside the dialog', async () => {
    renderWithClient(<SettingsDrawer open onClose={vi.fn()} />);
    const dialog = screen.getByRole('dialog');
    await within(dialog).findByRole('heading', { name: 'Activity' });
    const items = [
      ...dialog.querySelectorAll<HTMLElement>(
        'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled])',
      ),
    ];
    const first = items[0]!;
    const last = items[items.length - 1]!;
    last.focus();
    fireEvent.keyDown(last, { key: 'Tab' });
    expect(first).toHaveFocus();
    first.focus();
    fireEvent.keyDown(first, { key: 'Tab', shiftKey: true });
    expect(last).toHaveFocus();
    dialog.focus();
    fireEvent.keyDown(dialog, { key: 'Tab', shiftKey: true });
    expect(last).toHaveFocus();
  });

  it('saves the delete-branch default, the polling intervals and the theme', async () => {
    renderWithClient(<SettingsDrawer open onClose={vi.fn()} />);
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
    fireEvent.click(screen.getByRole('button', { name: 'Dark' }));
    await waitFor(() => expect(lastSaved()).toEqual({ ...settings, theme: 'dark' }));
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
    renderWithClient(<SettingsDrawer open onClose={vi.fn()} />);
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
    renderWithClient(<SettingsDrawer open onClose={vi.fn()} />);
    fireEvent.click(await screen.findByRole('button', { name: 'Retry the move of INP-1' }));
    await waitFor(() => expect(api.retryTransition).toHaveBeenCalled());
    expect(api.retryTransition.mock.calls[0]?.[0]).toBe(7);
    fireEvent.click(screen.getByRole('button', { name: 'Dismiss the move of INP-1' }));
    await waitFor(() => expect(api.dismissRetry).toHaveBeenCalled());
    expect(api.dismissRetry.mock.calls[0]?.[0]).toBe(7);
  });
});
