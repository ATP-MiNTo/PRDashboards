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
  getSettings: vi.fn(),
  putSettings: vi.fn(),
  getJiraOptions: vi.fn(),
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

const settings = {
  polling: { githubSec: 60, jiraSec: 300 },
  theme: 'auto',
  defaultMethodForNewRepos: 'squash',
  deleteBranchAfterMerge: false,
  jiraSiteId: null,
  excludedStatuses: ['Done'],
};

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
  api.getSettings.mockResolvedValue(settings);
  api.putSettings.mockImplementation((v: unknown) => Promise.resolve(v));
  api.getJiraOptions.mockResolvedValue({
    statuses: ['Blocked', 'Closed', 'Done', 'In Review', 'Ready to Merge'],
    projects: ['INP', 'OPS'],
  });
});

/** Opens a dropdown of the form by its name and picks an option. */
function choose(scope: ReturnType<typeof within>, name: string, option: string) {
  fireEvent.click(scope.getByRole('combobox', { name }));
  fireEvent.click(screen.getByRole('option', { name: option }));
}

const optionNames = () =>
  within(screen.getByRole('listbox'))
    .getAllByRole('option')
    .map((o) => o.textContent);

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
    choose(within(form), 'Move the ticket to', 'Done');
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

  it('asks for hours on a stale-commit rule', async () => {
    renderWithClient(<RulesSection />);
    await screen.findByRole('checkbox', { name: 'Enable First' });
    fireEvent.click(screen.getByRole('button', { name: 'Add rule' }));
    const form = screen.getByRole('form', { name: 'New rule' });
    fireEvent.change(within(form).getByLabelText('When'), { target: { value: 'commit_stale' } });
    choose(within(form), 'Move the ticket to', 'Blocked');
    expect(within(form).getByRole('alert')).toHaveTextContent(
      'Hours must be a whole number from 1 to 720.',
    );
    expect(within(form).getByRole('button', { name: 'Preview' })).toBeDisabled();
    fireEvent.change(within(form).getByLabelText('Hours'), { target: { value: '48' } });
    expect(within(form).getByRole('button', { name: 'Preview' })).toBeEnabled();
  });

  it('edits a rule in place', async () => {
    renderWithClient(<RulesSection />);
    fireEvent.click(await screen.findByRole('button', { name: 'Edit Second' }));
    const form = screen.getByRole('form', { name: 'Edit rule' });
    choose(within(form), 'Move the ticket to', 'Closed');
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

  it('has no explanatory paragraph under the title', async () => {
    renderWithClient(<RulesSection />);
    await screen.findByRole('checkbox', { name: 'Enable First' });
    expect(screen.queryByText(/The first enabled rule that matches/)).toBeNull();
  });
});

