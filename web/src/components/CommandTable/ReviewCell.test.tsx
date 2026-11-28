import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from '@/api/client';
import { pr } from '@/test-fixtures';
import { renderWithClient } from '@/test-utils';
import { ToastProvider } from '../Toast/Toast';

const api = vi.hoisted(() => ({
  getAccounts: vi.fn(),
  approvePr: vi.fn(),
  requestChangesPr: vi.fn(),
  getSnapshot: vi.fn(),
}));

vi.mock('@/api/client', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/api/client')>()),
  ...api,
}));

import { ReviewCell } from './ReviewCell';

const result = { prId: 'a', state: 'APPROVED' };
const mine = () =>
  pr('a', 5, {
    roles: ['assigned'],
    author: 'bob',
    headSha: 'sha-5',
    approvals: { have: 0, need: 1, changesRequested: 0 },
  });

const view = (row = mine()) =>
  renderWithClient(
    <ToastProvider>
      <ReviewCell pr={row} />
    </ToastProvider>,
  );
const action = () => screen.findByRole('button', { name: 'Review action for #5' });

beforeEach(() => {
  Object.values(api).forEach((f) => f.mockReset());
  api.getAccounts.mockResolvedValue({
    github: { connected: true, login: 'octocat', method: 'pat' },
    jira: { connected: false, needsReconnect: false, sites: [], selectedSiteId: null },
  });
  api.getSnapshot.mockResolvedValue({ prs: [] });
  api.approvePr.mockResolvedValue(result);
  api.requestChangesPr.mockResolvedValue({ prId: 'a', state: 'CHANGES_REQUESTED' });
});

describe('ReviewCell state', () => {
  it.each([
    [{ have: 0, need: 1, changesRequested: 0 }, 'Waiting'],
    [{ have: 0, need: 2, changesRequested: 0 }, 'Waiting 0/2'],
    [{ have: 1, need: 2, changesRequested: 0 }, 'Approve 1/2'],
    [{ have: 1, need: 1, changesRequested: 0 }, 'Approve'],
    [{ have: 0, need: 2, changesRequested: 1 }, 'Change 1/2'],
  ] as const)('shows %j as "%s" for a pull request the viewer opened', (approvals, label) => {
    view(pr('a', 5, { roles: ['authored'], approvals }));
    expect(screen.getByText(label)).toBeInTheDocument();
    expect(screen.queryByRole('button')).toBeNull();
  });

  it('puts the reason and the approvals in the tooltip, and reads them out', () => {
    view(pr('a', 5, { roles: ['authored'], approvals: { have: 1, need: 2, changesRequested: 0 } }));
    const tip = document.querySelector('[data-tip]');
    expect(tip).toHaveAttribute(
      'data-tip',
      '1 of 2 reviewers approved. This repo needs 2 approvals to merge. You opened this pull request, so you cannot review it.',
    );
    expect(
      screen.getByText(/You opened this pull request, so you cannot review it\./),
    ).toBeInTheDocument();
  });

  it('shows the state, not the menu, for a draft', () => {
    view(pr('a', 5, { roles: ['assigned'], author: 'bob', isDraft: true }));
    expect(screen.queryByRole('button')).toBeNull();
    expect(document.querySelector('[data-tip]')?.getAttribute('data-tip')).toMatch(
      /A draft is reviewed/,
    );
  });

  it('shows the state once the viewer approved', async () => {
    view(
      pr('a', 5, {
        roles: ['assigned'],
        author: 'bob',
        reviews: [
          { id: 'r', state: 'APPROVED', submittedAt: '2026-01-01T00:00:00Z', author: 'octocat' },
        ],
      }),
    );
    await waitFor(() =>
      expect(document.querySelector('[data-tip]')?.getAttribute('data-tip')).toMatch(
        /^You approved\. /,
      ),
    );
    expect(screen.queryByRole('button')).toBeNull();
  });
});

