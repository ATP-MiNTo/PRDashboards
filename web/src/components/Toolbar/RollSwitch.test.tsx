import { act, fireEvent, render, screen } from '@testing-library/react';
import { useState } from 'react';
import { describe, expect, it, vi } from 'vitest';
import { RollSwitch, type RollOption } from './RollSwitch';

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
