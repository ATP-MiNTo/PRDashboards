import { act, fireEvent, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from '@/api/client';
import { pr } from '@/test-fixtures';
import { renderWithClient } from '@/test-utils';
import { ToastProvider } from '../Toast/Toast';

const api = vi.hoisted(() => ({
  getAccounts: vi.fn(),
  markPrReady: vi.fn(),
  getSnapshot: vi.fn(),
}));

vi.mock('@/api/client', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/api/client')>()),
  ...api,
}));

import { MergeCell } from './MergeCell';

const view = (row = pr('a', 5, { headSha: 'sha-5' }), onMerge = vi.fn(), launched = false) => {
  renderWithClient(
    <ToastProvider>
      <MergeCell pr={row} launched={launched} onMerge={onMerge} />
    </ToastProvider>,
  );
  return onMerge;
};

beforeEach(() => {
  Object.values(api).forEach((f) => f.mockReset());
  api.getAccounts.mockResolvedValue({
    github: { connected: true, login: 'octocat', method: 'pat' },
    jira: { connected: false, needsReconnect: false, sites: [], selectedSiteId: null },
  });
  api.markPrReady.mockResolvedValue({ prId: 'a', state: 'OPEN' });
  api.getSnapshot.mockResolvedValue({ prs: [] });
});

afterEach(() => vi.useRealTimers());

describe('MergeCell merge button', () => {
  it('is the Merge button of a pull request that can merge, and asks for the dialog', () => {
    const onMerge = view();
    const button = screen.getByRole('button', { name: 'Merge' });
    expect(button).toBeEnabled();
    expect(button).not.toHaveAttribute('aria-disabled');
    expect(screen.queryByText('Ready to merge')).toBeNull();
    fireEvent.click(button);
    expect(onMerge).toHaveBeenCalledTimes(1);
  });

  it('reads as disabled but can be focused, and gives the reason as its description', () => {
    const onMerge = view(pr('a', 5, { mergeable: 'CONFLICTING' }));
    const button = screen.getByRole('button', { name: 'Merge' });
    expect(button).toHaveAttribute('aria-disabled', 'true');
    expect(button).toHaveAccessibleDescription('Merge conflicts');
    expect(document.querySelector('[data-tip]')).toHaveAttribute('data-tip', 'Merge conflicts');
    button.focus();
    expect(button).toHaveFocus();
    fireEvent.click(button);
    expect(onMerge).not.toHaveBeenCalled();
  });

  it('shows the launch instead of the button once the merge was accepted', () => {
    view(pr('a', 5), vi.fn(), true);
    expect(screen.getByRole('status', { name: 'Merged' })).toBeInTheDocument();
    expect(screen.queryByRole('button')).toBeNull();
  });
});

describe('MergeCell in a repository the account can only read', () => {
  const repo = (role: 'READ' | 'TRIAGE' | 'WRITE' | undefined) => ({
    ...pr('a', 5).repo,
    nameWithOwner: 'ATP-MiNTo/PRDashboards',
    viewerPermission: role,
  });
  const reason = "Read-only here: your token's account can't write to ATP-MiNTo/PRDashboards";

  it.each(['READ', 'TRIAGE'] as const)('disables Merge for a %s role, with the reason', (role) => {
    const onMerge = view(pr('a', 5, { repo: repo(role) }));
    const button = screen.getByRole('button', { name: 'Merge' });
    expect(button).toHaveAttribute('aria-disabled', 'true');
    expect(button).toHaveAccessibleDescription(reason);
    expect(document.querySelector('[data-tip]')).toHaveAttribute('data-tip', reason);
    fireEvent.click(button);
    expect(onMerge).not.toHaveBeenCalled();
  });

  it('keeps Merge for a writer and for a snapshot without the role', () => {
    view(pr('a', 5, { repo: repo('WRITE') }));
    expect(screen.getByRole('button', { name: 'Merge' })).toBeEnabled();
  });

  it('disables Ready of a draft somebody else opened, and never calls the server', async () => {
    view(pr('a', 5, { isDraft: true, author: 'priya', repo: repo('READ') }));
    const button = await screen.findByRole('button', { name: 'Mark ready' });
    expect(button).toHaveAttribute('aria-disabled', 'true');
    expect(button).toHaveAccessibleDescription(reason);
    fireEvent.click(button);
    expect(api.markPrReady).not.toHaveBeenCalled();
  });

  it('lets the author of the draft mark it ready, by login or by role', async () => {
    view(pr('a', 5, { isDraft: true, author: 'OctoCat', repo: repo('READ') }));
    await waitFor(() => expect(api.getAccounts).toHaveBeenCalled());
    await waitFor(() => expect(screen.getByRole('button', { name: 'Mark ready' })).toBeEnabled());
    fireEvent.click(screen.getByRole('button', { name: 'Mark ready' }));
    await waitFor(() => expect(api.markPrReady).toHaveBeenCalledTimes(1));
  });
});

