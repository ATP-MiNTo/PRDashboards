import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError, type MergeResult } from '@/api/client';
import { pr, ticket } from '@/test-fixtures';
import { renderWithClient } from '@/test-utils';

const api = vi.hoisted(() => ({
  getSettings: vi.fn(),
  getTransitions: vi.fn(),
  mergePr: vi.fn(),
}));

vi.mock('@/api/client', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/api/client')>()),
  ...api,
}));

import { MergeDialog } from './MergeDialog';

const DONE = {
  id: '31',
  name: 'Done',
  toStatus: 'Done',
  toCategory: 'done',
  fields: [
    {
      id: 'resolution',
      name: 'Resolution',
      required: true,
      type: 'resolution',
      allowedValues: [
        { id: '10000', name: 'Fixed' },
        { id: '10001', name: "Won't Do" },
      ],
    },
  ],
};
const PROGRESS = {
  id: '21',
  name: 'Start',
  toStatus: 'In Progress',
  toCategory: 'indeterminate',
  fields: [],
};

const withTickets = () =>
  pr('a', 7, { headSha: 'sha-7', tickets: [ticket('INP-1'), ticket('INP-2')] });

const okResult = (over: Partial<MergeResult> = {}): MergeResult => ({
  prId: 'a',
  method: 'squash',
  sha: 'merged',
  steps: [
    { kind: 'merge', ticket: null, state: 'ok', message: null, toStatus: null, retryId: null },
  ],
  ...over,
});

