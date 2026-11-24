import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { SyncRule } from '@/api/client';
import { newRule } from '@/lib/rules';
import { pr, snapshot, ticket } from '@/test-fixtures';
import { renderWithClient } from '@/test-utils';

const api = vi.hoisted(() => ({
  getRules: vi.fn(),
  putRules: vi.fn(),
  previewRule: vi.fn(),
  getSnapshot: vi.fn(),
  getActivity: vi.fn(),
  getRetries: vi.fn(),
}));

vi.mock('@/api/client', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/api/client')>()),
  ...api,
}));

import { RulesSection } from './RulesSection';

const rule = (id: string, over: Partial<SyncRule> = {}): SyncRule => ({
  ...newRule(),
  id,
  trigger: 'review_approved',
  target: 'Ready to Merge',
  ...over,
});

const saved = () => api.putRules.mock.calls.at(-1)?.[0] as { rules: SyncRule[]; baseline: boolean };

beforeEach(() => {
  Object.values(api).forEach((f) => f.mockReset());
  api.getRules.mockResolvedValue({
    rules: [rule('a', { name: 'First' }), rule('b', { name: 'Second' })],
  });
  api.putRules.mockImplementation((u: { rules: SyncRule[] }) =>
    Promise.resolve({ rules: u.rules }),
  );
  api.previewRule.mockResolvedValue({
    moves: [
      { ticketKey: 'INP-1', status: 'In Review', alreadyInTarget: false },
      { ticketKey: 'INP-2', status: 'Ready to Merge', alreadyInTarget: true },
    ],
    waiting: [],
  });
  api.getSnapshot.mockResolvedValue(snapshot([pr('a', 1, { tickets: [ticket('INP-1')] })]));
  api.getActivity.mockResolvedValue({ items: [], nextCursor: null });
  api.getRetries.mockResolvedValue({ retries: [] });
});

