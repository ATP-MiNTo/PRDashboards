import { act, fireEvent, render, screen, within } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { Toolbar, type ToolbarProps } from './Toolbar';

function base(): ToolbarProps {
  return {
    sort: 'updated',
    reversed: false,
    onSort: vi.fn(),
    onReverse: vi.fn(),
    show: 'all',
    onShow: vi.fn(),
    showCounts: { all: 5, mine: 3, review: 2 },
    mode: 'visible',
    onMode: vi.fn(),
    counts: { visible: 4, all: 5, hidden: 1 },
  };
}

function setup(over: Partial<ToolbarProps> = {}) {
  const props = { ...base(), ...over };
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
    const show = screen.getByRole('button', { name: /^Show, now All, 5/ });
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
    expect(tip).toHaveTextContent('All Everything.');
    expect(tip).toHaveTextContent('Mine Assigned to you or opened by you.');
    expect(tip).toHaveTextContent('To review Waiting on your review.');
  });

  it('ends with the legend of the role colours and the stack mark', () => {
    setup();
    const legend = screen.getByRole('list', { name: 'Legend' });
    expect(
      within(legend)
        .getAllByRole('listitem')
        .map((li) => li.textContent),
    ).toEqual(['Assigned', 'Opened by me', 'Review requested', 'Stack']);
  });

  it('gives Assigned and Opened by me the same green dot and Review requested the red one', () => {
    setup();
    const dot = (name: string) =>
      within(screen.getByRole('list', { name: 'Legend' }))
        .getByText(name)
        .querySelector('span')!.className;
    expect(dot('Assigned')).toMatch(/dot_assigned/);
    expect(dot('Opened by me')).toMatch(/dot_authored/);
    expect(dot('Review requested')).toMatch(/dot_reviewer/);
  });

  it.each([
    ['all', /^Show, now All, 5\./, null],
    ['mine', /^Show, now Mine, 3\./, /rollDot_green/],
    ['review', /^Show, now To review, 2\./, /rollDot_red/],
  ] as const)('names the %s mode and draws its dot only when it is coloured', (show, name, dot) => {
    setup({ show });
    const button = screen.getByRole('button', { name });
    const dots = button.querySelectorAll('[class*="rollDot"]');
    if (dot === null) expect(dots).toHaveLength(0);
    else {
      expect(dots).toHaveLength(1);
      expect(dots[0]!.className).toMatch(dot);
    }
  });

  it('cycles All, Mine, To review and back with a click', () => {
    const onShow = vi.fn();
    const { rerender } = render(<Toolbar {...base()} show="all" onShow={onShow} />);
    const steps = [
      ['all', 'mine'],
      ['mine', 'review'],
      ['review', 'all'],
    ] as const;
    for (const [now, next] of steps) {
      rerender(<Toolbar {...base()} show={now} onShow={onShow} />);
      fireEvent.click(screen.getByRole('button', { name: /^Show, now/ }));
      expect(onShow).toHaveBeenLastCalledWith(next);
    }
  });
});
