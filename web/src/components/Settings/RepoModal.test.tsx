import { act, fireEvent, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { RepoSettings } from '@/api/client';
import { pr, snapshot } from '@/test-fixtures';
import { renderWithClient } from '@/test-utils';

const api = vi.hoisted(() => ({
  putRepo: vi.fn(),
  getRepos: vi.fn(),
  getSnapshot: vi.fn(),
  getSettings: vi.fn(),
  getAccounts: vi.fn(),
}));

vi.mock('@/api/client', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/api/client')>()),
  ...api,
}));

import { REPO_SAVE_MS, RepoModal } from './RepoModal';

const base: RepoSettings = {
  id: 'R1',
  nameWithOwner: 'acme/web',
  isNew: false,
  openPrs: 2,
  approvalsRequired: 0,
  defaultMethod: 'squash',
  inheritMethod: true,
  deleteBranch: 'default',
  branchRules: [
    { pattern: 'main', method: 'merge' },
    { pattern: 'production', method: 'merge' },
  ],
};

const settings = {
  polling: { githubSec: 60, jiraSec: 300 },
  theme: 'auto',
  defaultMethodForNewRepos: 'squash',
  deleteBranchAfterMerge: false,
  multiPrTickets: 'all',
  jiraSiteId: null,
  excludedStatuses: ['Done'],
};

beforeEach(() => {
  api.putRepo.mockImplementation((_id: string, u: unknown) => Promise.resolve(u));
  api.getRepos.mockResolvedValue({ repos: [base] });
  api.getSnapshot.mockResolvedValue(
    snapshot([
      pr('a', 1, { baseRef: 'main', headRef: 'feat/x' }),
      pr('b', 2, {
        baseRef: 'release/1',
        headRef: 'feat/y',
        repo: { ...pr('b', 2).repo, id: 'R9' },
      }),
    ]),
  );
  api.getSettings.mockResolvedValue(settings);
  api.getAccounts.mockResolvedValue({
    github: { connected: true, login: 'me', method: 'pat', needsReconnect: false },
    jira: { connected: false, sites: [], selectedSiteId: null, needsReconnect: false },
  });
});

afterEach(() => vi.useRealTimers());

async function open(over: Partial<RepoSettings> = {}, onClose = vi.fn()) {
  renderWithClient(<RepoModal repo={{ ...base, ...over }} onClose={onClose} />);
  await screen.findByRole('dialog', { name: 'acme/web' });
  // The two rolls name the default once the settings are read.
  await screen.findByRole('button', {
    name: /^Delete branch after merge, now (Use default \(No\)|Yes|No)/,
  });
  await waitFor(() => expect(api.getSettings).toHaveBeenCalled());
  await act(async () => {});
  return onClose;
}

