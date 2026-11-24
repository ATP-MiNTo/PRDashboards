import { fireEvent, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { MULTI_PR_HINT, newRule } from '@/lib/rules';
import { renderWithClient } from '@/test-utils';

const api = vi.hoisted(() => ({ getRules: vi.fn(), putRules: vi.fn() }));

vi.mock('@/api/client', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/api/client')>()),
  ...api,
}));

import { MultiPrSection } from './MultiPrSection';

const rule = (id: string, mode: 'all' | 'any') => ({
  ...newRule(),
  id,
  name: `Rule ${id}`,
  target: 'Done',
  mode,
});

const lastMode = () =>
  (
    api.putRules.mock.calls.at(-1)?.[0] as { rules: { mode: string }[]; baseline: boolean }
  ).rules.map((r) => r.mode);

beforeEach(() => {
  Object.values(api).forEach((f) => f.mockReset());
  api.getRules.mockResolvedValue({ rules: [rule('a', 'all'), rule('b', 'all')] });
  api.putRules.mockImplementation((u: { rules: unknown[] }) => Promise.resolve({ rules: u.rules }));
});

describe('MultiPrSection', () => {
  it('explains itself with the exact hint, as a tooltip and an accessible description', async () => {
    renderWithClient(<MultiPrSection />);
    const info = await screen.findByRole('button', { name: 'About multi-PR tickets' });
    const tip = screen.getByRole('tooltip');
    expect(tip).toHaveTextContent(MULTI_PR_HINT);
    expect(info).toHaveAccessibleDescription(MULTI_PR_HINT);
    expect(MULTI_PR_HINT).toBe(
      'When a ticket is linked to several PRs. All: the ticket moves only when every linked PR meets the rule. Any: the first PR that meets it moves the ticket. Left-click for All, right-click for Any.',
    );
  });

  it('starts at All with a pressed state and a label that names the mode', async () => {
    renderWithClient(<MultiPrSection />);
    const chip = await screen.findByRole('button', { name: 'Rule a: All' });
    expect(chip).toHaveAttribute('aria-pressed', 'true');
  });

  it('sets Any on right-click without opening the browser menu, and All on left-click', async () => {
    renderWithClient(<MultiPrSection />);
    const chip = await screen.findByRole('button', { name: 'Rule a: All' });
    const allowed = fireEvent.contextMenu(chip);
    expect(allowed).toBe(false);
    await waitFor(() => expect(lastMode()).toEqual(['any', 'all']));
    expect(api.putRules.mock.calls.at(-1)?.[0]).toMatchObject({ baseline: true });
    const any = await screen.findByRole('button', { name: 'Rule a: Any' });
    expect(any).toHaveAttribute('aria-pressed', 'false');
    fireEvent.click(any);
    await waitFor(() => expect(lastMode()).toEqual(['all', 'all']));
  });

  it('sets All on Enter and Any on Shift+Enter', async () => {
    api.getRules.mockResolvedValue({ rules: [rule('a', 'any')] });
    renderWithClient(<MultiPrSection />);
    const chip = await screen.findByRole('button', { name: 'Rule a: Any' });
    fireEvent.keyDown(chip, { key: 'Enter' });
    await waitFor(() => expect(lastMode()).toEqual(['all']));
    fireEvent.keyDown(await screen.findByRole('button', { name: 'Rule a: All' }), {
      key: 'Enter',
      shiftKey: true,
    });
    await waitFor(() => expect(lastMode()).toEqual(['any']));
  });

  it('does not save when the mode is already set', async () => {
    renderWithClient(<MultiPrSection />);
    fireEvent.click(await screen.findByRole('button', { name: 'Rule a: All' }));
    expect(api.putRules).not.toHaveBeenCalled();
  });

  it('leaves the context menu alone outside a chip', async () => {
    renderWithClient(<MultiPrSection />);
    await screen.findByRole('button', { name: 'Rule a: All' });
    expect(fireEvent.contextMenu(screen.getByRole('heading', { name: 'Multi-PR tickets' }))).toBe(
      true,
    );
  });
});
