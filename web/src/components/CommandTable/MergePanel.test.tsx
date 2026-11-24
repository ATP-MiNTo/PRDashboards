import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError, type MergeResult } from '@/api/client';
import { pr, ticket } from '@/test-fixtures';
import { renderWithClient } from '@/test-utils';

const api = vi.hoisted(() => ({
  getSettings: vi.fn(),
  getTransitions: vi.fn(),
  mergePr: vi.fn(),
  retryTransition: vi.fn(),
  getSnapshot: vi.fn(),
}));

vi.mock('@/api/client', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/api/client')>()),
  ...api,
}));

import { MergePanel } from './MergePanel';

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

const choose = (name: string, option: string) => {
  fireEvent.click(screen.getByRole('combobox', { name }));
  fireEvent.click(screen.getByRole('option', { name: option }));
};

beforeEach(() => {
  api.getSettings.mockResolvedValue({
    polling: { githubSec: 60, jiraSec: 300 },
    theme: 'auto',
    defaultMethodForNewRepos: 'squash',
    deleteBranchAfterMerge: true,
    jiraSiteId: null,
  });
  api.getTransitions.mockImplementation((key: string) =>
    Promise.resolve({ key, transitions: [DONE, PROGRESS] }),
  );
  api.mergePr.mockResolvedValue(okResult());
  api.getSnapshot.mockResolvedValue({ prs: [] });
});

