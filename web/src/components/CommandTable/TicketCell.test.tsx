import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import type { ReactElement } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from '@/api/client';
import { pr, ticket } from '@/test-fixtures';
import { renderWithClient } from '@/test-utils';
import { ToastProvider } from '../Toast/Toast';
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

const view = (ui: ReactElement) => renderWithClient(<ToastProvider>{ui}</ToastProvider>);
const plus = () => screen.getByRole('button', { name: 'Link one of my tickets' });
const menu = () => screen.findByRole('menu', { name: 'My tickets' });

describe('TicketCell badges', () => {
  it('shows "KEY · Status" as a link to Jira over https, with the summary and source as the tooltip', () => {
    view(
      <TicketCell
        pr={pr('a', 1, {
          tickets: [ticket('INP-1'), ticket('INP-2', { source: 'title' })],
        })}
      />,
    );
    const link = screen.getByRole('link', { name: 'INP-1 · In Review' });
    expect(link).toHaveAttribute('href', 'https://acme.atlassian.net/browse/INP-1');
    expect(link).toHaveAttribute('rel', 'noopener noreferrer');
    expect(link).toHaveAttribute('title', 'Summary of INP-1, from branch');
    expect(screen.getByRole('link', { name: 'INP-2 · In Review' })).toHaveAttribute(
      'title',
      'Summary of INP-2, from title',
    );
  });

  it('says "No Jira link" without a ticket, and shows what Jira did not answer for a key', () => {
    const { unmount } = view(<TicketCell pr={pr('a', 1)} />);
    expect(screen.getByText('No Jira link')).toBeInTheDocument();
    unmount();
    view(
      <TicketCell
        pr={pr('a', 1, {
          tickets: [ticket('INP-9', { found: false, summary: null, status: null, url: null })],
        })}
      />,
    );
    expect(screen.getByText('INP-9 · Not found in Jira')).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: /INP-9/ })).toBeNull();
  });

  it('does not link a ticket whose URL is not https', () => {
    view(
      <TicketCell
        pr={pr('a', 1, { tickets: [ticket('INP-1', { url: 'javascript:alert(1)' })] })}
      />,
    );
    expect(screen.queryByRole('link')).toBeNull();
    expect(screen.getByText('INP-1 · In Review')).toBeInTheDocument();
  });

  it('says the ticket was not checked when Jira is not connected', () => {
    view(
      <TicketCell
        pr={pr('a', 1, {
          ticketLookup: 'unverified',
          tickets: [ticket('INP-1', { found: false, summary: null, status: null, url: null })],
        })}
      />,
    );
    expect(screen.getByText('INP-1 · Not checked in Jira')).toBeInTheDocument();
  });

  it('offers a retry when Jira was unreachable', async () => {
    view(
      <TicketCell pr={pr('a', 1, { ticketLookup: 'unreachable', tickets: [ticket('INP-1')] })} />,
    );
    expect(screen.getByRole('alert')).toHaveTextContent('Jira unreachable, retry the lookup');
    fireEvent.click(screen.getByRole('button', { name: 'Retry lookup' }));
    await waitFor(() => expect(api.syncNow).toHaveBeenCalledTimes(1));
  });

  it('shows what a ticket with an All rule still waits on', () => {
    const waiting = {
      rule: 'r1',
      total: 3,
      waitingOn: [
        { prId: 'a', number: 34, repo: 'acme/FE' },
        { prId: 'b', number: 56, repo: 'acme/proxy' },
      ],
    };
    view(<TicketCell pr={pr('a', 1, { tickets: [ticket('INP-1', { waiting })] })} />);
    expect(screen.getByText('INP-1: Waiting on 2 of 3 PRs: FE #34, proxy #56')).toBeInTheDocument();
  });
});

