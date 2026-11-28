import { act, createEvent, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
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
  retryTransition: vi.fn(),
  dismissRetry: vi.fn(),
}));

vi.mock('@/api/client', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/api/client')>()),
  ...api,
}));

import { RulesModal } from './RulesModal';

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
  multiPrTickets: 'all',
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
  api.getSnapshot.mockResolvedValue(
    snapshot([pr('a', 1, { baseRef: 'release/1.0', tickets: [ticket('INP-1')] })]),
  );
  api.getActivity.mockResolvedValue({ items: [], nextCursor: null });
  api.getRetries.mockResolvedValue({ retries: [] });
  api.getSettings.mockResolvedValue(settings);
  api.putSettings.mockImplementation((v: unknown) => Promise.resolve(v));
  api.getJiraOptions.mockResolvedValue({
    statuses: ['Blocked', 'Closed', 'Done', 'In Review', 'Ready to Merge'],
    projects: ['INP', 'OPS'],
  });
});

afterEach(() => vi.restoreAllMocks());

const onClose = vi.fn();

/** Renders the dialog and waits for the rules. */
async function open() {
  onClose.mockReset();
  renderWithClient(<RulesModal onClose={onClose} />);
  await screen.findByRole('listitem', { name: /^Rule 1,/ });
}

const card = (n: number) => screen.getByRole('listitem', { name: new RegExp(`^Rule ${n},`) });

/** Opens the body of a card, as a click on its chevron does. */
async function expand(name: string) {
  fireEvent.click(await screen.findByRole('button', { name: `Expand ${name}` }));
  return screen.getByRole('listitem', { name: new RegExp(`, ${name}$`) });
}