describe('RuleForm choices', () => {
  const openForm = async () => {
    renderWithClient(<RulesSection />);
    await screen.findByRole('checkbox', { name: 'Enable First' });
    await waitFor(() => expect(api.getJiraOptions).toHaveBeenCalled());
    fireEvent.click(screen.getByRole('button', { name: 'Add rule' }));
    return within(screen.getByRole('form', { name: 'New rule' }));
  };

  it('offers the Jira statuses for the target and for the current status, and the projects', async () => {
    const form = await openForm();
    fireEvent.click(form.getByRole('combobox', { name: 'Move the ticket to' }));
    expect(optionNames()).toEqual([
      'Choose a status',
      'Blocked',
      'Closed',
      'Done',
      'In Review',
      'Ready to Merge',
    ]);
    expect(screen.getByRole('option', { name: 'Choose a status' })).toHaveAttribute(
      'aria-disabled',
      'true',
    );
    fireEvent.click(screen.getByRole('option', { name: 'Ready to Merge' }));
    expect(form.getByRole('combobox', { name: 'Move the ticket to' })).toHaveTextContent(
      'Ready to Merge',
    );

    fireEvent.click(form.getByRole('combobox', { name: 'Current status' }));
    expect(optionNames()).toEqual([
      'Any status',
      'Blocked',
      'Closed',
      'Done',
      'In Review',
      'Ready to Merge',
    ]);
    fireEvent.click(screen.getByRole('option', { name: 'In Review' }));

    fireEvent.click(form.getByRole('combobox', { name: 'Jira project' }));
    expect(optionNames()).toEqual(['Any project', 'INP', 'OPS']);
    fireEvent.click(screen.getByRole('option', { name: 'OPS' }));
    expect(form.getByRole('combobox', { name: 'Jira project' })).toHaveTextContent('OPS');

    fireEvent.change(form.getByLabelText('Repository'), { target: { value: 'acme/*' } });
    fireEvent.click(form.getByRole('button', { name: 'Preview' }));
    await form.findByRole('region', { name: 'Preview' });
    fireEvent.click(form.getByRole('button', { name: 'Save rule' }));
    await waitFor(() => expect(api.putRules).toHaveBeenCalled());
    expect(saved().rules[2]).toMatchObject({
      target: 'Ready to Merge',
      filters: { repo: 'acme/*', project: 'OPS', status: 'In Review', branch: '', issueType: '' },
    });
  });

  it('can clear the project and status filters again', async () => {
    api.getRules.mockResolvedValue({
      rules: [
        rule('a', {
          name: 'First',
          filters: { ...newRule().filters, project: 'INP', status: 'Done' },
        }),
      ],
    });
    renderWithClient(<RulesSection />);
    fireEvent.click(await screen.findByRole('button', { name: 'Edit First' }));
    const form = within(screen.getByRole('form', { name: 'Edit rule' }));
    choose(form, 'Jira project', 'Any project');
    choose(form, 'Current status', 'Any status');
    expect(form.getByRole('combobox', { name: 'Jira project' })).toHaveTextContent('Any project');
    expect(form.getByRole('combobox', { name: 'Current status' })).toHaveTextContent('Any status');
  });

  it('keeps showing a value that Jira no longer lists', async () => {
    api.getRules.mockResolvedValue({
      rules: [
        rule('a', {
          name: 'First',
          target: 'Legacy',
          filters: { ...newRule().filters, project: 'OLD', status: 'Gone' },
        }),
      ],
    });
    renderWithClient(<RulesSection />);
    fireEvent.click(await screen.findByRole('button', { name: 'Edit First' }));
    const form = within(screen.getByRole('form', { name: 'Edit rule' }));
    expect(form.getByRole('combobox', { name: 'Move the ticket to' })).toHaveTextContent('Legacy');
    expect(form.getByRole('combobox', { name: 'Jira project' })).toHaveTextContent('OLD');
    expect(form.getByRole('combobox', { name: 'Current status' })).toHaveTextContent('Gone');
  });

  it('shows a saved near-duplicate of a listed status by its own spelling', async () => {
    api.getJiraOptions.mockResolvedValue({ statuses: ['Done', 'Reopen'], projects: ['A_B', 'AB'] });
    api.getRules.mockResolvedValue({
      rules: [
        rule('a', {
          name: 'First',
          target: 'Re-open',
          filters: { ...newRule().filters, project: 'AB', status: 're open' },
        }),
      ],
    });
    renderWithClient(<RulesSection />);
    fireEvent.click(await screen.findByRole('button', { name: 'Edit First' }));
    const form = within(screen.getByRole('form', { name: 'Edit rule' }));
    const target = form.getByRole('combobox', { name: 'Move the ticket to' });
    expect(target).toHaveTextContent('Re-open');
    fireEvent.click(target);
    expect(optionNames()).toEqual(['Done', 'Re-open']);
    fireEvent.keyDown(target, { key: 'Escape' });
    expect(form.getByRole('combobox', { name: 'Current status' })).toHaveTextContent('re open');
    fireEvent.click(form.getByRole('combobox', { name: 'Jira project' }));
    expect(optionNames()).toEqual(['Any project', 'A_B', 'AB']);
  });

  it('falls back to the statuses and projects of the snapshot when Jira gives none', async () => {
    api.getJiraOptions.mockResolvedValue({ statuses: [], projects: [] });
    const form = await openForm();
    fireEvent.click(form.getByRole('combobox', { name: 'Move the ticket to' }));
    expect(optionNames()).toEqual(['Choose a status', 'In Review']);
    fireEvent.click(screen.getByRole('option', { name: 'In Review' }));
    fireEvent.click(form.getByRole('combobox', { name: 'Jira project' }));
    expect(optionNames()).toEqual(['Any project', 'INP']);
  });

  it('falls back to the snapshot when the Jira options fail', async () => {
    api.getJiraOptions.mockRejectedValue(new Error('409'));
    renderWithClient(<RulesSection />);
    await screen.findByRole('checkbox', { name: 'Enable First' });
    await waitFor(() => expect(api.getJiraOptions).toHaveBeenCalled());
    fireEvent.click(screen.getByRole('button', { name: 'Add rule' }));
    const form = within(screen.getByRole('form', { name: 'New rule' }));
    choose(form, 'Move the ticket to', 'In Review');
    expect(form.getByRole('combobox', { name: 'Move the ticket to' })).toHaveTextContent(
      'In Review',
    );
    expect(form.getByRole('combobox', { name: 'Current status' })).toBeInTheDocument();
  });

  it('keeps issue type, repository and branch as text inputs', async () => {
    const form = await openForm();
    for (const name of ['Repository', 'Target branch', 'Issue type']) {
      expect(form.getByLabelText(name)).toHaveAttribute('type', 'text');
    }
  });

  it('asks Jira once, on demand', async () => {
    const form = await openForm();
    choose(form, 'Move the ticket to', 'Done');
    choose(form, 'Current status', 'Done');
    expect(api.getJiraOptions).toHaveBeenCalledTimes(1);
  });
});

