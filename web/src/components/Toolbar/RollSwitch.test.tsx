import { act, fireEvent, render, screen } from '@testing-library/react';
import { useState } from 'react';
import { describe, expect, it, vi } from 'vitest';
import { ROLL_DEFAULT_HINT, RollSwitch, type DefaultChoice, type RollOption } from './RollSwitch';

const OPTIONS: RollOption<'a' | 'b' | 'c'>[] = [
  { id: 'a', label: 'Alpha' },
  { id: 'b', label: 'Beta' },
  { id: 'c', label: 'Gamma' },
];

function Harness({ onChange }: { onChange?: (v: string) => void }) {
  const [v, setV] = useState<'a' | 'b' | 'c'>('a');
  return (
    <RollSwitch
      name="Show"
      options={OPTIONS}
      value={v}
      count={{ a: 1, b: 2, c: 3 }[v]}
      onChange={(n) => {
        setV(n);
        onChange?.(n);
      }}
    />
  );
}

describe('RollSwitch', () => {
  it('draws a dot before the label of an option with a tone, and not before the others', () => {
    const options: RollOption<'a' | 'b' | 'c'>[] = [
      { id: 'a', label: 'Alpha' },
      { id: 'b', label: 'Beta', tone: 'green' },
      { id: 'c', label: 'Gamma', tone: 'red' },
    ];
    const dots = () => screen.getByRole('button').querySelectorAll('[class*="rollDot"]');
    const view = (value: 'a' | 'b' | 'c') => (
      <RollSwitch name="Show" options={options} value={value} onChange={() => {}} />
    );
    const { rerender } = render(view('a'));
    expect(dots()).toHaveLength(0);
    rerender(view('b'));
    expect(dots()).toHaveLength(1);
    expect(dots()[0]!.className).toMatch(/rollDot_green/);
    expect(dots()[0]).not.toHaveAttribute('style');
    rerender(view('c'));
    expect(dots()[0]!.className).toMatch(/rollDot_red/);
    // The dot is decoration: the name says the state in words.
    expect(screen.getByRole('button')).toHaveAccessibleName(/now Gamma/);
  });

  it('names the switch with its state and count', () => {
    render(<Harness />);
    expect(screen.getByRole('button', { name: /^Show, now Alpha, 1/ })).toBeInTheDocument();
  });

  it('cycles through the options and wraps around', () => {
    const onChange = vi.fn();
    render(<Harness onChange={onChange} />);
    const button = () => screen.getByRole('button');
    fireEvent.click(button());
    expect(button()).toHaveAccessibleName(/now Beta, 2/);
    fireEvent.click(button());
    expect(button()).toHaveAccessibleName(/now Gamma, 3/);
    fireEvent.click(button());
    expect(button()).toHaveAccessibleName(/now Alpha, 1/);
    expect(onChange.mock.calls.map((c) => c[0])).toEqual(['b', 'c', 'a']);
  });

  it('announces the new state, and nothing on load', () => {
    render(<Harness />);
    expect(screen.getByRole('status')).toBeEmptyDOMElement();
    fireEvent.click(screen.getByRole('button'));
    expect(screen.getByRole('status')).toHaveTextContent('Show: Beta');
  });

  it('shows the label, and the old one rolls out while the animation runs', () => {
    vi.useFakeTimers();
    try {
      render(<Harness />);
      fireEvent.click(screen.getByRole('button'));
      const words = screen.getByRole('button').textContent;
      expect(words).toContain('Beta');
      expect(words).toContain('Alpha');
      act(() => void vi.advanceTimersByTime(400));
      expect(screen.getByRole('button').textContent).not.toContain('Alpha');
    } finally {
      vi.useRealTimers();
    }
  });

  it('skips the rolling label under reduced motion', () => {
    vi.stubGlobal('matchMedia', (q: string) => ({ matches: q.includes('reduce'), media: q }));
    try {
      render(<Harness />);
      fireEvent.click(screen.getByRole('button'));
      expect(screen.getByRole('button').textContent).not.toContain('Alpha');
    } finally {
      vi.unstubAllGlobals();
    }
  });

  it('is a native button, so Enter and Space work', () => {
    render(<Harness />);
    expect(screen.getByRole('button').tagName).toBe('BUTTON');
    expect(screen.getByRole('button')).toHaveAttribute('type', 'button');
  });
});

type Pick = 'default' | 'x' | 'y' | 'z';