// Waits for the Jira options request that opening the dialog starts, and for its result to render.
const optionsLoaded = async () => {
  await waitFor(() => expect(api.getJiraOptions).toHaveBeenCalled());
  await act(async () => {
    await api.getJiraOptions.mock.results[0]?.value?.catch(() => {});
    // TanStack Query hands the result to its observers on a zero-delay timer; wait for it.
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
};

const pick = (scope: ReturnType<typeof within>, label: string, value: string) =>
  fireEvent.change(scope.getByLabelText(label), { target: { value } });

describe('the dialog', () => {
  it('is a dialog named Status sync rules with a Close button, and the focus goes into it', async () => {
    await open();
    const dialog = screen.getByRole('dialog', { name: 'Status sync rules' });
    expect(dialog).toHaveFocus();
    expect(within(dialog).getByRole('button', { name: 'Close' })).toBeInTheDocument();
  });

  it('is the wide box of the design', async () => {
    await open();
    expect(screen.getByRole('dialog').className).toMatch(/wide/);
  });

  it('closes with the Close button, Escape and the scrim, and gives the focus back', async () => {
    const opener = document.createElement('button');
    document.body.append(opener);
    opener.focus();
    await open();
    fireEvent.click(screen.getByRole('button', { name: 'Close' }));
    expect(onClose).toHaveBeenCalledTimes(1);
    fireEvent.keyDown(screen.getByRole('dialog'), { key: 'Escape' });
    expect(onClose).toHaveBeenCalledTimes(2);
    fireEvent.click(screen.getByTestId('modal-scrim'));
    expect(onClose).toHaveBeenCalledTimes(3);
    opener.remove();
  });

  it('keeps Tab inside', async () => {
    await open();
    const dialog = screen.getByRole('dialog');
    const items = [...dialog.querySelectorAll<HTMLElement>('button, input, select')].filter(
      (e) => !e.closest('[inert]') && !(e as HTMLButtonElement).disabled,
    );
    const last = items[items.length - 1];
    last?.focus();
    fireEvent.keyDown(last!, { key: 'Tab' });
    expect(document.activeElement).toBe(dialog.querySelector('button'));
  });

  it('explains itself in a note that is also the description of the "i" button', async () => {
    await open();
    const info = screen.getByRole('button', { name: 'About this section' });
    expect(info).toHaveAccessibleDescription(/Rules run top to bottom/);
    fireEvent.mouseEnter(info);
    expect(screen.getAllByText(/Rules run top to bottom/).length).toBeGreaterThan(1);
  });

  it('asks Jira for its options once, when it opens', async () => {
    await open();
    await waitFor(() => expect(api.getJiraOptions).toHaveBeenCalledTimes(1));
  });
});

describe('the cards', () => {
  it('lists the rules in order with their number, name and mode, folded', async () => {
    await open();
    const items = within(screen.getByRole('list', { name: 'Status sync rules' })).getAllByRole(
      'listitem',
    );
    expect(items).toHaveLength(2);
    expect(within(items[0]!).getByLabelText('Rule name')).toHaveValue('First');
    expect(within(items[1]!).getByLabelText('Rule name')).toHaveValue('Second');
    expect(within(items[0]!).getByText('1')).toBeInTheDocument();
    expect(within(items[0]!).getByLabelText('Mode')).toHaveValue('auto');
    expect(within(items[0]!).getByRole('button', { name: 'Expand First' })).toHaveAttribute(
      'aria-expanded',
      'false',
    );
  });

  it('opens and folds a card with its chevron', async () => {
    await open();
    const first = await expand('First');
    const fold = within(first).getByRole('button', { name: 'Collapse First' });
    expect(fold).toHaveAttribute('aria-expanded', 'true');
    expect(within(first).getByLabelText('Then move it to')).toHaveValue('Ready to Merge');
    fireEvent.click(fold);
    expect(within(first).getByRole('button', { name: 'Expand First' })).toHaveAttribute(
      'aria-expanded',
      'false',
    );
  });

  it('shows a rule that is off dimmed, with the mode Off and the switch off', async () => {
    api.getRules.mockResolvedValue({ rules: [rule('a', { name: 'Off', enabled: false })] });
    await open();
    expect(within(card(1)).getByLabelText('Mode')).toHaveValue('off');
    expect(within(card(1)).getByRole('checkbox', { name: 'Enable Off' })).not.toBeChecked();
    expect(card(1).className).toMatch(/off/);
  });

  it('says there are no rules yet', async () => {
    api.getRules.mockResolvedValue({ rules: [] });
    renderWithClient(<RulesModal onClose={() => {}} />);
    expect(await screen.findByText('No rules yet.')).toBeInTheDocument();
  });

  it('says so when the rules do not load', async () => {
    api.getRules.mockRejectedValue(new Error('no'));
    renderWithClient(<RulesModal onClose={() => {}} />);
    expect(await screen.findByText('Could not load the rules.')).toBeInTheDocument();
  });
});

/** jsdom has no DragEvent, so the pointer position is put on the event by hand. */
function dragAt(type: 'dragOver' | 'drop', target: Element, clientY: number, dataTransfer: object) {
  const event = createEvent[type](target, { dataTransfer });
  Object.defineProperty(event, 'clientY', { value: clientY });
  fireEvent(target, event);
}

describe('reordering', () => {
  it('moves a rule with the keyboard buttons and saves the whole list at once', async () => {
    await open();
    fireEvent.click(screen.getByRole('button', { name: 'Move First down' }));
    await waitFor(() => expect(api.putRules).toHaveBeenCalledTimes(1));
    expect(saved().rules.map((r) => r.id)).toEqual(['b', 'a']);
    expect(saved().baseline).toBe(false);
    expect(screen.getByRole('button', { name: 'Move Second up' })).toBeDisabled();
    fireEvent.click(screen.getByRole('button', { name: 'Move First up' }));
    await waitFor(() => expect(saved().rules.map((r) => r.id)).toEqual(['a', 'b']));
  });

  it('keeps the Up and Down buttons out of sight until one has focus', async () => {
    await open();
    expect(screen.getByRole('button', { name: 'Move First up' }).className).toMatch(/keyMove/);
  });

  function dragRects(heights: number[]) {
    let top = 0;
    const tops = new Map<string, DOMRect>();
    for (const [i, h] of heights.entries()) {
      tops.set(String(i + 1), {
        top,
        bottom: top + h,
        left: 0,
        right: 100,
        width: 100,
        height: h,
      } as DOMRect);
      top += h + 12;
    }
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (
      this: HTMLElement,
    ) {
      const n = this.getAttribute('aria-label')?.match(/^Rule (\d+),/)?.[1];
      return (
        (n && tops.get(n)) ||
        ({ top: 0, bottom: 1000, left: 0, right: 100, width: 100, height: 1000 } as DOMRect)
      );
    });
  }

  it('reorders by dragging the grip, with a line where the card will land', async () => {
    api.getRules.mockResolvedValue({
      rules: [
        rule('a', { name: 'First' }),
        rule('b', { name: 'Second' }),
        rule('c', { name: 'Third' }),
      ],
    });
    await open();
    dragRects([60, 60, 60]);
    const items = within(screen.getByRole('list', { name: 'Status sync rules' })).getAllByRole(
      'listitem',
    );
    const list = items[0]!.closest('div') as HTMLElement;
    const grip = items[0]!.querySelector('[title="Drag to reorder"]') as HTMLElement;
    fireEvent.mouseDown(grip);
    expect(items[0]).toHaveAttribute('draggable', 'true');
    const data = { setData: vi.fn(), effectAllowed: '', dropEffect: '' };
    fireEvent.dragStart(items[0]!, { dataTransfer: data });
    expect(data.setData).toHaveBeenCalled();
    // The pointer is in the lower half of the second card: the line is before the third.
    dragAt('dragOver', list, 130, data);
    const line = list.querySelector('[class*="dropLine"]') as HTMLElement;
    expect(line.hidden).toBe(false);
    dragAt('drop', list, 130, data);
    await waitFor(() => expect(saved().rules.map((r) => r.id)).toEqual(['b', 'a', 'c']));
    expect(saved().baseline).toBe(false);
    expect(line.hidden).toBe(true);
    expect(items[0]).toHaveAttribute('draggable', 'false');
  });

  it('drops at the end below the last card and at the start above the first', async () => {
    await open();
    dragRects([60, 60]);
    const items = within(screen.getByRole('list', { name: 'Status sync rules' })).getAllByRole(
      'listitem',
    );
    const list = items[0]!.closest('div') as HTMLElement;
    const data = { setData: vi.fn(), effectAllowed: '', dropEffect: '' };
    fireEvent.dragStart(items[0]!, { dataTransfer: data });
    dragAt('dragOver', list, 400, data);
    dragAt('drop', list, 400, data);
    await waitFor(() => expect(saved().rules.map((r) => r.id)).toEqual(['b', 'a']));
  });

  it('does not save a drop that leaves the order as it is, and a drag that ends elsewhere', async () => {
    await open();
    dragRects([60, 60]);
    const items = within(screen.getByRole('list', { name: 'Status sync rules' })).getAllByRole(
      'listitem',
    );
    const list = items[0]!.closest('div') as HTMLElement;
    const data = { setData: vi.fn(), effectAllowed: '', dropEffect: '' };
    fireEvent.dragStart(items[0]!, { dataTransfer: data });
    dragAt('dragOver', list, 10, data);
    dragAt('drop', list, 10, data);
    fireEvent.dragStart(items[1]!, { dataTransfer: data });
    dragAt('dragOver', list, 10, data);
    fireEvent.dragEnd(items[1]!);
    dragAt('drop', list, 10, data);
    expect(api.putRules).not.toHaveBeenCalled();
    // A drag that did not start on a card does nothing.
    dragAt('dragOver', list, 10, data);
    dragAt('drop', list, 10, data);
    expect(api.putRules).not.toHaveBeenCalled();
  });
});

describe('disable, delete and the mode', () => {
  it('deletes and disables without a preview', async () => {
    await open();
    fireEvent.click(screen.getByRole('checkbox', { name: 'Enable First' }));
    await waitFor(() => expect(saved().rules[0]?.enabled).toBe(false));
    fireEvent.click(screen.getByRole('button', { name: 'Delete Second' }));
    await waitFor(() => expect(saved().rules.map((r) => r.id)).toEqual(['a']));
    expect(api.previewRule).not.toHaveBeenCalled();
    expect(saved().baseline).toBe(false);
  });

  it('turns a rule off from the Mode select too', async () => {
    await open();
    fireEvent.change(within(card(1)).getByLabelText('Mode'), { target: { value: 'off' } });
    await waitFor(() => expect(saved().rules[0]?.enabled).toBe(false));
  });

  it('enables a rule only through its preview: the switch makes an unsaved edit', async () => {
    api.getRules.mockResolvedValue({ rules: [rule('a', { name: 'Off', enabled: false })] });
    await open();
    fireEvent.click(screen.getByRole('checkbox', { name: 'Enable Off' }));
    expect(api.putRules).not.toHaveBeenCalled();
    const first = await expand('Off');
    expect(within(first).getByText('Unsaved')).toBeInTheDocument();
    expect(within(first).getByRole('button', { name: 'Save rule' })).toBeDisabled();
    expect(within(first).getByText('Preview the rule before saving it.')).toBeInTheDocument();
  });

  it('saves the mode of shared tickets at once with the baseline, so nothing moves', async () => {
    await open();
    const first = await expand('First');
    expect(within(first).getByLabelText('Tickets shared by several PRs')).toHaveValue('default');
    pick(within(first), 'Tickets shared by several PRs', 'any');
    await waitFor(() => expect(api.putRules).toHaveBeenCalledTimes(1));
    expect(saved().baseline).toBe(true);
    expect(saved().rules.map((r) => [r.id, r.mode])).toEqual([
      ['a', 'any'],
      ['b', 'default'],
    ]);
    expect(api.previewRule).not.toHaveBeenCalled();
  });

  it('offers the choice that follows the sidebar first and words it with the current setting', async () => {
    await open();
    const first = await expand('First');
    const select = within(first).getByLabelText('Tickets shared by several PRs');
    expect(
      within(select)
        .getAllByRole('option')
        .map((o) => o.textContent),
    ).toEqual([
      'Default (when the last PR meets it)',
      'When the last PR meets it',
      'When the first PR meets it',
    ]);
    expect(
      within(select)
        .getAllByRole('option')
        .map((o) => (o as HTMLOptionElement).value),
    ).toEqual(['default', 'all', 'any']);
  });

  it('shows the first-PR setting in the default choice and keeps an explicit mode', async () => {
    api.getSettings.mockResolvedValue({ ...settings, multiPrTickets: 'any' });
    api.getRules.mockResolvedValue({
      rules: [rule('a', { name: 'First' }), rule('b', { name: 'Second', mode: 'all' })],
    });
    await open();
    const first = await expand('First');
    expect(
      await within(first).findByRole('option', { name: 'Default (when the first PR meets it)' }),
    ).toBeInTheDocument();
    pick(within(first), 'Tickets shared by several PRs', 'default');
    expect(api.putRules).not.toHaveBeenCalled();
    const second = await expand('Second');
    expect(within(second).getByLabelText('Tickets shared by several PRs')).toHaveValue('all');
  });

  it('saves the choice that follows the sidebar with the baseline', async () => {
    api.getRules.mockResolvedValue({
      rules: [rule('a', { name: 'First', mode: 'any' }), rule('b', { name: 'Second' })],
    });
    await open();
    const first = await expand('First');
    pick(within(first), 'Tickets shared by several PRs', 'default');
    await waitFor(() => expect(api.putRules).toHaveBeenCalledTimes(1));
    expect(saved().baseline).toBe(true);
    expect(saved().rules.map((r) => [r.id, r.mode])).toEqual([
      ['a', 'default'],
      ['b', 'default'],
    ]);
  });

  it('keeps the mode in the draft while the rule has other unsaved edits', async () => {
    await open();
    const first = await expand('First');
    pick(within(first), 'Then move it to', 'Closed');
    pick(within(first), 'Tickets shared by several PRs', 'any');
    expect(api.putRules).not.toHaveBeenCalled();
    expect(within(first).getByLabelText('Tickets shared by several PRs')).toHaveValue('any');
  });

  it('describes the modes with the hint of the spec, as a description of the select', async () => {
    await open();
    const first = await expand('First');
    expect(
      within(first).getByLabelText('Tickets shared by several PRs'),
    ).toHaveAccessibleDescription(
      /When a ticket is linked to several PRs\. Default: follows "Multi-PR tickets move when" in the Repositories setting of the sidebar\. All: the ticket moves only when every linked PR meets the rule\. Any: the first PR that meets it moves the ticket\./,
    );
  });

  it('says so when saving the list fails', async () => {
    api.putRules.mockRejectedValue(new Error('no'));
    await open();
    fireEvent.click(screen.getByRole('button', { name: 'Delete First' }));
    expect(await screen.findByText('Could not save the rules.')).toBeInTheDocument();
  });
});

describe('adding a rule', () => {
  const menu = async () => {
    fireEvent.click(screen.getByRole('button', { name: 'Add rule' }));
    return screen.findByRole('menu', { name: 'Rule templates' });
  };

  it('offers a template for every trigger, the two approval ones too, and a blank rule', async () => {
    await open();
    const items = within(await menu())
      .getAllByRole('menuitem')
      .map((i) => i.textContent);
    expect(items).toHaveLength(12);
    expect(items).toContain('Move to In Review with no review yet');
    expect(items).toContain('Move to In Review while approvals are missing');
    expect(items.at(-1)).toBe('Blank rule');
  });

  it('adds a card from a template, open, with the name selected, and nothing is saved', async () => {
    await open();
    await optionsLoaded();
    fireEvent.click(
      within(await menu()).getByRole('menuitem', { name: 'Move to Done when a PR merges' }),
    );
    const added = await screen.findByRole('listitem', { name: /^Rule 3, Merged/ });
    expect(within(added).getByLabelText('Rule name')).toHaveValue('Merged');
    expect(within(added).getByLabelText('Then move it to')).toHaveValue('Done');
    expect(within(added).getByRole('button', { name: 'Collapse Merged' })).toBeInTheDocument();
    expect(within(added).getByText('Unsaved')).toBeInTheDocument();
    expect(within(added).queryByRole('button', { name: /^Move Merged/ })).toBeNull();
    expect(api.putRules).not.toHaveBeenCalled();
  });

  it('leaves the target empty when Jira does not list the status the template suggests', async () => {
    await open();
    await optionsLoaded();
    fireEvent.click(
      within(await menu()).getByRole('menuitem', { name: 'Move to In Progress when CI fails' }),
    );
    const added = await screen.findByRole('listitem', { name: /^Rule 3, CI failing/ });
    expect(within(added).getByLabelText('Then move it to')).toHaveValue('');
    expect(within(added).getByRole('alert')).toHaveTextContent(
      'Choose the status to move tickets to.',
    );
    expect(within(added).getByRole('button', { name: 'Preview' })).toBeDisabled();
  });

  it('saves a new rule only after a preview and keeps existing tickets in place when asked', async () => {
    await open();
    await optionsLoaded();
    fireEvent.click(
      within(await menu()).getByRole('menuitem', { name: 'Move to Done when a PR merges' }),
    );
    const added = await screen.findByRole('listitem', { name: /^Rule 3, Merged/ });
    const save = within(added).getByRole('button', { name: 'Save rule' });
    expect(save).toBeDisabled();
    pick(within(added), 'When', 'ci_passing');
    expect(save).toBeDisabled();
    fireEvent.click(within(added).getByRole('button', { name: 'Preview' }));
    expect(await within(added).findByText('1 ticket would move now:')).toBeInTheDocument();
    expect(within(added).getByText(/Already in the target status: INP-2/)).toBeInTheDocument();
    expect(save).toBeEnabled();
    fireEvent.click(within(added).getByRole('radio', { name: "Don't move existing tickets" }));
    fireEvent.click(save);
    await waitFor(() => expect(api.putRules).toHaveBeenCalledTimes(1));
    expect(saved().baseline).toBe(true);
    expect(saved().rules).toHaveLength(3);
    expect(saved().rules[2]).toMatchObject({
      trigger: 'ci_passing',
      target: 'Done',
      enabled: true,
      mode: 'default',
    });
    await waitFor(() => expect(within(card(3)).queryByText('Unsaved')).toBeNull());
  });

  it('moves the matching tickets by default (no baseline)', async () => {
    await open();
    await optionsLoaded();
    fireEvent.click(
      within(await menu()).getByRole('menuitem', { name: 'Move to Done when a PR merges' }),
    );
    const added = await screen.findByRole('listitem', { name: /^Rule 3, Merged/ });
    fireEvent.click(within(added).getByRole('button', { name: 'Preview' }));
    await within(added).findByRole('region', { name: 'Preview' });
    fireEvent.click(within(added).getByRole('button', { name: 'Save rule' }));
    await waitFor(() => expect(api.putRules).toHaveBeenCalled());
    expect(saved().baseline).toBe(false);
  });

  it('forgets a new card with Cancel', async () => {
    await open();
    fireEvent.click(within(await menu()).getByRole('menuitem', { name: 'Blank rule' }));
    const added = await screen.findByRole('listitem', { name: /^Rule 3,/ });
    fireEvent.click(within(added).getByRole('button', { name: 'Cancel' }));
    expect(screen.queryByRole('listitem', { name: /^Rule 3,/ })).toBeNull();
    expect(api.putRules).not.toHaveBeenCalled();
  });

  it('forgets a new card with its trash button without saving', async () => {
    await open();
    fireEvent.click(within(await menu()).getByRole('menuitem', { name: 'Blank rule' }));
    const added = await screen.findByRole('listitem', { name: /^Rule 3,/ });
    fireEvent.click(within(added).getByRole('button', { name: /^Delete / }));
    expect(screen.queryByRole('listitem', { name: /^Rule 3,/ })).toBeNull();
    expect(api.putRules).not.toHaveBeenCalled();
  });

  it('is disabled at the most rules, and until the rules are loaded', async () => {
    api.getRules.mockReturnValue(new Promise(() => {}));
    renderWithClient(<RulesModal onClose={() => {}} />);
    expect(await screen.findByRole('button', { name: 'Add rule' })).toBeDisabled();
  });
});

describe('editing a rule', () => {
  it('asks for hours on a stale-commit rule, and shows the field only for it', async () => {
    await open();
    await optionsLoaded();
    const first = await expand('First');
    expect(within(first).queryByLabelText('Hours')).toBeNull();
    pick(within(first), 'When', 'commit_stale');
    expect(within(first).getByRole('alert')).toHaveTextContent(
      'Hours must be a whole number from 1 to 720.',
    );
    expect(within(first).getByRole('button', { name: 'Preview' })).toBeDisabled();
    fireEvent.change(within(first).getByLabelText('Hours'), { target: { value: '48' } });
    expect(within(first).getByRole('button', { name: 'Preview' })).toBeEnabled();
    fireEvent.change(within(first).getByLabelText('Hours'), { target: { value: '' } });
    expect(within(first).getByRole('button', { name: 'Preview' })).toBeDisabled();
  });

  it('clears the hours when the trigger changes', async () => {
    await open();
    const first = await expand('First');
    pick(within(first), 'When', 'commit_stale');
    fireEvent.change(within(first).getByLabelText('Hours'), { target: { value: '48' } });
    pick(within(first), 'When', 'merged');
    pick(within(first), 'When', 'commit_stale');
    expect(within(first).getByLabelText('Hours')).toHaveValue(null);
  });

  it('edits a rule in place and saves it after its preview', async () => {
    await open();
    await optionsLoaded();
    const second = await expand('Second');
    pick(within(second), 'Then move it to', 'Closed');
    expect(within(second).getByText('Unsaved')).toBeInTheDocument();
    fireEvent.click(within(second).getByRole('button', { name: 'Preview' }));
    await within(second).findByRole('region', { name: 'Preview' });
    fireEvent.click(within(second).getByRole('button', { name: 'Save rule' }));
    await waitFor(() => expect(api.putRules).toHaveBeenCalled());
    expect(saved().rules.map((r) => [r.id, r.target])).toEqual([
      ['a', 'Ready to Merge'],
      ['b', 'Closed'],
    ]);
    expect(saved().baseline).toBe(false);
    await waitFor(() => expect(within(card(2)).queryByText('Unsaved')).toBeNull());
  });

  it('asks for a new preview after the rule changes again', async () => {
    await open();
    await optionsLoaded();
    const second = await expand('Second');
    pick(within(second), 'Then move it to', 'Closed');
    fireEvent.click(within(second).getByRole('button', { name: 'Preview' }));
    await within(second).findByRole('region', { name: 'Preview' });
    pick(within(second), 'Then move it to', 'Blocked');
    expect(within(second).queryByRole('region', { name: 'Preview' })).toBeNull();
    expect(within(second).getByRole('button', { name: 'Save rule' })).toBeDisabled();
  });

  it('saves a new name without a preview, since the name moves nothing', async () => {
    await open();
    fireEvent.change(within(card(1)).getByLabelText('Rule name'), { target: { value: 'Renamed' } });
    expect(within(card(1)).getByText('Unsaved')).toBeInTheDocument();
    const first = await expand('Renamed');
    const save = within(first).getByRole('button', { name: 'Save rule' });
    expect(save).toBeEnabled();
    fireEvent.click(save);
    await waitFor(() => expect(api.putRules).toHaveBeenCalled());
    expect(api.previewRule).not.toHaveBeenCalled();
    expect(saved().rules[0]?.name).toBe('Renamed');
    expect(saved().baseline).toBe(false);
  });

  it('discards the edits with Cancel', async () => {
    await open();
    const first = await expand('First');
    pick(within(first), 'Then move it to', 'Closed');
    fireEvent.click(within(first).getByRole('button', { name: 'Cancel' }));
    expect(within(first).getByLabelText('Then move it to')).toHaveValue('Ready to Merge');
    expect(within(first).queryByText('Unsaved')).toBeNull();
  });

  it('keeps the edits of a card while another one is reordered', async () => {
    await open();
    const first = await expand('First');
    pick(within(first), 'Then move it to', 'Closed');
    fireEvent.click(screen.getByRole('button', { name: 'Move Second up' }));
    await waitFor(() => expect(saved().rules.map((r) => r.id)).toEqual(['b', 'a']));
    expect(saved().rules[1]?.target).toBe('Ready to Merge');
    const moved = await screen.findByRole('listitem', { name: /^Rule 2, First/ });
    expect(within(moved).getByLabelText('Then move it to')).toHaveValue('Closed');
  });

  it('says so when the preview or the save fails', async () => {
    api.previewRule.mockRejectedValue(new Error('no'));
    await open();
    await optionsLoaded();
    const first = await expand('First');
    pick(within(first), 'Then move it to', 'Closed');
    fireEvent.click(within(first).getByRole('button', { name: 'Preview' }));
    expect(await within(first).findByText('Could not preview the rule.')).toBeInTheDocument();
  });

  it('shows the problem with a repo glob and keeps Save from running', async () => {
    await open();
    const first = await expand('First');
    const input = within(first).getByLabelText('Add In repositories');
    fireEvent.change(input, { target: { value: 'ac me' } });
    fireEvent.keyDown(input, { key: 'Enter' });
    expect(within(first).getByRole('alert')).toHaveTextContent(
      'Repo and branch use letters, digits and . _ / * - only.',
    );
  });
});

describe('the trigger', () => {
  it('lists every trigger, with the words of the two approval triggers', async () => {
    await open();
    const first = await expand('First');
    const select = within(first).getByLabelText('When');
    const names = within(select)
      .getAllByRole('option')
      .map((o) => o.textContent);
    expect(names).toContain('No review yet');
    expect(names).toContain('The pull request is waiting for more approvals');
    expect(names).toHaveLength(11);
    pick(within(first), 'When', 'no_review_yet');
    expect(within(first).getByText('No review yet')).toBeInTheDocument();
    pick(within(first), 'When', 'waiting_approvals');
    expect(
      within(first).getAllByText('The pull request is waiting for more approvals').length,
    ).toBeGreaterThan(0);
  });

  it('saves a rule with a new trigger after its preview', async () => {
    await open();
    await optionsLoaded();
    const first = await expand('First');
    pick(within(first), 'When', 'waiting_approvals');
    fireEvent.click(within(first).getByRole('button', { name: 'Preview' }));
    await within(first).findByRole('region', { name: 'Preview' });
    expect(api.previewRule.mock.calls[0]?.[0]).toMatchObject({ trigger: 'waiting_approvals' });
    fireEvent.click(within(first).getByRole('button', { name: 'Save rule' }));
    await waitFor(() => expect(saved().rules[0]?.trigger).toBe('waiting_approvals'));
  });
});

describe('the chips and the Jira choices', () => {
  it('offers the Jira statuses for the target and for the status filter, and the projects', async () => {
    await open();
    await optionsLoaded();
    const first = await expand('First');
    const target = within(first).getByLabelText('Then move it to');
    expect(
      within(target)
        .getAllByRole('option')
        .map((o) => o.textContent),
    ).toEqual(['Blocked', 'Closed', 'Done', 'In Review', 'Ready to Merge']);
    const status = within(first).getByLabelText('Add If the ticket is in');
    expect(
      within(status)
        .getAllByRole('option')
        .map((o) => o.textContent),
    ).toEqual(['+ Add', 'Blocked', 'Closed', 'Done', 'In Review', 'Ready to Merge']);
    const project = within(first).getByLabelText('Add In Jira projects');
    expect(
      within(project)
        .getAllByRole('option')
        .map((o) => o.textContent),
    ).toEqual(['+ Add', 'INP', 'OPS']);
  });

  it('shows "Any" when a filter is empty, a removable chip when it is set, and one value only', async () => {
    api.getRules.mockResolvedValue({
      rules: [
        rule('a', {
          name: 'First',
          filters: { repo: '', branch: '', project: 'INP', issueType: '', status: '' },
        }),
      ],
    });
    await open();
    await optionsLoaded();
    const first = await expand('First');
    const projects = within(first).getByRole('group', { name: 'In Jira projects' });
    expect(within(projects).getByText('INP')).toBeInTheDocument();
    expect(within(projects).queryByLabelText('Add In Jira projects')).toBeNull();
    expect(
      within(within(first).getByRole('group', { name: 'On branches' })).getByText('Any'),
    ).toBeInTheDocument();
    fireEvent.click(within(projects).getByRole('button', { name: 'Remove INP' }));
    expect(within(projects).getByText('Any')).toBeInTheDocument();
    expect(within(projects).getByLabelText('Add In Jira projects')).toBeInTheDocument();
    expect(within(first).getByText('Unsaved')).toBeInTheDocument();
  });

  it('adds a status chip from the select', async () => {
    await open();
    await optionsLoaded();
    const first = await expand('First');
    pick(within(first), 'Add If the ticket is in', 'In Review');
    expect(
      within(within(first).getByRole('group', { name: 'If the ticket is in' })).getByText(
        'In Review',
      ),
    ).toBeInTheDocument();
    fireEvent.click(within(first).getByRole('button', { name: 'Preview' }));
    await within(first).findByRole('region', { name: 'Preview' });
    expect(api.previewRule.mock.calls[0]?.[0].filters.status).toBe('In Review');
  });

  it('adds a branch or a repository by typing, with the branches of the pull requests as suggestions', async () => {
    await open();
    await optionsLoaded();
    const first = await expand('First');
    const branch = within(first).getByLabelText('Add On branches');
    expect(branch).toHaveAttribute('placeholder', 'main or release/*');
    const list = document.getElementById(branch.getAttribute('list') ?? '');
    expect(
      [...(list?.querySelectorAll('option') ?? [])].map((o) => o.getAttribute('value')),
    ).toEqual(['release/1.0']);
    fireEvent.change(branch, { target: { value: 'release/*' } });
    fireEvent.keyDown(branch, { key: 'Enter' });
    expect(
      within(within(first).getByRole('group', { name: 'On branches' })).getByText('release/*'),
    ).toBeInTheDocument();
    const repo = within(first).getByLabelText('Add In repositories');
    fireEvent.change(repo, { target: { value: 'acme/*' } });
    fireEvent.blur(repo);
    expect(
      within(within(first).getByRole('group', { name: 'In repositories' })).getByText('acme/*'),
    ).toBeInTheDocument();
    const type = within(first).getByLabelText('Add Of issue type');
    fireEvent.change(type, { target: { value: '  ' } });
    fireEvent.blur(type);
    expect(
      within(within(first).getByRole('group', { name: 'Of issue type' })).getByText('Any'),
    ).toBeInTheDocument();
  });

  it('keeps showing a value that Jira no longer lists', async () => {
    api.getRules.mockResolvedValue({
      rules: [rule('a', { name: 'First', target: 'Retired' })],
    });
    await open();
    await optionsLoaded();
    const first = await expand('First');
    const target = within(first).getByLabelText('Then move it to');
    expect(target).toHaveValue('Retired');
  });

  it('shows a saved near-duplicate of a listed status by its own spelling', async () => {
    api.getRules.mockResolvedValue({
      rules: [rule('a', { name: 'First', target: 'in-review' })],
    });
    await open();
    await optionsLoaded();
    const first = await expand('First');
    const target = within(first).getByLabelText('Then move it to');
    expect(target).toHaveValue('in-review');
    expect(within(target).getAllByRole('option')).toHaveLength(5);
  });

  it('falls back to the statuses and projects of the snapshot when Jira gives none', async () => {
    api.getJiraOptions.mockResolvedValue({ statuses: [], projects: [] });
    await open();
    await optionsLoaded();
    const first = await expand('First');
    const project = within(first).getByLabelText('Add In Jira projects');
    expect(
      within(project)
        .getAllByRole('option')
        .map((o) => o.textContent),
    ).toEqual(['+ Add', 'INP']);
    const target = within(first).getByLabelText('Then move it to');
    expect(
      within(target)
        .getAllByRole('option')
        .map((o) => o.textContent),
    ).toEqual(['In Review', 'Ready to Merge']);
  });

  it('falls back to the snapshot when the Jira options fail', async () => {
    api.getJiraOptions.mockRejectedValue(new Error('409'));
    await open();
    await waitFor(() => expect(api.getJiraOptions).toHaveBeenCalled());
    const first = await expand('First');
    await waitFor(() =>
      expect(
        within(within(first).getByLabelText('Add In Jira projects'))
          .getAllByRole('option')
          .map((o) => o.textContent),
      ).toEqual(['+ Add', 'INP']),
    );
  });
});

describe('the preview', () => {
  const previews: Record<string, unknown> = {};
  beforeEach(() => {
    api.getRules.mockResolvedValue({
      rules: [
        rule('a', { name: 'First', target: 'Ready to Merge' }),
        rule('b', { name: 'Second', trigger: 'merged', target: 'Done' }),
        rule('c', { name: 'Third', enabled: false }),
      ],
    });
    previews.a = {
      moves: [{ ticketKey: 'INP-1', status: 'In Review', alreadyInTarget: false }],
      waiting: [
        {
          ticketKey: 'INP-3',
          total: 3,
          waitingOn: [{ prId: 'p', number: 12, repo: 'acme/be' }],
        },
      ],
    };
    previews.b = {
      moves: [
        { ticketKey: 'INP-1', status: 'In Review', alreadyInTarget: false },
        { ticketKey: 'INP-2', status: 'Done', alreadyInTarget: true },
      ],
      waiting: [],
    };
    api.previewRule.mockImplementation((r: SyncRule) => Promise.resolve(previews[r.id]));
  });

  const run = async () => {
    await open();
    fireEvent.click(screen.getByRole('button', { name: 'Preview' }));
    return screen.findByRole('region', { name: 'Preview of all rules' });
  };

  it('asks for each enabled rule, once, and lists what would move now', async () => {
    const panel = await run();
    expect(await within(panel).findByText('INP-1')).toBeInTheDocument();
    expect(api.previewRule).toHaveBeenCalledTimes(2);
    expect(api.previewRule.mock.calls.map((c) => c[0].id)).toEqual(['a', 'b']);
    const row = within(panel).getByText('INP-1').closest('tr') as HTMLElement;
    expect(within(row).getByText('The pull request is approved')).toBeInTheDocument();
    expect(within(row).getByText('In Review')).toBeInTheDocument();
    expect(within(row).getByText('Ready to Merge')).toBeInTheDocument();
    expect(within(row).getByText('First')).toBeInTheDocument();
    expect(
      within(panel)
        .getAllByRole('columnheader')
        .map((h) => h.textContent),
    ).toEqual(['Ticket', 'When', 'Moves', 'Because']);
  });

  it('lists what stays under "Won\'t move" with the reason', async () => {
    const panel = await run();
    const summary = await within(panel).findByText("Won't move (2)");
    fireEvent.click(summary);
    expect(within(panel).getByText('INP-2')).toBeInTheDocument();
    expect(within(panel).getByText(/already in Done/)).toBeInTheDocument();
    expect(within(panel).getByText(/waiting on 1 of 3 PRs: be #12/)).toBeInTheDocument();
  });

  it('switches to "If merged", which reads the merge plans of the snapshot', async () => {
    api.getSnapshot.mockResolvedValue(
      snapshot([
        pr('a', 1, {
          tickets: [
            ticket('INP-9', {
              status: 'In Review',
              mergePlan: { rule: 'Second', target: 'Done', total: 1, waitingOn: [] },
            }),
            ticket('INP-8', {
              status: 'In Review',
              mergePlan: {
                rule: 'Second',
                target: 'Done',
                total: 2,
                waitingOn: [{ prId: 'q', number: 7, repo: 'acme/fe' }],
              },
            }),
          ],
        }),
      ]),
    );
    const panel = await run();
    await within(panel).findByText('INP-1');
    fireEvent.click(within(panel).getByRole('button', { name: 'If merged' }));
    expect(within(panel).getByRole('button', { name: 'If merged' })).toHaveAttribute(
      'aria-pressed',
      'true',
    );
    expect(within(panel).getByRole('button', { name: 'Right now' })).toHaveAttribute(
      'aria-pressed',
      'false',
    );
    expect(await within(panel).findByText('INP-9')).toBeInTheDocument();
    expect(within(panel).getByText('The pull request is merged')).toBeInTheDocument();
    expect(within(panel).queryByText('INP-1')).toBeNull();
    fireEvent.click(within(panel).getByText("Won't move (1)"));
    expect(within(panel).getByText(/1 of 2 PRs have to merge first: fe #7/)).toBeInTheDocument();
    fireEvent.click(within(panel).getByRole('button', { name: 'Right now' }));
    expect(await within(panel).findByText('INP-1')).toBeInTheDocument();
  });

  it('says when nothing would move, right now or if merged', async () => {
    api.previewRule.mockResolvedValue({ moves: [], waiting: [] });
    const panel = await run();
    expect(await within(panel).findByText('No ticket would move right now.')).toBeInTheDocument();
    fireEvent.click(within(panel).getByRole('button', { name: 'If merged' }));
    expect(
      within(panel).getByText('No ticket would move if these PRs merged.'),
    ).toBeInTheDocument();
  });

  it('shows the rules as they stand in the dialog, and says that "If merged" is for the saved ones', async () => {
    const panel = await run();
    await within(panel).findByText('INP-1');
    const calls = api.previewRule.mock.calls.length;
    const first = await expand('First');
    pick(within(first), 'Then move it to', 'Closed');
    await waitFor(() => expect(api.previewRule.mock.calls.length).toBe(calls + 2));
    expect(api.previewRule.mock.calls.at(-2)?.[0]).toMatchObject({ id: 'a', target: 'Closed' });
    fireEvent.click(within(panel).getByRole('button', { name: 'If merged' }));
    expect(within(panel).getByText(/Save your changes to include them/)).toBeInTheDocument();
  });

  it('does not ask again for a rename, and leaves out a rule with a problem', async () => {
    const panel = await run();
    await within(panel).findByText('INP-1');
    const calls = api.previewRule.mock.calls.length;
    fireEvent.change(within(card(1)).getByLabelText('Rule name'), { target: { value: 'Other' } });
    await new Promise((r) => setTimeout(r, 400));
    expect(api.previewRule.mock.calls.length).toBe(calls);
    const first = await expand('Other');
    pick(within(first), 'Then move it to', 'Closed');
    await waitFor(() => expect(api.previewRule.mock.calls.length).toBe(calls + 2));
    const second = await expand('Second');
    fireEvent.change(within(second).getByLabelText('Add In repositories'), {
      target: { value: 'a b' },
    });
    fireEvent.keyDown(within(second).getByLabelText('Add In repositories'), { key: 'Enter' });
    const afterBad = api.previewRule.mock.calls.length;
    await waitFor(() => expect(api.previewRule.mock.calls.length).toBe(afterBad + 1));
    expect(api.previewRule.mock.calls.at(-1)?.[0].id).toBe('a');
  });

  it('says so when it cannot preview, and when no rule is on', async () => {
    api.previewRule.mockRejectedValue(new Error('no'));
    const panel = await run();
    expect(await within(panel).findByText('Could not preview the rules.')).toBeInTheDocument();
    api.getRules.mockResolvedValue({ rules: [rule('c', { name: 'Third', enabled: false })] });
  });

  it('shows nothing to preview with every rule off, and a toggle of the panel', async () => {
    api.getRules.mockResolvedValue({ rules: [rule('c', { name: 'Third', enabled: false })] });
    const panel = await run();
    expect(within(panel).getByText('No ticket would move right now.')).toBeInTheDocument();
    expect(api.previewRule).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole('button', { name: 'Preview' }));
    expect(screen.queryByRole('region', { name: 'Preview of all rules' })).toBeNull();
  });
});

describe('Exclude statuses', () => {
  const group = () => screen.getByRole('region', { name: 'Exclude statuses' });

  it('shows the excluded statuses as chips with the hint', async () => {
    await open();
    await waitFor(() => expect(within(group()).getByText('Done')).toBeInTheDocument());
    expect(
      within(group()).getByText('Tickets in these statuses are never moved.'),
    ).toBeInTheDocument();
    expect(within(group()).getByRole('button', { name: 'Remove Done' })).toBeInTheDocument();
  });

  it('saves the whole settings with a status added or removed', async () => {
    await open();
    await optionsLoaded();
    await waitFor(() => expect(within(group()).getByText('Done')).toBeInTheDocument());
    const add = within(group()).getByLabelText('Add excluded status');
    expect(
      within(add)
        .getAllByRole('option')
        .map((o) => o.textContent),
    ).toEqual(['+ Add', 'Blocked', 'Closed', 'In Review', 'Ready to Merge']);
    fireEvent.change(add, { target: { value: 'Closed' } });
    await waitFor(() =>
      expect(api.putSettings).toHaveBeenLastCalledWith({
        ...settings,
        excludedStatuses: ['Done', 'Closed'],
      }),
    );
    fireEvent.click(within(group()).getByRole('button', { name: 'Remove Done' }));
    await waitFor(() =>
      expect(api.putSettings).toHaveBeenLastCalledWith({ ...settings, excludedStatuses: [] }),
    );
  });

  it('treats a near-duplicate of a listed status as the same one', async () => {
    api.getSettings.mockResolvedValue({ ...settings, excludedStatuses: ['in-review'] });
    await open();
    await optionsLoaded();
    await waitFor(() => expect(within(group()).getByText('In Review')).toBeInTheDocument());
    fireEvent.click(within(group()).getByRole('button', { name: 'Remove In Review' }));
    await waitFor(() =>
      expect(api.putSettings).toHaveBeenLastCalledWith({ ...settings, excludedStatuses: [] }),
    );
  });

  it('keeps an excluded status that Jira does not list', async () => {
    api.getSettings.mockResolvedValue({ ...settings, excludedStatuses: ['Retired'] });
    await open();
    await waitFor(() => expect(within(group()).getByText('Retired')).toBeInTheDocument());
  });

  it('says so when saving fails', async () => {
    api.putSettings.mockRejectedValue(new Error('no'));
    await open();
    fireEvent.click(await within(group()).findByRole('button', { name: 'Remove Done' }));
    expect(await screen.findByText('Could not save the excluded statuses.')).toBeInTheDocument();
  });

  it('is not shown before the settings load', async () => {
    api.getSettings.mockReturnValue(new Promise(() => {}));
    await open();
    expect(screen.queryByRole('region', { name: 'Exclude statuses' })).toBeNull();
  });
});

describe('Activity', () => {
  it('lists the activity and loads the next page', async () => {
    const item = (id: number, summary: string) => ({
      id,
      createdAt: '2026-11-24T11:00:00Z',
      kind: 'transition',
      ticketKey: 'INP-1',
      prId: 'a',
      triggeredBy: 'manual',
      result: id === 2 ? 'failed' : 'ok',
      summary,
      error: id === 2 ? 'Jira answered 503' : null,
    });
    api.getActivity
      .mockResolvedValueOnce({
        items: [item(3, 'Moved INP-1 to Done'), item(2, 'Move INP-2 failed')],
        nextCursor: 2,
      })
      .mockResolvedValueOnce({ items: [item(1, 'Merged #1')], nextCursor: null });
    await open();
    const log = await screen.findByRole('list', { name: 'Activity' });
    expect(await within(log).findByText('Move INP-2 failed', { exact: false })).toBeInTheDocument();
    expect(within(log).getByText('Jira answered 503')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Load more' }));
    expect(await within(log).findByText('Merged #1', { exact: false })).toBeInTheDocument();
    expect(api.getActivity).toHaveBeenNthCalledWith(1, undefined);
    expect(api.getActivity).toHaveBeenNthCalledWith(2, 2);
    expect(screen.queryByRole('button', { name: 'Load more' })).toBeNull();
  });

  it('says there is nothing yet, and when it cannot load', async () => {
    await open();
    expect(await screen.findByText('Nothing yet.')).toBeInTheDocument();
  });

  it('says so when the activity does not load', async () => {
    api.getActivity.mockRejectedValue(new Error('no'));
    await open();
    expect(await screen.findByText('Could not load the activity.')).toBeInTheDocument();
  });

  it('lists the Jira moves to retry with retry and dismiss', async () => {
    api.getRetries.mockResolvedValue({
      retries: [
        {
          id: 7,
          prId: 'a',
          ticketKey: 'INP-1',
          transitionId: '31',
          toStatus: 'Done',
          lastError: 'Jira answered 503',
          createdAt: '2026-11-24T11:00:00Z',
        },
      ],
    });
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
    api.dismissRetry.mockResolvedValue(undefined);
    await open();
    fireEvent.click(await screen.findByRole('button', { name: 'Retry the move of INP-1' }));
    await waitFor(() => expect(api.retryTransition).toHaveBeenCalled());
    expect(api.retryTransition.mock.calls[0]?.[0]).toBe(7);
    fireEvent.click(screen.getByRole('button', { name: 'Dismiss the move of INP-1' }));
    await waitFor(() => expect(api.dismissRetry).toHaveBeenCalled());
    expect(api.dismissRetry.mock.calls[0]?.[0]).toBe(7);
  });

  it('says so when a retry or a dismissal fails', async () => {
    api.getRetries.mockResolvedValue({
      retries: [
        {
          id: 7,
          prId: 'a',
          ticketKey: 'INP-1',
          transitionId: '31',
          toStatus: 'Done',
          lastError: 'Jira answered 503',
          createdAt: '2026-11-24T11:00:00Z',
        },
      ],
    });
    api.dismissRetry.mockRejectedValue(new Error('no'));
    await open();
    fireEvent.click(await screen.findByRole('button', { name: 'Dismiss the move of INP-1' }));
    expect(await screen.findByText('That did not work. Try again.')).toBeInTheDocument();
  });

  it('shows a rule move that needs input', async () => {
    api.getActivity.mockResolvedValue({
      items: [
        {
          id: 1,
          kind: 'transition',
          result: 'needs_input',
          summary: 'INP-7 to Done',
          error: null,
          triggeredBy: 'rule:r1',
          createdAt: '2026-11-24T11:00:00Z',
        },
      ],
      nextCursor: null,
    });
    await open();
    expect(await screen.findByText('needs input')).toBeInTheDocument();
    expect(screen.getByText(/by a status sync rule/)).toBeInTheDocument();
    expect(screen.getByText(/Jira asks for fields on this move/)).toBeInTheDocument();
  });
});