describe('RulesSection', () => {
  it('lists the rules in order with their mode', async () => {
    renderWithClient(<RulesSection />);
    await screen.findByRole('checkbox', { name: 'Enable First' });
    const items = within(screen.getByRole('list', { name: 'Status sync rules' })).getAllByRole(
      'listitem',
    );
    expect(items).toHaveLength(2);
    expect(items[0]).toHaveTextContent('First');
    expect(items[1]).toHaveTextContent('Second');
    expect(items[0]).toHaveTextContent('Mode All');
  });

  it('moves a rule with the keyboard buttons and saves the whole list at once', async () => {
    renderWithClient(<RulesSection />);
    fireEvent.click(await screen.findByRole('button', { name: 'Move First down' }));
    await waitFor(() => expect(api.putRules).toHaveBeenCalledTimes(1));
    expect(saved().rules.map((r) => r.id)).toEqual(['b', 'a']);
    expect(saved().baseline).toBe(false);
    expect(screen.getByRole('button', { name: 'Move Second up' })).toBeDisabled();
  });

  it('reorders by dragging', async () => {
    renderWithClient(<RulesSection />);
    await screen.findByRole('checkbox', { name: 'Enable First' });
    const items = within(screen.getByRole('list', { name: 'Status sync rules' })).getAllByRole(
      'listitem',
    );
    const data = { setData: vi.fn(), effectAllowed: '' };
    fireEvent.dragStart(items[0]!, { dataTransfer: data });
    fireEvent.dragOver(items[1]!, { dataTransfer: data });
    fireEvent.drop(items[1]!, { dataTransfer: data });
    await waitFor(() => expect(saved().rules.map((r) => r.id)).toEqual(['b', 'a']));
  });

  it('deletes and disables without a preview', async () => {
    renderWithClient(<RulesSection />);
    fireEvent.click(await screen.findByRole('checkbox', { name: 'Enable First' }));
    await waitFor(() => expect(saved().rules[0]?.enabled).toBe(false));
    fireEvent.click(screen.getByRole('button', { name: 'Delete Second' }));
    await waitFor(() => expect(saved().rules.map((r) => r.id)).toEqual(['a']));
    expect(api.previewRule).not.toHaveBeenCalled();
  });

  it('adds a rule only after a preview and keeps existing tickets in place when asked', async () => {
    renderWithClient(<RulesSection />);
    await screen.findByRole('checkbox', { name: 'Enable First' });
    fireEvent.click(screen.getByRole('button', { name: 'Add rule' }));
    const form = screen.getByRole('form', { name: 'New rule' });
    const save = within(form).getByRole('button', { name: 'Save rule' });
    expect(save).toBeDisabled();
    fireEvent.change(within(form).getByLabelText('Move the ticket to'), {
      target: { value: 'Done' },
    });
    fireEvent.change(within(form).getByLabelText('When'), { target: { value: 'ci_passing' } });
    expect(save).toBeDisabled();
    fireEvent.click(within(form).getByRole('button', { name: 'Preview' }));
    expect(await within(form).findByText('1 ticket would move now:')).toBeInTheDocument();
    expect(within(form).getByText(/Already in the target status: INP-2/)).toBeInTheDocument();
    expect(save).toBeEnabled();
    fireEvent.click(within(form).getByRole('radio', { name: "Don't move existing tickets" }));
    fireEvent.click(save);
    await waitFor(() => expect(api.putRules).toHaveBeenCalledTimes(1));
    expect(saved().baseline).toBe(true);
    expect(saved().rules).toHaveLength(3);
    expect(saved().rules[2]).toMatchObject({
      trigger: 'ci_passing',
      target: 'Done',
      enabled: true,
      mode: 'all',
    });
    await waitFor(() => expect(screen.queryByRole('form')).toBeNull());
  });

  it('asks for hours on a stale-commit rule and offers the statuses it has seen', async () => {
    renderWithClient(<RulesSection />);
    await screen.findByRole('checkbox', { name: 'Enable First' });
    fireEvent.click(screen.getByRole('button', { name: 'Add rule' }));
    const form = screen.getByRole('form', { name: 'New rule' });
    fireEvent.change(within(form).getByLabelText('When'), { target: { value: 'commit_stale' } });
    fireEvent.change(within(form).getByLabelText('Move the ticket to'), {
      target: { value: 'Blocked' },
    });
    expect(within(form).getByRole('alert')).toHaveTextContent(
      'Hours must be a whole number from 1 to 720.',
    );
    expect(within(form).getByRole('button', { name: 'Preview' })).toBeDisabled();
    fireEvent.change(within(form).getByLabelText('Hours'), { target: { value: '48' } });
    expect(within(form).getByRole('button', { name: 'Preview' })).toBeEnabled();
    await waitFor(() =>
      expect(form.querySelector('datalist option[value="In Review"]')).not.toBeNull(),
    );
  });

  it('edits a rule in place', async () => {
    renderWithClient(<RulesSection />);
    fireEvent.click(await screen.findByRole('button', { name: 'Edit Second' }));
    const form = screen.getByRole('form', { name: 'Edit rule' });
    fireEvent.change(within(form).getByLabelText('Move the ticket to'), {
      target: { value: 'Closed' },
    });
    fireEvent.click(within(form).getByRole('button', { name: 'Preview' }));
    await within(form).findByRole('region', { name: 'Preview' });
    fireEvent.click(within(form).getByRole('button', { name: 'Save rule' }));
    await waitFor(() => expect(api.putRules).toHaveBeenCalled());
    expect(saved().rules.map((r) => [r.id, r.target])).toEqual([
      ['a', 'Ready to Merge'],
      ['b', 'Closed'],
    ]);
    expect(saved().baseline).toBe(false);
  });

  it('opens the preview when a disabled rule is enabled', async () => {
    api.getRules.mockResolvedValue({ rules: [rule('a', { name: 'Off', enabled: false })] });
    renderWithClient(<RulesSection />);
    fireEvent.click(await screen.findByRole('checkbox', { name: 'Enable Off' }));
    const form = await screen.findByRole('form', { name: 'Edit rule' });
    expect(within(form).getByLabelText('Enabled')).toBeChecked();
    expect(api.putRules).not.toHaveBeenCalled();
  });
});
