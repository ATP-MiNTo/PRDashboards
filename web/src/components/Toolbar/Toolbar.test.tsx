import { act, fireEvent, render, screen, within } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { Toolbar, type ToolbarProps } from './Toolbar';

function setup(over: Partial<ToolbarProps> = {}) {
  const props: ToolbarProps = {
    sort: 'updated',
    reversed: false,
    onSort: vi.fn(),
    onReverse: vi.fn(),
    show: 'everything',
    onShow: vi.fn(),
    showCounts: { everything: 5, assigned: 3, authored: 2 },
    mode: 'visible',
    onMode: vi.fn(),
    counts: { visible: 4, all: 5, hidden: 1 },
    ...over,
  };
  render(<Toolbar {...props} />);
  return props;
}

describe('Toolbar', () => {
  it('offers the five sort options, with Size of change', () => {
    setup();
    const select = screen.getByRole('combobox', { name: 'Sort' });
    expect(
      within(select)
        .getAllByRole('option')
        .map((o) => o.textContent),
    ).toEqual([
      'Recently updated',
      'Title',
      'Repository',
      'Size of change',
      'Ready to merge first',
    ]);
  });

  it('reports a sort choice', () => {
    const p = setup();
    fireEvent.change(screen.getByRole('combobox', { name: 'Sort' }), { target: { value: 'size' } });
    expect(p.onSort).toHaveBeenCalledWith('size');
  });

  it('swings the arrow only after it was clicked, and points it down when reversed', () => {
    const p = setup({ reversed: true });
    const arrow = () => document.querySelector('svg g')!.getAttribute('class')!;
    expect(arrow()).toMatch(/arrowDown/);
    expect(arrow()).not.toMatch(/arrowSwing/);
    fireEvent.click(screen.getByRole('button', { name: 'Reverse sort order' }));
    expect(p.onReverse).toHaveBeenCalledOnce();
    expect(arrow()).toMatch(/arrowSwing/);
  });

  it('points the arrow up in the natural order', () => {
    setup();
    expect(document.querySelector('svg g')!.getAttribute('class')).toMatch(/arrowUp/);
  });

  it('shows the two roll switches with their counts, Show before Hidden PRs', () => {
    setup();
    const show = screen.getByRole('button', { name: /^Show, now Everything, 5/ });
    const hidden = screen.getByRole('button', { name: /^Hidden PRs, now Visible, 4/ });
    expect(show.compareDocumentPosition(hidden) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it('says in both hints how to switch to the next state', () => {
    setup();
    for (const label of ['Show', 'Hidden PRs']) {
      const trigger = screen.getByText(label, { selector: 'span[tabindex]' });
      act(() => trigger.focus());
      expect(screen.getByRole('tooltip')).toHaveTextContent(
        'Click the button to switch to the next state.',
      );
      act(() => trigger.blur());
    }
  });

  it('explains each Show state in its hint', () => {
    setup();
    act(() => screen.getByText('Show', { selector: 'span[tabindex]' }).focus());
    const tip = screen.getByRole('tooltip');
    expect(tip).toHaveTextContent('Everything Assigned to you and opened by you.');
    expect(tip).toHaveTextContent('Assigned to me Assigned to you.');
    expect(tip).toHaveTextContent('Opened by me Opened by you.');
  });

  it('ends with the legend of the role colours and the stack mark', () => {
    setup();
    const legend = screen.getByRole('list', { name: 'Legend' });
    expect(
      within(legend)
        .getAllByRole('listitem')
        .map((li) => li.textContent),
    ).toEqual(['Assigned', 'Opened by me', 'Stack']);
  });
});