describe('ReviewCell action menu', () => {
  it('offers Action with Approve and Request change, described by the review state', async () => {
    view();
    const button = await action();
    expect(button).toHaveTextContent('Action');
    expect(button).toHaveAttribute('aria-haspopup', 'menu');
    expect(button).toHaveAccessibleDescription(/Waiting on 1 reviewer/);
    fireEvent.click(button);
    const menu = screen.getByRole('menu', { name: 'Review #5' });
    expect(
      within(menu)
        .getAllByRole('menuitem')
        .map((i) => i.textContent),
    ).toEqual(['Approve', 'Request changes']);
  });

  it('approves with the head the card showed and says so in a toast', async () => {
    view();
    fireEvent.click(await action());
    fireEvent.click(screen.getByRole('menuitem', { name: 'Approve' }));
    await waitFor(() =>
      expect(api.approvePr).toHaveBeenCalledWith('a', { expectedHeadSha: 'sha-5' }),
    );
    expect(await screen.findByText('You approved #5')).toBeInTheDocument();
    expect(screen.queryByRole('menu')).toBeNull();
  });

  it.each([
    [new ApiError(409, 'head_moved'), /card was refreshed/],
    [new ApiError(409, 'not_open'), /no longer open/],
    [new ApiError(422, 'own_pull_request'), /You opened this pull request/],
    [
      new ApiError(403, 'missing_permission', null, 'The token needs pull_requests: write'),
      /pull_requests: write/,
    ],
    [
      new ApiError(422, 'review_rejected', null, 'Cannot approve your own PR'),
      /Cannot approve your own PR/,
    ],
    [new ApiError(429, 'rate_limited'), /Too many requests/],
    [new ApiError(502, 'upstream_error'), /could not be reached/],
    [new Error('network'), /Something went wrong/],
  ])('says in a toast why an approval failed: %#', async (error, text) => {
    api.approvePr.mockRejectedValue(error);
    view();
    fireEvent.click(await action());
    fireEvent.click(screen.getByRole('menuitem', { name: 'Approve' }));
    expect(await screen.findByText(text)).toBeInTheDocument();
  });

  it('opens the request dialog and sends nothing before the comment is written', async () => {
    view();
    fireEvent.click(await action());
    fireEvent.click(screen.getByRole('menuitem', { name: 'Request changes' }));
    const dialog = screen.getByRole('dialog', { name: 'Request changes on acme/web #5' });
    expect(within(dialog).getByText('Change 5')).toBeInTheDocument();
    fireEvent.click(within(dialog).getByRole('button', { name: 'Request changes' }));
    expect(within(dialog).getByRole('alert')).toHaveTextContent('Write a comment first');
    expect(within(dialog).getByRole('textbox', { name: /Comment/ })).toHaveFocus();
    fireEvent.change(within(dialog).getByRole('textbox', { name: /Comment/ }), {
      target: { value: '   ' },
    });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Request changes' }));
    expect(api.requestChangesPr).not.toHaveBeenCalled();
  });

  it('sends the comment with the head, closes and toasts', async () => {
    view();
    fireEvent.click(await action());
    fireEvent.click(screen.getByRole('menuitem', { name: 'Request changes' }));
    const dialog = screen.getByRole('dialog');
    fireEvent.change(within(dialog).getByRole('textbox', { name: /Comment/ }), {
      target: { value: 'Please add a test' },
    });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Request changes' }));
    await waitFor(() =>
      expect(api.requestChangesPr).toHaveBeenCalledWith('a', {
        expectedHeadSha: 'sha-5',
        body: 'Please add a test',
      }),
    );
    expect(await screen.findByText('You requested changes on #5')).toBeInTheDocument();
    expect(screen.queryByRole('dialog')).toBeNull();
  });

  it('keeps the dialog and the comment when GitHub refuses, and says why inline', async () => {
    api.requestChangesPr.mockRejectedValue(new ApiError(409, 'head_moved'));
    view();
    fireEvent.click(await action());
    fireEvent.click(screen.getByRole('menuitem', { name: 'Request changes' }));
    const dialog = screen.getByRole('dialog');
    const box = within(dialog).getByRole('textbox', { name: /Comment/ });
    fireEvent.change(box, { target: { value: 'Please add a test' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Request changes' }));
    expect(await within(dialog).findByRole('alert')).toHaveTextContent('card was refreshed');
    expect(box).toHaveValue('Please add a test');
    await waitFor(() =>
      expect(within(dialog).getByRole('button', { name: 'Request changes' })).toBeEnabled(),
    );
  });

  it('closes the dialog with Cancel and with Escape, and gives the focus back', async () => {
    view();
    fireEvent.click(await action());
    fireEvent.click(screen.getByRole('menuitem', { name: 'Request changes' }));
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(screen.queryByRole('dialog')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Review action for #5' }));
    fireEvent.click(screen.getByRole('menuitem', { name: 'Request changes' }));
    fireEvent.keyDown(screen.getByRole('dialog'), { key: 'Escape' });
    expect(screen.queryByRole('dialog')).toBeNull();
    expect(api.requestChangesPr).not.toHaveBeenCalled();
  });
});
