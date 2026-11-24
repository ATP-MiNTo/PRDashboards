import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { pr, ticket } from '@/test-fixtures';
import { renderWithClient } from '@/test-utils';
import { TicketCell } from './TicketCell';

const api = vi.hoisted(() => ({
  putPrTickets: vi.fn(),
  getAssignedTickets: vi.fn(),
  syncNow: vi.fn(),
}));

vi.mock('@/api/client', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/api/client')>()),
  ...api,
}));

beforeEach(() => {
  Object.values(api).forEach((f) => f.mockReset());
  api.putPrTickets.mockResolvedValue(undefined);
  api.syncNow.mockResolvedValue({ running: true });
  api.getAssignedTickets.mockResolvedValue({
    tickets: [
      {
        key: 'OPS-9',
        summary: 'Rotate keys',
        status: 'To Do',
        statusCategory: 'new',
        project: 'OPS',
        issueType: 'Task',
      },
      {
        key: 'INP-1',
        summary: 'Add login',
        status: 'In Review',
        statusCategory: 'indeterminate',
        project: 'INP',
        issueType: 'Story',
      },
    ],
  });
});

describe('TicketCell', () => {
  it('shows key, status, title and source, and links to Jira over https', () => {
    renderWithClient(
      <TicketCell
        pr={pr('a', 1, {
          tickets: [ticket('INP-1'), ticket('INP-2', { source: 'title' })],
        })}
      />,
    );
    const link = screen.getByRole('link', { name: 'INP-1' });
    expect(link).toHaveAttribute('href', 'https://acme.atlassian.net/browse/INP-1');
    expect(link).toHaveAttribute('rel', 'noopener noreferrer');
    expect(screen.getByText('Summary of INP-1')).toBeInTheDocument();
    expect(screen.getAllByText('In Review')).toHaveLength(2);
    expect(screen.getByText('from branch')).toBeInTheDocument();
    expect(screen.getByText('from title')).toBeInTheDocument();
  });

  it('says so when there is no ticket or Jira does not know the key', () => {
    const { unmount } = renderWithClient(<TicketCell pr={pr('a', 1)} />);
    expect(screen.getByText('No ticket')).toBeInTheDocument();
    unmount();
    renderWithClient(
      <TicketCell
        pr={pr('a', 1, {
          tickets: [ticket('INP-9', { found: false, summary: null, status: null, url: null })],
        })}
      />,
    );
    expect(screen.getByText('Not found in Jira')).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'INP-9' })).toBeNull();
  });

  it('unlinks an automatic ticket by hiding it, keeping the manual ones', async () => {
    renderWithClient(
      <TicketCell
        pr={pr('a', 1, {
          tickets: [ticket('INP-1'), ticket('OPS-9', { source: 'manual' })],
          removedKeys: ['INP-5'],
        })}
      />,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Unlink INP-1' }));
    await waitFor(() =>
      expect(api.putPrTickets).toHaveBeenCalledWith('a', {
        manual: ['OPS-9'],
        removed: ['INP-5', 'INP-1'],
      }),
    );
  });

  it('unlinks a manual ticket by dropping it', async () => {
    renderWithClient(
      <TicketCell pr={pr('a', 1, { tickets: [ticket('OPS-9', { source: 'manual' })] })} />,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Unlink OPS-9' }));
    await waitFor(() =>
      expect(api.putPrTickets).toHaveBeenCalledWith('a', { manual: [], removed: [] }),
    );
  });

  it('collapses to two tickets and expands with "+N more"', async () => {
    renderWithClient(
      <TicketCell
        pr={pr('a', 1, {
          tickets: [ticket('INP-1'), ticket('INP-2'), ticket('INP-3'), ticket('INP-4')],
        })}
      />,
    );
    expect(screen.queryByText('INP-3')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: '+2 more' }));
    expect(screen.getByText('INP-3')).toBeInTheDocument();
    expect(screen.getByText('INP-4')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Show less' }));
    expect(screen.queryByText('INP-3')).toBeNull();
  });

  it('links an assigned ticket from a searchable picker, leaving out the ones already linked', async () => {
    renderWithClient(<TicketCell pr={pr('a', 1, { tickets: [ticket('INP-1')] })} />);
    fireEvent.click(screen.getByRole('button', { name: 'Link ticket' }));
    const list = await screen.findByRole('list', { name: 'Assigned tickets' });
    await waitFor(() => expect(within(list).getAllByRole('listitem')).toHaveLength(1));
    expect(within(list).queryByText('INP-1')).toBeNull();

    fireEvent.change(screen.getByRole('searchbox', { name: 'Search your tickets' }), {
      target: { value: 'nothing' },
    });
    expect(screen.getByText('No matching tickets')).toBeInTheDocument();
    fireEvent.change(screen.getByRole('searchbox', { name: 'Search your tickets' }), {
      target: { value: '' },
    });
    fireEvent.change(screen.getByRole('searchbox', { name: 'Search your tickets' }), {
      target: { value: 'rotate' },
    });
    fireEvent.click(screen.getByRole('button', { name: /OPS-9/ }));
    await waitFor(() =>
      expect(api.putPrTickets).toHaveBeenCalledWith('a', { manual: ['OPS-9'], removed: [] }),
    );
  });

  it('shows a picker error and closes the picker', async () => {
    api.getAssignedTickets.mockRejectedValue(new Error('down'));
    renderWithClient(<TicketCell pr={pr('a', 1)} />);
    fireEvent.click(screen.getByRole('button', { name: 'Link ticket' }));
    expect(await screen.findByText('Could not load your tickets.')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Close' }));
    expect(screen.getByRole('button', { name: 'Link ticket' })).toBeInTheDocument();
  });

  it('offers a retry when Jira was unreachable', async () => {
    renderWithClient(
      <TicketCell pr={pr('a', 1, { ticketLookup: 'unreachable', tickets: [ticket('INP-1')] })} />,
    );
    expect(screen.getByRole('alert')).toHaveTextContent('Jira unreachable, retry the lookup');
    fireEvent.click(screen.getByRole('button', { name: 'Retry lookup' }));
    await waitFor(() => expect(api.syncNow).toHaveBeenCalledTimes(1));
  });

  it('says the ticket was not checked when Jira is not connected', () => {
    renderWithClient(
      <TicketCell
        pr={pr('a', 1, {
          ticketLookup: 'unverified',
          tickets: [ticket('INP-1', { found: false, summary: null, status: null, url: null })],
        })}
      />,
    );
    expect(screen.getByText('Not checked in Jira')).toBeInTheDocument();
  });

  it('shows an error when the change could not be saved', async () => {
    api.putPrTickets.mockRejectedValue(new Error('422'));
    renderWithClient(<TicketCell pr={pr('a', 1, { tickets: [ticket('INP-1')] })} />);
    fireEvent.click(screen.getByRole('button', { name: 'Unlink INP-1' }));
    expect(await screen.findByText('Could not save the change.')).toBeInTheDocument();
  });
});