describe('Exclude statuses', () => {
  const group = async () => within(await screen.findByRole('group', { name: 'Exclude statuses' }));
  const trigger = async () => (await group()).getByRole('combobox', { name: 'Exclude statuses' });
  const open = async () => {
    const box = await trigger();
    fireEvent.click(box);
    return box;
  };

  it('shows the excluded statuses as chips, the summary and the hint', async () => {
    renderWithClient(<RulesSection />);
    const box = await trigger();
    expect(screen.getByText('Tickets in these statuses are never moved.')).toBeInTheDocument();
    expect(box).toHaveTextContent('Done');
    expect((await group()).getByRole('button', { name: 'Remove Done' })).toBeInTheDocument();
    await open();
    expect(screen.getByRole('option', { name: 'Done' })).toHaveAttribute('aria-selected', 'true');
    expect(screen.getByRole('option', { name: 'Closed' })).toHaveAttribute(
      'aria-selected',
      'false',
    );
  });

  it('searches, then saves the whole settings with the status added or removed', async () => {
    let stored = settings;
    api.getSettings.mockImplementation(() => Promise.resolve(stored));
    api.putSettings.mockImplementation((v: typeof settings) => {
      stored = v;
      return Promise.resolve(v);
    });
    renderWithClient(<RulesSection />);
    await open();
    fireEvent.change(screen.getByRole('searchbox', { name: 'Search Exclude statuses' }), {
      target: { value: 'clo' },
    });
    expect(screen.getAllByRole('option')).toHaveLength(1);
    fireEvent.click(screen.getByRole('option', { name: 'Closed' }));
    await waitFor(() =>
      expect(api.putSettings).toHaveBeenLastCalledWith({
        ...settings,
        excludedStatuses: ['Done', 'Closed'],
      }),
    );
    fireEvent.click((await group()).getByRole('button', { name: 'Remove Done' }));
    await waitFor(() =>
      expect(api.putSettings).toHaveBeenLastCalledWith({
        ...settings,
        excludedStatuses: ['Closed'],
      }),
    );
  });

  it('treats a near-duplicate of a listed status as the same one', async () => {
    api.getJiraOptions.mockResolvedValue({ statuses: ['Reopen', 'Re open', 'Done'], projects: [] });
    api.getSettings.mockResolvedValue({ ...settings, excludedStatuses: ['re-open'] });
    renderWithClient(<RulesSection />);
    await open();
    expect(screen.getAllByRole('option').map((o) => o.textContent)).toEqual(['✓Reopen', '✓Done']);
    expect(screen.getByRole('option', { name: 'Reopen' })).toHaveAttribute('aria-selected', 'true');
  });

  it('keeps an excluded status that Jira does not list, and falls back to the snapshot', async () => {
    api.getJiraOptions.mockResolvedValue({ statuses: [], projects: [] });
    api.getSettings.mockResolvedValue({ ...settings, excludedStatuses: ['Archived'] });
    renderWithClient(<RulesSection />);
    expect(
      await (await group()).findByRole('button', { name: 'Remove Archived' }),
    ).toBeInTheDocument();
    await open();
    expect(screen.getByRole('option', { name: 'Archived' })).toHaveAttribute(
      'aria-selected',
      'true',
    );
    expect(screen.getByRole('option', { name: 'In Review' })).toHaveAttribute(
      'aria-selected',
      'false',
    );
  });

  it('says so when saving fails', async () => {
    api.putSettings.mockRejectedValue(new Error('x'));
    renderWithClient(<RulesSection />);
    await open();
    fireEvent.click(screen.getByRole('option', { name: 'Closed' }));
    expect(await screen.findByText('Could not save the excluded statuses.')).toBeInTheDocument();
  });
});