const PICKS: RollOption<Pick>[] = [
  { id: 'default', label: 'Use default (Y)' },
  { id: 'x', label: 'X' },
  { id: 'y', label: 'Y' },
  { id: 'z', label: 'Z' },
];

function WithDefault({
  start = 'default',
  choice = { id: 'default', resolvesTo: 'y' },
  onChange,
}: {
  start?: Pick;
  choice?: DefaultChoice<Pick>;
  onChange?: (v: string) => void;
}) {
  const [v, setV] = useState<Pick>(start);
  return (
    <RollSwitch
      name="Pick"
      options={PICKS}
      value={v}
      defaultChoice={choice}
      onChange={(n) => {
        setV(n);
        onChange?.(n);
      }}
    />
  );
}

describe('RollSwitch with a default', () => {
  const button = () => screen.getByRole('button');

  it('turns through the explicit options only and never lands on the default', () => {
    const onChange = vi.fn();
    render(<WithDefault start="x" onChange={onChange} />);
    for (let i = 0; i < 4; i++) fireEvent.click(button());
    expect(onChange.mock.calls.map((c) => c[0])).toEqual(['y', 'z', 'x', 'y']);
  });

  it('goes from the default to the explicit option after the one the default resolves to', () => {
    const onChange = vi.fn();
    render(<WithDefault onChange={onChange} />);
    fireEvent.click(button());
    expect(onChange).toHaveBeenLastCalledWith('z');
  });

  it('starts at the first explicit option when the default resolves to none, and wraps from the last', () => {
    const onChange = vi.fn();
    const { unmount } = render(<WithDefault choice={{ id: 'default' }} onChange={onChange} />);
    fireEvent.click(button());
    expect(onChange).toHaveBeenLastCalledWith('x');
    unmount();
    render(<WithDefault choice={{ id: 'default', resolvesTo: 'z' }} onChange={onChange} />);
    fireEvent.click(button());
    expect(onChange).toHaveBeenLastCalledWith('x');
  });

  it('switches the default on and off with a right-click, back to the remembered option', () => {
    const onChange = vi.fn();
    render(
      <WithDefault
        start="z"
        choice={{ id: 'default', resolvesTo: 'y', remembered: 'z' }}
        onChange={onChange}
      />,
    );
    const notPrevented = fireEvent.contextMenu(button());
    expect(notPrevented).toBe(false);
    expect(onChange).toHaveBeenLastCalledWith('default');
    expect(button()).toHaveAccessibleName(/now Use default \(Y\)/);
    fireEvent.contextMenu(button());
    expect(onChange).toHaveBeenLastCalledWith('z');
    expect(onChange).toHaveBeenCalledTimes(2);
  });

  it('goes back to where the default resolves to when nothing is remembered', () => {
    const onChange = vi.fn();
    render(<WithDefault onChange={onChange} />);
    fireEvent.contextMenu(button());
    expect(onChange).toHaveBeenLastCalledWith('y');
  });

  it('does the same with Shift+click, which is what Shift+Enter and Shift+Space send', () => {
    const onChange = vi.fn();
    render(<WithDefault start="x" onChange={onChange} />);
    fireEvent.click(button(), { shiftKey: true });
    expect(onChange).toHaveBeenLastCalledWith('default');
    fireEvent.click(button(), { shiftKey: true });
    expect(onChange).toHaveBeenLastCalledWith('y');
    expect(onChange).toHaveBeenCalledTimes(2);
  });

  it('explains both gestures in the tooltip and the accessible name', () => {
    render(<WithDefault />);
    expect(button()).toHaveAttribute('title', ROLL_DEFAULT_HINT);
    expect(button()).toHaveAccessibleName(/^Pick, now Use default \(Y\)\. Click: next choice\./);
    expect(ROLL_DEFAULT_HINT).toBe(
      'Click: next choice. Right-click, Shift+click or Shift+Enter: use default or choose',
    );
  });

  it('announces the state it goes to', () => {
    render(<WithDefault />);
    fireEvent.contextMenu(button());
    expect(screen.getByRole('status')).toHaveTextContent('Pick: Y');
  });

  it('leaves a switch without a default as it was: Shift+click and right-click change nothing', () => {
    const onChange = vi.fn();
    render(<Harness onChange={onChange} />);
    expect(screen.getByRole('button')).not.toHaveAttribute('title');
    expect(fireEvent.contextMenu(screen.getByRole('button'))).toBe(true);
    expect(onChange).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole('button'), { shiftKey: true });
    expect(onChange).toHaveBeenLastCalledWith('b');
  });
});