describe('TicketCell "My tickets" menu', () => {
  it('reads the assigned tickets only when the menu opens', async () => {
    view(<TicketCell pr={pr('a', 1)} />);
    expect(api.getAssignedTickets).not.toHaveBeenCalled();
    expect(plus()).toHaveAttribute('aria-haspopup', 'menu');
    fireEvent.click(plus());
    expect(await menu()).toBeInTheDocument();
    await waitFor(() => expect(api.getAssignedTickets).toHaveBeenCalledTimes(1));
  });

  it('lists the tickets that are not linked yet and links the one that is chosen', async () => {
    view(<TicketCell pr={pr('a', 1, { tickets: [ticket('INP-1')] })} />);
    fireEvent.click(plus());
    const list = await menu();
    const item = await within(list).findByRole('menuitem', { name: /OPS-9/ });
    expect(within(list).queryByRole('menuitem', { name: /Add login/ })).toBeNull();
    fireEvent.click(item);
    await waitFor(() =>
      expect(api.putPrTickets).toHaveBeenCalledWith('a', {
        manual: ['OPS-9'],
        removed: [],
      }),
    );
    expect(screen.queryByRole('menu')).toBeNull();
  });

  it('brings back a ticket that was unlinked when it is linked again', async () => {
    view(<TicketCell pr={pr('a', 1, { removedKeys: ['OPS-9'] })} />);
    fireEvent.click(plus());
    fireEvent.click(await screen.findByRole('menuitem', { name: /OPS-9/ }));
    await waitFor(() =>
      expect(api.putPrTickets).toHaveBeenCalledWith('a', { manual: ['OPS-9'], removed: [] }),
    );
  });

  it('shows a search box for a longer list, and says when nothing matches', async () => {
    api.getAssignedTickets.mockResolvedValue({
      tickets: Array.from({ length: 7 }, (_, i) => ({
        key: `OPS-${i + 1}`,
        summary: `Task ${i + 1}`,
        status: 'To Do',
        statusCategory: 'new',
        project: 'OPS',
        issueType: 'Task',
      })),
    });
    view(<TicketCell pr={pr('a', 1)} />);
    fireEvent.click(plus());
    const search = await screen.findByRole('searchbox', { name: 'Search your tickets' });
    expect(
      screen.getAllByRole('menuitem').filter((i) => /OPS-/.test(i.textContent ?? '')),
    ).toHaveLength(7);
    fireEvent.change(search, { target: { value: 'task 3' } });
    expect(screen.getAllByRole('menuitem')).toHaveLength(1);
    fireEvent.change(search, { target: { value: 'nothing' } });
    expect(screen.getByText('No matching tickets')).toBeInTheDocument();
  });

  it('says so when no assigned ticket is left to link', async () => {
    view(<TicketCell pr={pr('a', 1, { tickets: [ticket('INP-1'), ticket('OPS-9')] })} />);
    fireEvent.click(plus());
    expect(await screen.findByText('No more tickets assigned to you')).toBeInTheDocument();
  });

  it('says Jira is not connected, or that the list could not be loaded', async () => {
    api.getAssignedTickets.mockRejectedValue(new ApiError(409, 'not_connected'));
    const { unmount } = view(<TicketCell pr={pr('a', 1)} />);
    fireEvent.click(plus());
    expect(await screen.findByText('Jira is not connected.')).toBeInTheDocument();
    unmount();
    api.getAssignedTickets.mockRejectedValue(new Error('down'));
    view(<TicketCell pr={pr('a', 1)} />);
    fireEvent.click(plus());
    expect(await screen.findByText('Could not load your tickets.')).toBeInTheDocument();
  });

  it('unlinks an automatic ticket by hiding it, keeping the manual ones', async () => {
    view(
      <TicketCell
        pr={pr('a', 1, {
          tickets: [ticket('INP-1'), ticket('OPS-9', { source: 'manual' })],
          removedKeys: ['INP-5'],
        })}
      />,
    );
    fireEvent.click(plus());
    fireEvent.click(await screen.findByRole('menuitem', { name: /Unlink INP-1/ }));
    await waitFor(() =>
      expect(api.putPrTickets).toHaveBeenCalledWith('a', {
        manual: ['OPS-9'],
        removed: ['INP-5', 'INP-1'],
      }),
    );
  });

  it('unlinks a manual ticket by dropping it', async () => {
    view(<TicketCell pr={pr('a', 1, { tickets: [ticket('OPS-9', { source: 'manual' })] })} />);
    fireEvent.click(plus());
    fireEvent.click(await screen.findByRole('menuitem', { name: /Unlink OPS-9/ }));
    await waitFor(() =>
      expect(api.putPrTickets).toHaveBeenCalledWith('a', { manual: [], removed: [] }),
    );
  });

  it('has no "On this pull request" section without linked tickets', async () => {
    view(<TicketCell pr={pr('a', 1)} />);
    fireEvent.click(plus());
    await menu();
    expect(screen.queryByText('On this pull request')).toBeNull();
  });

  it('shows a toast when the change could not be saved', async () => {
    api.putPrTickets.mockRejectedValue(new Error('422'));
    view(<TicketCell pr={pr('a', 1, { tickets: [ticket('INP-1')] })} />);
    fireEvent.click(plus());
    fireEvent.click(await screen.findByRole('menuitem', { name: /Unlink INP-1/ }));
    expect(await screen.findByText('Could not save the change.')).toBeInTheDocument();
  });

  it('closes with Escape and gives the focus back to the plus button', async () => {
    view(<TicketCell pr={pr('a', 1)} />);
    fireEvent.click(plus());
    const list = await menu();
    fireEvent.keyDown(list, { key: 'Escape' });
    expect(screen.queryByRole('menu')).toBeNull();
    expect(plus()).toHaveFocus();
  });
});