describe('MergeCell draft', () => {
  const draft = () => pr('a', 5, { isDraft: true, headSha: 'sha-5' });

  it('shows Ready for a draft, not Merge, with a tooltip', () => {
    view(draft());
    expect(screen.getByRole('button', { name: 'Mark ready' })).toHaveAccessibleDescription(
      'Draft. Mark ready for review to merge.',
    );
    expect(screen.queryByRole('button', { name: 'Merge' })).toBeNull();
  });

  it('marks it ready with the head the card showed, then frosts a disabled Merge for a moment', async () => {
    view(draft());
    fireEvent.click(screen.getByRole('button', { name: 'Mark ready' }));
    await waitFor(() =>
      expect(api.markPrReady).toHaveBeenCalledWith('a', { expectedHeadSha: 'sha-5' }),
    );
    expect(await screen.findByText('#5 is no longer a draft')).toBeInTheDocument();
    const cold = screen.getByRole('button', { name: 'Merge' });
    expect(cold).toHaveAttribute('aria-disabled', 'true');
    expect(cold).toHaveAccessibleDescription('Checks restart after leaving draft.');
    expect(cold.className).toMatch(/cold/);
  });

  it('ends the frost sweep after about a second, and goes back to Ready if the snapshot never catches up', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] });
    view(draft());
    fireEvent.click(screen.getByRole('button', { name: 'Mark ready' }));
    await act(() => vi.advanceTimersByTimeAsync(50));
    expect(screen.getByRole('button', { name: 'Merge' }).className).toMatch(/cold/);
    await act(() => vi.advanceTimersByTimeAsync(1400));
    const after = screen.getByRole('button', { name: 'Merge' });
    expect(after.className).not.toMatch(/cold/);
    expect(after).toHaveAttribute('aria-disabled', 'true');
    await act(() => vi.advanceTimersByTimeAsync(30_000));
    expect(screen.getByRole('button', { name: 'Mark ready' })).toBeInTheDocument();
  });

  it.each([
    [new ApiError(409, 'not_draft'), /not a draft/],
    [new ApiError(409, 'head_moved'), /card was refreshed/],
    [
      new ApiError(403, 'missing_permission', null, 'Needs pull_requests: write'),
      /pull_requests: write/,
    ],
  ])('says in a toast why marking ready failed: %#', async (error, text) => {
    api.markPrReady.mockRejectedValue(error);
    view(draft());
    fireEvent.click(screen.getByRole('button', { name: 'Mark ready' }));
    expect(await screen.findByText(text)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Mark ready' })).toBeEnabled();
  });

  it('disables Ready while the request is out', async () => {
    let finish: (v: unknown) => void = () => {};
    api.markPrReady.mockReturnValue(new Promise((r) => (finish = r)));
    view(draft());
    fireEvent.click(screen.getByRole('button', { name: 'Mark ready' }));
    await waitFor(() => expect(screen.getByRole('button', { name: 'Mark ready' })).toBeDisabled());
    expect(screen.getByRole('button', { name: 'Mark ready' })).toHaveAttribute('aria-busy', 'true');
    finish({ prId: 'a', state: 'OPEN' });
  });
});