describe('MergePanel', () => {
  it('lists every linked ticket as "Don\'t move" and fetches no transitions', () => {
    renderWithClient(<MergePanel pr={withTickets()} method="squash" onClose={vi.fn()} />);
    const list = screen.getByRole('list', { name: 'Tickets' });
    expect(within(list).getAllByRole('listitem')).toHaveLength(2);
    expect(within(list).getAllByText("Don't move")).toHaveLength(2);
    expect(api.getTransitions).not.toHaveBeenCalled();
  });

  it('needs an explicit confirmation to merge without moving a ticket', async () => {
    renderWithClient(<MergePanel pr={withTickets()} method="merge" onClose={vi.fn()} />);
    await waitFor(() =>
      expect(screen.getByRole('checkbox', { name: 'Delete branch after merge' })).toBeChecked(),
    );
    const button = screen.getByRole('button', { name: 'Merge without ticket' });
    expect(button).toBeDisabled();
    expect(screen.getByText('Confirm that no ticket will be moved')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('checkbox', { name: 'Merge without moving any ticket' }));
    expect(button).toBeEnabled();
    fireEvent.click(button);
    await waitFor(() => expect(api.mergePr).toHaveBeenCalledTimes(1));
    const [id, key, body] = api.mergePr.mock.calls[0] as [string, string, unknown];
    expect(id).toBe('a');
    expect(key).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
    expect(body).toEqual({
      method: 'merge',
      expectedHeadSha: 'sha-7',
      deleteBranch: true,
      tickets: [
        { key: 'INP-1', transitionId: null, fields: {} },
        { key: 'INP-2', transitionId: null, fields: {} },
      ],
    });
  });

  it('loads the moves only of a ticket that is being moved and gates the button on its fields', async () => {
    renderWithClient(<MergePanel pr={withTickets()} method="squash" onClose={vi.fn()} />);
    fireEvent.click(screen.getByRole('button', { name: 'Move INP-1' }));
    await screen.findByRole('combobox', { name: 'Move INP-1' });
    expect(api.getTransitions).toHaveBeenCalledTimes(1);
    expect(api.getTransitions.mock.calls[0]?.[0]).toBe('INP-1');
    choose('Move INP-1', 'Move to Done');

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
    renderWithClient(<MergePanel pr={withTickets()} method="squash" onClose={vi.fn()} />);
    fireEvent.click(screen.getByRole('button', { name: 'Move INP-1' }));
    await screen.findByRole('combobox', { name: 'Move INP-1' });
    choose('Move INP-1', 'Move to Done');
    fireEvent.change(screen.getByLabelText('Resolution (required)'), {
      target: { value: '10000' },
    });
    fireEvent.click(screen.getByRole('checkbox', { name: 'Confirm moving INP-1 to Done' }));
    choose('Move INP-1', 'Move to In Progress');
    expect(screen.queryByLabelText('Resolution (required)')).toBeNull();
    expect(
      screen.getByRole('checkbox', { name: 'Confirm moving INP-1 to In Progress' }),
    ).not.toBeChecked();
    expect(screen.getByRole('button', { name: 'Merge & move to In Progress' })).toBeDisabled();
  });

  it('counts the tickets in the button when several are moved', async () => {
    renderWithClient(<MergePanel pr={withTickets()} method="squash" onClose={vi.fn()} />);
    for (const key of ['INP-1', 'INP-2']) {
      fireEvent.click(screen.getByRole('button', { name: `Move ${key}` }));
      await waitFor(() =>
        expect(screen.getByRole('combobox', { name: `Move ${key}` })).toBeInTheDocument(),
      );
      choose(`Move ${key}`, 'Move to In Progress');
    }
    expect(screen.getByRole('button', { name: 'Merge & update 2 tickets' })).toBeInTheDocument();
    expect(api.getTransitions).toHaveBeenCalledTimes(2);
  });

  it('follows the delete-branch setting and lets the person change it', async () => {
    renderWithClient(<MergePanel pr={pr('a', 7)} method="squash" onClose={vi.fn()} />);
    const box = screen.getByRole('checkbox', { name: 'Delete branch after merge' });
    await waitFor(() => expect(box).toBeChecked());
    fireEvent.click(box);
    expect(box).not.toBeChecked();
    fireEvent.click(screen.getByRole('checkbox', { name: 'Merge without moving any ticket' }));
    fireEvent.click(screen.getByRole('button', { name: 'Merge without ticket' }));
    await waitFor(() => expect(api.mergePr).toHaveBeenCalled());
    expect(api.mergePr.mock.calls[0]?.[2]).toMatchObject({ deleteBranch: false, tickets: [] });
  });

  it('shows why it cannot merge when the row is blocked', () => {
    renderWithClient(
      <MergePanel
        pr={pr('a', 7, { mergeable: 'CONFLICTING' })}
        method="squash"
        onClose={vi.fn()}
      />,
    );
    expect(screen.getByText('Merge conflicts')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('checkbox', { name: 'Merge without moving any ticket' }));
    expect(screen.getByRole('button', { name: 'Merge without ticket' })).toBeDisabled();
  });

  it('blocks the panel after the head moved until it is opened again', async () => {
    api.mergePr.mockRejectedValue(new ApiError(409, 'head_moved'));
    renderWithClient(<MergePanel pr={pr('a', 7)} method="squash" onClose={vi.fn()} />);
    fireEvent.click(screen.getByRole('checkbox', { name: 'Merge without moving any ticket' }));
    fireEvent.click(screen.getByRole('button', { name: 'Merge without ticket' }));
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'The pull request changed since the page loaded',
    );
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Merge without ticket' })).toBeDisabled(),
    );
    expect(
      screen.getByText('The pull request changed. Cancel and open this panel again.'),
    ).toBeInTheDocument();
  });

  it('uses a new key for the next attempt after a refusal', async () => {
    api.mergePr.mockRejectedValue(new ApiError(409, 'not_mergeable'));
    renderWithClient(<MergePanel pr={pr('a', 7)} method="squash" onClose={vi.fn()} />);
    fireEvent.click(screen.getByRole('checkbox', { name: 'Merge without moving any ticket' }));
    fireEvent.click(screen.getByRole('button', { name: 'Merge without ticket' }));
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'GitHub says this pull request cannot be merged right now',
    );
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Merge without ticket' })).toBeEnabled(),
    );
    fireEvent.click(screen.getByRole('button', { name: 'Merge without ticket' }));
    await waitFor(() => expect(api.mergePr).toHaveBeenCalledTimes(2));
    expect(api.mergePr.mock.calls[0]?.[1]).not.toBe(api.mergePr.mock.calls[1]?.[1]);
  });

  it('keeps the key after a server error so a repeat replays the same merge', async () => {
    api.mergePr.mockRejectedValue(new ApiError(502, 'upstream_error'));
    renderWithClient(<MergePanel pr={pr('a', 7)} method="squash" onClose={vi.fn()} />);
    fireEvent.click(screen.getByRole('checkbox', { name: 'Merge without moving any ticket' }));
    fireEvent.click(screen.getByRole('button', { name: 'Merge without ticket' }));
    await screen.findByRole('alert');
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Merge without ticket' })).toBeEnabled(),
    );
    fireEvent.click(screen.getByRole('button', { name: 'Merge without ticket' }));
    await waitFor(() => expect(api.mergePr).toHaveBeenCalledTimes(2));
    expect(api.mergePr.mock.calls[0]?.[1]).toBe(api.mergePr.mock.calls[1]?.[1]);
  });

  it('lists the steps after the merge and retries a failed move', async () => {
    api.mergePr.mockResolvedValue(
      okResult({
        steps: [
          {
            kind: 'merge',
            ticket: null,
            state: 'ok',
            message: null,
            toStatus: null,
            retryId: null,
          },
          {
            kind: 'delete_branch',
            ticket: null,
            state: 'ok',
            message: null,
            toStatus: null,
            retryId: null,
          },
          {
            kind: 'transition',
            ticket: 'INP-1',
            state: 'failed',
            message: 'Jira answered 503',
            toStatus: 'Done',
            retryId: 7,
          },
        ],
      }),
    );
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
    renderWithClient(<MergePanel pr={withTickets()} method="squash" onClose={vi.fn()} />);
    fireEvent.click(screen.getByRole('checkbox', { name: 'Merge without moving any ticket' }));
    fireEvent.click(screen.getByRole('button', { name: 'Merge without ticket' }));
    const steps = await screen.findByRole('list', { name: 'Merge steps' });
    expect(within(steps).getByText('Delete branch')).toBeInTheDocument();
    expect(within(steps).getByText('Move INP-1 to Done')).toBeInTheDocument();
    expect(within(steps).getByText('Jira answered 503')).toBeInTheDocument();
    fireEvent.click(within(steps).getByRole('button', { name: 'Retry Move INP-1 to Done' }));
    await waitFor(() => expect(api.retryTransition).toHaveBeenCalled());
    expect(api.retryTransition.mock.calls[0]?.[0]).toBe(7);
    await waitFor(() => expect(within(steps).queryByRole('button', { name: /Retry/ })).toBeNull());
    expect(within(steps).queryByText('Failed')).toBeNull();
  });

  it('does not offer a move for a ticket Jira does not know', () => {
    renderWithClient(
      <MergePanel
        pr={pr('a', 7, {
          tickets: [ticket('INP-9', { found: false, summary: null, status: null })],
        })}
        method="squash"
        onClose={vi.fn()}
      />,
    );
    expect(screen.getByText('Not found in Jira, it will not be moved')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Move INP-9' })).toBeNull();
  });
});
