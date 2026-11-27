import { QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { MergeResult } from '@/api/client';
import { makeQueryClient } from '@/app/providers';
import { keys, type RecentMerge } from '@/api/queries';
import { pr, ticket } from '@/test-fixtures';

const api = vi.hoisted(() => ({ retryTransition: vi.fn() }));
vi.mock('@/api/client', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/api/client')>()),
  ...api,
}));

import { HistoryView } from './HistoryView';

const result = (steps: MergeResult['steps']): MergeResult => ({ steps }) as MergeResult;

function merge(id: string, number: number, steps: MergeResult['steps'], at: number): RecentMerge {
  return {
    prId: id,
    pr: pr(id, number, {
      title: `Title ${number}`,
      tickets: [ticket('INP-1'), ticket('INP-2', { status: 'In Review' })],
    }),
    result: result(steps),
    at,
  };
}

const STEPS: MergeResult['steps'] = [
  { kind: 'merge', ticket: null, toStatus: null, state: 'ok', message: null, retryId: null },
  {
    kind: 'transition',
    ticket: 'INP-1',
    toStatus: 'Done',
    state: 'ok',
    message: null,
    retryId: null,
  },
];

function show(merges: RecentMerge[], terms: string[] = []) {
  const client = makeQueryClient();
  client.setQueryData(keys.merges, merges);
  return render(
    <QueryClientProvider client={client}>
      <HistoryView terms={terms} />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  api.retryTransition.mockReset();
  vi.useFakeTimers({ toFake: ['Date'] });
  vi.setSystemTime(new Date('2026-11-24T12:00:00Z'));
});

describe('HistoryView', () => {
  it('says nothing was merged yet', () => {
    show([]);
    const view = screen.getByRole('region', { name: 'Merged pull requests' });
    expect(within(view).getByText(/Nothing merged here yet/)).toBeInTheDocument();
    expect(within(view).getByLabelText('0 merged')).toBeInTheDocument();
  });

  it('lists a merge with its link, where it is from, the Jira outcome, the diff and when', () => {
    show([merge('a', 7, STEPS, Date.parse('2026-11-24T11:55:00Z'))]);
    const view = screen.getByRole('region', { name: 'Merged pull requests' });
    expect(within(view).getByRole('link', { name: 'Title 7' })).toHaveAttribute(
      'href',
      'https://github.com/acme/web/pull/7',
    );
    expect(within(view).getByText('acme/web #7 · priya')).toBeInTheDocument();
    expect(within(view).getByText('INP-1 moved to Done')).toBeInTheDocument();
    expect(within(view).getByText('INP-2 left in In Review')).toBeInTheDocument();
    expect(within(view).getByText('+12')).toBeInTheDocument();
    expect(within(view).getByText('Merged 5m ago')).toBeInTheDocument();
    expect(within(view).getByLabelText('1 merged')).toBeInTheDocument();
  });

  it('filters with the search terms and says when nothing matches', () => {
    show(
      [
        merge('a', 7, STEPS, Date.parse('2026-11-24T11:55:00Z')),
        merge('b', 8, STEPS, Date.parse('2026-11-24T11:00:00Z')),
      ],
      ['title 8'],
    );
    expect(screen.queryByText('Title 7')).toBeNull();
    expect(screen.getByText('Title 8')).toBeInTheDocument();
    expect(screen.getByLabelText('1 merged')).toBeInTheDocument();
  });

  it('says when the search matches nothing', () => {
    show([merge('a', 7, STEPS, Date.now())], ['zzz']);
    expect(screen.getByText('Nothing merged matches the search.')).toBeInTheDocument();
  });

  it('marks a merge that was only queued, and offers to retry a failed move', async () => {
    api.retryTransition.mockResolvedValue({ step: STEPS[1] });
    show([
      merge(
        'a',
        7,
        [
          {
            kind: 'merge',
            ticket: null,
            toStatus: null,
            state: 'queued',
            message: null,
            retryId: null,
          },
          {
            kind: 'transition',
            ticket: 'INP-1',
            toStatus: 'Done',
            state: 'failed',
            message: 'x',
            retryId: 9,
          },
          {
            kind: 'delete_branch',
            ticket: null,
            toStatus: null,
            state: 'failed',
            message: 'x',
            retryId: null,
          },
        ],
        Date.now(),
      ),
    ]);
    expect(screen.getByText('Queued just now')).toBeInTheDocument();
    expect(screen.getByText('INP-1 did not move')).toBeInTheDocument();
    expect(screen.getByText('Branch not deleted')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Retry the move of INP-1' }));
    await waitFor(() => expect(api.retryTransition).toHaveBeenCalledWith(9, expect.anything()));
  });

  it('says when a merge had no Jira link', () => {
    const m = merge('a', 7, [STEPS[0]!], Date.now());
    m.pr = { ...m.pr, tickets: [] };
    show([m]);
    expect(screen.getByText('No Jira link')).toBeInTheDocument();
  });
});
