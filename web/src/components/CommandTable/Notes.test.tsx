import { fireEvent, screen, within } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { pr, snapshot } from '@/test-fixtures';
import { renderWithClient } from '@/test-utils';

const api = vi.hoisted(() => ({
  getSnapshot: vi.fn(),
  getSettings: vi.fn(),
  getRepos: vi.fn(),
  getAccounts: vi.fn(),
  getRetries: vi.fn(),
  getActivity: vi.fn(),
  getSetup: vi.fn(),
}));

vi.mock('@/api/client', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/api/client')>()),
  ...api,
}));

class FakeEventSource {
  addEventListener() {}
  close() {}
}

import { CommandTable } from './CommandTable';

beforeEach(() => {
  Object.values(api).forEach((f) => f.mockReset());
  localStorage.clear();
  api.getSettings.mockResolvedValue({
    polling: { githubSec: 60, jiraSec: 300 },
    theme: 'auto',
    defaultMethodForNewRepos: 'squash',
    deleteBranchAfterMerge: false,
    jiraSiteId: null,
    excludedStatuses: ['Done'],
  });
  api.getSnapshot.mockResolvedValue(
    snapshot([pr('a', 1, { title: 'Add login' }), pr('b', 2, { title: 'Plain fix' })]),
  );
  api.getRepos.mockResolvedValue({ repos: [] });
  api.getRetries.mockResolvedValue({ retries: [] });
  api.getActivity.mockResolvedValue({ items: [], nextCursor: null });
  api.getAccounts.mockResolvedValue({
    github: { connected: false, login: null, method: null, warning: null },
    jira: { connected: false, needsReconnect: false, sites: [], selectedSiteId: null },
  });
  api.getSetup.mockResolvedValue({
    github: { configured: true, callbackUrl: '' },
    atlassian: { configured: true, callbackUrl: '' },
  });
  vi.stubGlobal('EventSource', FakeEventSource);
});

const card = (title: string) => screen.getByText(title).closest('article')!;

describe('Notes on pull requests', () => {
  it('adds a note, shows its preview and badge, and keeps it in this browser', async () => {
    renderWithClient(<CommandTable />);
    await screen.findByText('Add login');
    const own = within(card('Add login'));
    expect(own.queryByText('Note')).toBeNull();
    fireEvent.click(own.getByRole('button', { name: 'Add a note to #1' }));
    const dialog = screen.getByRole('dialog', { name: 'Note on acme/web #1' });
    expect(within(dialog).queryByRole('button', { name: 'Delete' })).toBeNull();
    fireEvent.change(within(dialog).getByLabelText(/^Note/), {
      target: {
        value: 'Ask Priya about the login flow, then merge after the release is cut on Friday',
      },
    });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }));
    expect(screen.queryByRole('dialog')).toBeNull();
    const edit = own.getByRole('button', { name: /^Edit the note on #1: Ask Priya/ });
    // The preview is the first 60 characters.
    expect(edit.closest('[data-tip]')).toHaveAttribute(
      'data-tip',
      'Ask Priya about the login flow, then merge after the release is'.slice(0, 60),
    );
    expect(own.getByText('Note')).toBeInTheDocument();
    expect(within(card('Plain fix')).queryByText('Note')).toBeNull();
    expect(JSON.parse(localStorage.getItem('prd-notes')!)).toEqual({
      a: 'Ask Priya about the login flow, then merge after the release is cut on Friday',
    });
  });

  it('edits and deletes a note, and Cancel keeps it', async () => {
    localStorage.setItem('prd-notes', JSON.stringify({ a: 'first' }));
    renderWithClient(<CommandTable />);
    await screen.findByText('Add login');
    const own = within(card('Add login'));
    fireEvent.click(own.getByRole('button', { name: 'Edit the note on #1: first' }));
    let dialog = screen.getByRole('dialog', { name: 'Note on acme/web #1' });
    expect(within(dialog).getByLabelText(/^Note/)).toHaveValue('first');
    fireEvent.change(within(dialog).getByLabelText(/^Note/), { target: { value: 'second' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }));
    expect(JSON.parse(localStorage.getItem('prd-notes')!)).toEqual({ a: 'first' });

    fireEvent.click(own.getByRole('button', { name: 'Edit the note on #1: first' }));
    dialog = screen.getByRole('dialog');
    fireEvent.change(within(dialog).getByLabelText(/^Note/), { target: { value: 'second' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }));
    expect(own.getByRole('button', { name: 'Edit the note on #1: second' })).toBeInTheDocument();

    fireEvent.click(own.getByRole('button', { name: 'Edit the note on #1: second' }));
    fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Delete' }));
    expect(own.getByRole('button', { name: 'Add a note to #1' })).toBeInTheDocument();
    expect(own.queryByText('Note')).toBeNull();
    expect(JSON.parse(localStorage.getItem('prd-notes')!)).toEqual({});
  });

  it('drops the note of a pull request that is gone from the snapshot on the next change', async () => {
    localStorage.setItem('prd-notes', JSON.stringify({ a: 'kept', gone: 'old' }));
    renderWithClient(<CommandTable />);
    await screen.findByText('Add login');
    fireEvent.click(within(card('Plain fix')).getByRole('button', { name: 'Add a note to #2' }));
    fireEvent.change(within(screen.getByRole('dialog')).getByLabelText(/^Note/), {
      target: { value: 'b' },
    });
    fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Save' }));
    expect(JSON.parse(localStorage.getItem('prd-notes')!)).toEqual({ a: 'kept', b: 'b' });
  });

  it('works while storage is blocked', async () => {
    const get = vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('blocked');
    });
    const set = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('blocked');
    });
    renderWithClient(<CommandTable />);
    await screen.findByText('Add login');
    const own = within(card('Add login'));
    fireEvent.click(own.getByRole('button', { name: 'Add a note to #1' }));
    fireEvent.change(within(screen.getByRole('dialog')).getByLabelText(/^Note/), {
      target: { value: 'works' },
    });
    fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Save' }));
    expect(own.getByRole('button', { name: 'Edit the note on #1: works' })).toBeInTheDocument();
    get.mockRestore();
    set.mockRestore();
  });
});