describe('RepoModal', () => {
  it('shows the current choices', async () => {
    await open();
    expect(
      screen.getByRole('button', { name: /^Merge method, now Use default \(Squash\)/ }),
    ).toBeInTheDocument();
    expect(screen.getByRole('spinbutton', { name: 'Approvals required' })).toHaveValue('0');
    expect(screen.getByLabelText('Branch 1')).toHaveValue('main');
    expect(screen.getByLabelText('Method 2')).toHaveValue('merge');
  });

  it('saves a change on its own after a pause, as one request', async () => {
    await open();
    vi.useFakeTimers({ shouldAdvanceTime: true });
    // The default is No, so a click goes to Yes and the next one to No.
    fireEvent.click(screen.getByRole('button', { name: /^Delete branch after merge, now/ }));
    fireEvent.click(screen.getByRole('button', { name: /^Delete branch after merge, now/ }));
    expect(api.putRepo).not.toHaveBeenCalled();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(REPO_SAVE_MS + 10);
    });
    expect(api.putRepo).toHaveBeenCalledTimes(1);
    expect(api.putRepo).toHaveBeenCalledWith('R1', {
      defaultMethod: 'squash',
      inheritMethod: true,
      deleteBranch: 'no',
      approvalsRequired: 0,
      branchRules: base.branchRules,
      clearNew: true,
    });
  });

  it('opens on the default for a repository that was never customised', async () => {
    await open();
    expect(
      screen.getByRole('button', { name: /^Delete branch after merge, now Use default \(No\)/ }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole('button', { name: /^Merge method, now Use default \(Squash\)/ }),
    ).toBeInTheDocument();
  });

  it('opens on the stored explicit choices', async () => {
    renderWithClient(
      <RepoModal
        repo={{ ...base, inheritMethod: false, defaultMethod: 'rebase', deleteBranch: 'yes' }}
        onClose={vi.fn()}
      />,
    );
    expect(
      await screen.findByRole('button', { name: /^Delete branch after merge, now Yes/ }),
    ).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /^Merge method, now Rebase/ })).toBeInTheDocument();
  });

  it('turns the merge method with a click through the allowed methods, never landing on the default', async () => {
    await open();
    const roll = () => screen.getByRole('button', { name: /^Merge method, now/ });
    // From the default (Squash) a click goes to the method after Squash.
    fireEvent.click(roll());
    expect(roll()).toHaveAccessibleName(/now Merge commit/);
    fireEvent.click(roll());
    expect(roll()).toHaveAccessibleName(/now Rebase/);
    fireEvent.click(roll());
    expect(roll()).toHaveAccessibleName(/now Squash/);
    fireEvent.click(roll());
    expect(roll()).toHaveAccessibleName(/now Merge commit/);
  });

  it('turns the delete-branch roll between Yes and No with a click', async () => {
    await open();
    const roll = () => screen.getByRole('button', { name: /^Delete branch after merge, now/ });
    fireEvent.click(roll());
    expect(roll()).toHaveAccessibleName(/now Yes/);
    fireEvent.click(roll());
    expect(roll()).toHaveAccessibleName(/now No/);
    fireEvent.click(roll());
    expect(roll()).toHaveAccessibleName(/now Yes/);
  });

  it('switches the default off and on with a right-click, Shift+click or Shift+Enter', async () => {
    await open();
    const roll = () => screen.getByRole('button', { name: /^Merge method, now/ });
    // Off: the explicit value that is the default now. On again from an explicit value.
    expect(fireEvent.contextMenu(roll())).toBe(false);
    expect(roll()).toHaveAccessibleName(/now Squash/);
    fireEvent.contextMenu(roll());
    expect(roll()).toHaveAccessibleName(/now Use default \(Squash\)/);
    fireEvent.click(roll(), { shiftKey: true });
    expect(roll()).toHaveAccessibleName(/now Squash/);
    fireEvent.click(roll()); // Merge commit
    fireEvent.click(roll(), { shiftKey: true });
    expect(roll()).toHaveAccessibleName(/now Use default/);
    // The method that was chosen last is the one that comes back.
    fireEvent.contextMenu(roll());
    expect(roll()).toHaveAccessibleName(/now Merge commit/);
  });

  it('brings back the stored own method and the last delete-branch value when the default is switched off', async () => {
    renderWithClient(
      <RepoModal
        repo={{ ...base, inheritMethod: true, defaultMethod: 'rebase', deleteBranch: 'default' }}
        onClose={vi.fn()}
      />,
    );
    const method = await screen.findByRole('button', { name: /^Merge method, now/ });
    fireEvent.contextMenu(method);
    expect(method).toHaveAccessibleName(/now Rebase/);
    const del = await screen.findByRole('button', { name: /^Delete branch after merge, now/ });
    fireEvent.click(del);
    expect(del).toHaveAccessibleName(/now Yes/);
    fireEvent.contextMenu(del);
    expect(del).toHaveAccessibleName(/now Use default \(No\)/);
    fireEvent.contextMenu(del);
    expect(del).toHaveAccessibleName(/now Yes/);
  });

  it('saves the default as the follow flag and an explicit choice as the value', async () => {
    await open({ inheritMethod: false, defaultMethod: 'merge', deleteBranch: 'yes' });
    vi.useFakeTimers({ shouldAdvanceTime: true });
    fireEvent.contextMenu(screen.getByRole('button', { name: /^Merge method, now/ }));
    fireEvent.contextMenu(screen.getByRole('button', { name: /^Delete branch after merge, now/ }));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(REPO_SAVE_MS + 10);
    });
    expect(api.putRepo).toHaveBeenCalledTimes(1);
    expect(api.putRepo).toHaveBeenCalledWith('R1', {
      defaultMethod: 'merge',
      inheritMethod: true,
      deleteBranch: 'default',
      approvalsRequired: 0,
      branchRules: base.branchRules,
      clearNew: true,
    });
  });

  it('explains the gestures on both rolls', async () => {
    await open();
    for (const name of [/^Delete branch after merge, now/, /^Merge method, now/]) {
      expect(screen.getByRole('button', { name })).toHaveAttribute(
        'title',
        'Click: next choice. Right-click, Shift+click or Shift+Enter: use default or choose',
      );
    }
  });

  it('only turns through the methods the repository allows', async () => {
    const locked = pr('a', 1, { baseRef: 'main', headRef: 'feat/x' });
    locked.repo = { ...locked.repo, squashAllowed: false };
    api.getSnapshot.mockResolvedValue(snapshot([locked]));
    await open();
    await waitFor(() => expect(api.getSnapshot).toHaveBeenCalled());
    await act(async () => {});
    const roll = () => screen.getByRole('button', { name: /^Merge method, now/ });
    // The default is Squash, which the repository does not allow: the turn is Merge commit, Rebase.
    fireEvent.click(roll());
    expect(roll()).toHaveAccessibleName(/now Merge commit/);
    fireEvent.click(roll());
    expect(roll()).toHaveAccessibleName(/now Rebase/);
    fireEvent.click(roll());
    expect(roll()).toHaveAccessibleName(/now Merge commit/);
  });

  it('steps the approvals from 0, which requires none, up to 10', async () => {
    await open();
    const field = screen.getByRole('spinbutton', { name: 'Approvals required' });
    expect(field).toHaveAttribute('aria-valuemin', '0');
    expect(field).toHaveAttribute('aria-valuemax', '10');
    expect(screen.getByRole('button', { name: 'Fewer approvals' })).toBeDisabled();
    fireEvent.keyDown(field, { key: 'ArrowDown' });
    expect(field).toHaveValue('0');
    fireEvent.click(screen.getByRole('button', { name: 'More approvals' }));
    expect(field).toHaveValue('1');
    expect(screen.getByRole('button', { name: 'Fewer approvals' })).toBeEnabled();
    fireEvent.click(screen.getByRole('button', { name: 'Fewer approvals' }));
    expect(field).toHaveValue('0');
  });

  it('steps the approvals with the buttons and the arrow keys, within 0 and 10', async () => {
    await open({ approvalsRequired: 10 });
    const field = screen.getByRole('spinbutton', { name: 'Approvals required' });
    expect(screen.getByRole('button', { name: 'More approvals' })).toBeDisabled();
    fireEvent.click(screen.getByRole('button', { name: 'Fewer approvals' }));
    expect(field).toHaveValue('9');
    fireEvent.keyDown(field, { key: 'ArrowUp' });
    expect(field).toHaveValue('10');
    fireEvent.keyDown(field, { key: 'ArrowDown' });
    fireEvent.keyDown(field, { key: 'ArrowDown' });
    expect(field).toHaveValue('8');
  });

  it('steps the approvals with the wheel without scrolling the dialog', async () => {
    await open({ approvalsRequired: 3 });
    const field = screen.getByRole('spinbutton', { name: 'Approvals required' });
    const up = new WheelEvent('wheel', { deltaY: -10, cancelable: true, bubbles: true });
    field.dispatchEvent(up);
    expect(up.defaultPrevented).toBe(true);
    await waitFor(() => expect(field).toHaveValue('4'));
  });

  it('adds, edits and removes a branch rule, and offers the known branches', async () => {
    await open();
    fireEvent.click(screen.getByRole('button', { name: 'Add branch rule' }));
    const pattern = screen.getByLabelText('Branch 3');
    const list = document.getElementById(pattern.getAttribute('list')!)!;
    // Only the branches of this repository.
    expect([...list.querySelectorAll('option')].map((o) => o.getAttribute('value'))).toEqual([
      'feat/x',
      'main',
    ]);
    fireEvent.change(pattern, { target: { value: 'release/*' } });
    fireEvent.change(screen.getByLabelText('Method 3'), { target: { value: 'rebase' } });
    fireEvent.click(screen.getByRole('button', { name: 'Remove branch rule 1' }));
    expect(screen.getByLabelText('Branch 1')).toHaveValue('production');
    expect(screen.getByLabelText('Branch 2')).toHaveValue('release/*');
  });

  it('keeps an invalid pattern from being saved until it is fixed', async () => {
    await open();
    vi.useFakeTimers({ shouldAdvanceTime: true });
    fireEvent.change(screen.getByLabelText('Branch 1'), { target: { value: 'a b(' } });
    expect(screen.getByLabelText('Branch 1')).toHaveAttribute('aria-invalid', 'true');
    expect(screen.getByRole('alert')).toHaveTextContent('Patterns use letters');
    await act(async () => {
      await vi.advanceTimersByTimeAsync(REPO_SAVE_MS + 10);
    });
    expect(api.putRepo).not.toHaveBeenCalled();
    fireEvent.change(screen.getByLabelText('Branch 1'), { target: { value: 'develop' } });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(REPO_SAVE_MS + 10);
    });
    expect(api.putRepo).toHaveBeenCalledTimes(1);
  });

  it('stops at 50 rules', async () => {
    await open({
      branchRules: Array.from({ length: 50 }, (_, i) => ({
        pattern: `b${i}`,
        method: 'merge' as const,
      })),
    });
    expect(screen.getByRole('button', { name: 'Add branch rule' })).toBeDisabled();
  });

  it('sends a change that is waiting when it closes, with the invalid rows left out', async () => {
    const onClose = await open();
    fireEvent.change(screen.getByLabelText('Branch 1'), { target: { value: 'a b(' } });
    fireEvent.click(screen.getByRole('button', { name: 'Done' }));
    expect(onClose).toHaveBeenCalled();
    await waitFor(() => expect(api.putRepo).toHaveBeenCalledTimes(1));
    expect(api.putRepo.mock.calls[0]?.[1]).toMatchObject({
      branchRules: [{ pattern: 'production', method: 'merge' }],
    });
  });

  it('saves nothing when it closes unchanged, except to confirm a new repository', async () => {
    await open();
    fireEvent.click(screen.getByRole('button', { name: 'Done' }));
    expect(api.putRepo).not.toHaveBeenCalled();
  });

  it('confirms a new repository when it closes', async () => {
    renderWithClient(<RepoModal repo={{ ...base, isNew: true }} onClose={vi.fn()} />);
    expect(await screen.findByText(/New repository\./)).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Done' }));
    await waitFor(() =>
      expect(api.putRepo).toHaveBeenCalledWith('R1', expect.objectContaining({ clearNew: true })),
    );
  });

  it('says so when the save fails', async () => {
    api.putRepo.mockRejectedValue(new Error('x'));
    await open();
    fireEvent.click(screen.getByRole('button', { name: 'Add branch rule' }));
    fireEvent.change(screen.getByLabelText('Branch 3'), { target: { value: 'dev' } });
    expect(
      await screen.findByText('Could not save the repository.', {}, { timeout: 2000 }),
    ).toBeInTheDocument();
  });
});