const show = (row = pr('a', 7), onMerged = vi.fn(), onClose = vi.fn()) => {
  renderWithClient(<MergeDialog pr={row} onClose={onClose} onMerged={onMerged} />);
  return { onMerged, onClose };
};
const dialog = () => screen.getByRole('dialog', { name: /^Merge acme\/web #7\?$/ });
const withoutTicket = () =>
  fireEvent.click(screen.getByRole('checkbox', { name: 'Merge without moving any ticket' }));
const mergeWithout = () =>
  fireEvent.click(screen.getByRole('button', { name: 'Merge without ticket' }));
const choose = async (key: string, status: string) => {
  fireEvent.click(screen.getByRole('button', { name: `Move ${key}` }));
  fireEvent.click(await screen.findByRole('menuitem', { name: status }));
};

beforeEach(() => {
  Object.values(api).forEach((f) => f.mockReset());
  api.getSettings.mockResolvedValue({
    polling: { githubSec: 60, jiraSec: 300 },
    theme: 'auto',
    defaultMethodForNewRepos: 'squash',
    deleteBranchAfterMerge: true,
    multiPrTickets: 'all',
    jiraSiteId: null,
  });
  api.getTransitions.mockImplementation((key: string) =>
    Promise.resolve({ key, transitions: [DONE, PROGRESS] }),
  );
  api.mergePr.mockResolvedValue(okResult());
});

describe('MergeDialog', () => {
  it('is a modal dialog titled with the repository and number, the pull request title below', () => {
    show(pr('a', 7, { title: 'Fix the thing' }));
    const d = dialog();
    expect(d).toHaveAttribute('aria-modal', 'true');
    expect(within(d).getByRole('heading', { name: 'Merge acme/web #7?' })).toBeInTheDocument();
    expect(within(d).getByText('Fix the thing')).toBeInTheDocument();
    expect(within(d).getByText('Head commit is checked again before merging')).toBeInTheDocument();
  });

  it('closes with Cancel and with Escape', () => {
    const { onClose } = show();
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(onClose).toHaveBeenCalledTimes(1);
    fireEvent.keyDown(dialog(), { key: 'Escape' });
    expect(onClose).toHaveBeenCalledTimes(2);
  });

  it('shows the resolved method and where it comes from', () => {
    show(
      pr('a', 7, {
        mergeMethod: {
          method: 'merge',
          default: 'squash',
          source: 'rule: main',
          allowed: ['merge', 'rebase'],
          warning: 'Repo disallows squash merge, using merge commit',
        },
      }),
    );
    expect(screen.getByRole('button', { name: 'Merge method' })).toHaveTextContent('merge commit');
    expect(screen.getByText(/from branch rule main/)).toBeInTheDocument();
    expect(screen.getByText('Repo disallows squash merge, using merge commit')).toBeInTheDocument();
  });

  it('lets the person pick an allowed method for this merge only, and sends it', async () => {
    show(
      pr('a', 7, {
        mergeMethod: {
          method: 'squash',
          default: 'squash',
          source: 'repo default',
          allowed: ['squash', 'rebase'],
          warning: null,
        },
      }),
    );
    fireEvent.click(screen.getByRole('button', { name: 'Merge method' }));
    expect(screen.getByRole('menuitem', { name: 'Merge commit (not allowed)' })).toHaveAttribute(
      'aria-disabled',
      'true',
    );
    expect(screen.getByRole('menuitem', { name: 'Squash (default)' })).toBeInTheDocument();
    fireEvent.click(screen.getByRole('menuitem', { name: 'Rebase' }));
    expect(screen.getByRole('button', { name: 'Merge method' })).toHaveTextContent('rebase');
    expect(screen.getByText(/chosen for this merge/)).toBeInTheDocument();
    withoutTicket();
    mergeWithout();
    await waitFor(() => expect(api.mergePr).toHaveBeenCalledTimes(1));
    expect(api.mergePr.mock.calls[0]?.[2]).toMatchObject({ method: 'rebase' });
  });

  it('needs an explicit confirmation to merge without moving a ticket', async () => {
    const { onMerged } = show(withTickets());
    await waitFor(() =>
      expect(screen.getByRole('checkbox', { name: 'Delete branch after merge' })).toBeChecked(),
    );
    const button = screen.getByRole('button', { name: 'Merge without ticket' });
    expect(button).toBeDisabled();
    expect(screen.getByText('Confirm that no ticket will be moved')).toBeInTheDocument();
    expect(api.getTransitions).not.toHaveBeenCalled();
    withoutTicket();
    expect(button).toBeEnabled();
    fireEvent.click(button);
    await waitFor(() => expect(api.mergePr).toHaveBeenCalledTimes(1));
    const [id, key, body] = api.mergePr.mock.calls[0] as [string, string, unknown];
    expect(id).toBe('a');
    expect(key).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
    expect(body).toEqual({
      method: 'squash',
      expectedHeadSha: 'sha-7',
      deleteBranch: true,
      tickets: [
        { key: 'INP-1', transitionId: null, fields: {} },
        { key: 'INP-2', transitionId: null, fields: {} },
      ],
    });
    await waitFor(() => expect(onMerged).toHaveBeenCalledWith(okResult()));
  });

  it('says there is no ticket to move for a pull request without one', () => {
    show(pr('a', 7));
    expect(screen.getByText('No Jira ticket to move')).toBeInTheDocument();
  });

  it('loads the moves only of a ticket whose menu was opened and gates the button on its fields', async () => {
    show(withTickets());
    expect(api.getTransitions).not.toHaveBeenCalled();
    await choose('INP-1', 'Done');
    expect(api.getTransitions).toHaveBeenCalledTimes(1);
    expect(api.getTransitions.mock.calls[0]?.[0]).toBe('INP-1');
    expect(screen.getByRole('button', { name: 'Move INP-1' })).toHaveTextContent('Done');

    const button = screen.getByRole('button', { name: 'Merge & move to Done' });
    expect(button).toBeDisabled();
    expect(screen.getByText('INP-1: Resolution is required')).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText('Resolution (required)'), {
      target: { value: '10000' },
    });
    expect(screen.getByText('Confirm the move of INP-1')).toBeInTheDocument();
    expect(button).toBeDisabled();
    fireEvent.click(screen.getByRole('checkbox', { name: 'Confirm moving INP-1 to Done' }));
    expect(button).toBeEnabled();

    fireEvent.click(button);
    await waitFor(() => expect(api.mergePr).toHaveBeenCalledTimes(1));
    expect(api.mergePr.mock.calls[0]?.[2]).toMatchObject({
      tickets: [
        { key: 'INP-1', transitionId: '31', fields: { resolution: '10000' } },
        { key: 'INP-2', transitionId: null, fields: {} },
      ],
    });
  });

  it('clears the fields and the confirmation when the move changes', async () => {
    show(withTickets());
    await choose('INP-1', 'Done');
    fireEvent.change(screen.getByLabelText('Resolution (required)'), {
      target: { value: '10000' },
    });
    fireEvent.click(screen.getByRole('checkbox', { name: 'Confirm moving INP-1 to Done' }));
    await choose('INP-1', 'In Progress');
    expect(screen.queryByLabelText('Resolution (required)')).toBeNull();
    expect(
      screen.getByRole('checkbox', { name: 'Confirm moving INP-1 to In Progress' }),
    ).not.toBeChecked();
    expect(screen.getByRole('button', { name: 'Merge & move to In Progress' })).toBeDisabled();
  });

  it('counts the tickets in the button when several are moved', async () => {
    show(withTickets());
    await choose('INP-1', 'In Progress');
    await choose('INP-2', 'In Progress');
    expect(screen.getByRole('button', { name: 'Merge & update 2 tickets' })).toBeInTheDocument();
    expect(api.getTransitions).toHaveBeenCalledTimes(2);
  });

  it('can leave a moved ticket where it is again', async () => {
    show(withTickets());
    await choose('INP-1', 'In Progress');
    await choose('INP-1', 'Leave as In Review');
    expect(screen.getByRole('button', { name: 'Merge without ticket' })).toBeDisabled();
    expect(screen.getByText(/INP-1 stays in/)).toBeInTheDocument();
  });

  it('follows the delete-branch setting and lets the person change it', async () => {
    show(pr('a', 7));
    const box = screen.getByRole('checkbox', { name: 'Delete branch after merge' });
    await waitFor(() => expect(box).toBeChecked());
    expect(screen.getByText('Branch is deleted after merge')).toBeInTheDocument();
    fireEvent.click(box);
    expect(box).not.toBeChecked();
    expect(screen.getByText('Branch is kept after merge')).toBeInTheDocument();
    withoutTicket();
    mergeWithout();
    await waitFor(() => expect(api.mergePr).toHaveBeenCalled());
    expect(api.mergePr.mock.calls[0]?.[2]).toMatchObject({ deleteBranch: false, tickets: [] });
  });

  it('shows why it cannot merge when the card is blocked', () => {
    show(pr('a', 7, { mergeable: 'CONFLICTING' }));
    expect(screen.getByText('Merge conflicts')).toBeInTheDocument();
    withoutTicket();
    expect(screen.getByRole('button', { name: 'Merge without ticket' })).toBeDisabled();
  });

  it('notes the stack a pull request is part of', () => {
    show(
      pr('a', 7, {
        stack: { id: 'x', position: 2, size: 3, parentId: 'y', parentNumber: 6 },
      }),
    );
    expect(screen.getByText(/Part of a stack, 2 of 3/)).toBeInTheDocument();
  });

  it('blocks the dialog after the head moved until it is opened again', async () => {
    api.mergePr.mockRejectedValue(new ApiError(409, 'head_moved'));
    show();
    withoutTicket();
    mergeWithout();
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'The pull request changed since the page loaded',
    );
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Merge without ticket' })).toBeDisabled(),
    );
    expect(
      screen.getByText('The pull request changed. Cancel and open this dialog again.'),
    ).toBeInTheDocument();
  });

  it('uses a new key for the next attempt after a refusal', async () => {
    api.mergePr.mockRejectedValue(new ApiError(409, 'not_mergeable'));
    show();
    withoutTicket();
    mergeWithout();
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'GitHub says this pull request cannot be merged right now',
    );
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Merge without ticket' })).toBeEnabled(),
    );
    mergeWithout();
    await waitFor(() => expect(api.mergePr).toHaveBeenCalledTimes(2));
    expect(api.mergePr.mock.calls[0]?.[1]).not.toBe(api.mergePr.mock.calls[1]?.[1]);
  });

  it('keeps the key after a server error so a repeat replays the same merge', async () => {
    api.mergePr.mockRejectedValue(new ApiError(502, 'upstream_error'));
    show();
    withoutTicket();
    mergeWithout();
    await screen.findByRole('alert');
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Merge without ticket' })).toBeEnabled(),
    );
    mergeWithout();
    await waitFor(() => expect(api.mergePr).toHaveBeenCalledTimes(2));
    expect(api.mergePr.mock.calls[0]?.[1]).toBe(api.mergePr.mock.calls[1]?.[1]);
  });

  it('shows the server detail when the token lacks a permission, or GitHub did not merge', async () => {
    api.mergePr.mockRejectedValue(
      new ApiError(
        403,
        'missing_permission',
        null,
        'The token needs the GitHub permission contents: write',
      ),
    );
    show();
    withoutTicket();
    mergeWithout();
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'The token needs the GitHub permission contents: write',
    );
    api.mergePr.mockRejectedValue(
      new ApiError(409, 'merge_failed', null, 'GitHub did not merge: 2 approvals are required'),
    );
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Merge without ticket' })).toBeEnabled(),
    );
    mergeWithout();
    await waitFor(() =>
      expect(screen.getByRole('alert')).toHaveTextContent(
        'GitHub did not merge: 2 approvals are required',
      ),
    );
  });

  it('says it is merging, and cannot be closed meanwhile', async () => {
    let finish: (r: MergeResult) => void = () => {};
    api.mergePr.mockReturnValue(new Promise<MergeResult>((r) => (finish = r)));
    const { onClose } = show();
    withoutTicket();
    mergeWithout();
    const busy = await screen.findByRole('button', { name: 'Merging' });
    expect(busy).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Cancel' })).toBeDisabled();
    fireEvent.keyDown(dialog(), { key: 'Escape' });
    expect(onClose).not.toHaveBeenCalled();
    finish(okResult());
  });

  it('does not offer a move for a ticket Jira does not know', () => {
    show(pr('a', 7, { tickets: [ticket('INP-9', { found: false, summary: null, status: null })] }));
    expect(
      screen.getByText('Jira INP-9: Not found in Jira, it will not be moved'),
    ).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Move INP-9' })).toBeNull();
  });

  describe('with a status sync rule', () => {
    const READY = {
      id: '41',
      name: 'Ready',
      toStatus: 'Ready to Merge',
      toCategory: 'indeterminate',
      fields: [],
    };
    const plan = (waitingOn: { prId: string; number: number; repo: string }[]) => ({
      rule: 'Done on merge',
      target: 'ready to merge',
      total: 3,
      waitingOn,
    });

    it('preselects the transition of the rule and still asks for a confirmation', async () => {
      api.getTransitions.mockResolvedValue({ key: 'INP-1', transitions: [DONE, READY] });
      show(pr('a', 7, { tickets: [ticket('INP-1', { mergePlan: plan([]) })] }));
      expect(
        await screen.findByRole('button', { name: 'Merge & move to Ready to Merge' }),
      ).toBeDisabled();
      expect(screen.getByRole('button', { name: 'Move INP-1' })).toHaveTextContent(
        'Ready to Merge',
      );
      expect(screen.getByText('Confirm the move of INP-1')).toBeInTheDocument();
    });

    it('says why a rule waiting on other PRs does not move yet, and the menu can still force it', async () => {
      api.getTransitions.mockResolvedValue({ key: 'INP-1', transitions: [DONE, READY] });
      const waiting = plan([
        { prId: 'x', number: 34, repo: 'acme/fe' },
        { prId: 'y', number: 56, repo: 'acme/proxy' },
      ]);
      show(pr('a', 7, { tickets: [ticket('INP-1', { mergePlan: waiting })] }));
      expect(
        await screen.findByText("Won't move yet: waiting on fe #34, proxy #56"),
      ).toBeInTheDocument();
      expect(screen.getByRole('button', { name: 'Move INP-1' })).toHaveTextContent('In Review');
      await choose('INP-1', 'Ready to Merge');
      expect(screen.getByRole('button', { name: 'Merge & move to Ready to Merge' })).toBeDisabled();
    });
  });
});
