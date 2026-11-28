import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { Tip, placeTip } from './Tip';

describe('Tip', () => {
  it('carries the text as data-tip around its control, which stays reachable', () => {
    render(
      <Tip tip="Merge after #1">
        <button type="button">Merge</button>
      </Tip>,
    );
    expect(screen.getByRole('button', { name: 'Merge' }).parentElement).toHaveAttribute(
      'data-tip',
      'Merge after #1',
    );
  });

  it('can sit on the left', () => {
    const { container } = render(
      <Tip tip="x" side="left">
        <span>a</span>
      </Tip>,
    );
    expect(container.firstElementChild?.className).toMatch(/left/);
  });
});

describe('Tip placement', () => {
  const rect = (left: number, width: number) => ({ left, right: left + width, width }) as DOMRect;

  function place(left: number, width: number, side: 'top' | 'left' = 'top', viewport = 390) {
    const el = document.createElement('span');
    el.getBoundingClientRect = () => rect(left, width);
    placeTip(el, side, viewport);
    return el;
  }

  it('centres the bubble when there is room on both sides', () => {
    const el = place(500, 80, 'top', 1000);
    expect(el.dataset.align).toBe('center');
  });

  it('grows inwards near the left and right edges and never wider than the room', () => {
    const l = place(4, 32);
    expect(l.dataset.align).toBe('start');
    expect(l.style.getPropertyValue('--tip-max')).toBe('320px');
    const r = place(350, 32);
    expect(r.dataset.align).toBe('end');
    expect(parseFloat(r.style.getPropertyValue('--tip-max'))).toBeLessThanOrEqual(320);
    const narrow = place(300, 30);
    expect(narrow.dataset.align).toBe('end');
    expect(narrow.style.getPropertyValue('--tip-max')).toBe('320px');
  });

  it('limits a left bubble to the room on the left', () => {
    const el = place(100, 30, 'left');
    expect(el.style.getPropertyValue('--tip-max')).toBe('92px');
  });

  it('places the bubble when the pointer enters or a control inside gets focus', () => {
    render(
      <Tip tip="x">
        <button type="button">a</button>
      </Tip>,
    );
    const wrap = screen.getByRole('button').parentElement!;
    wrap.getBoundingClientRect = () => rect(2, 20);
    fireEvent.pointerEnter(wrap);
    expect(wrap.dataset.align).toBe('start');
  });

  it('keeps a hidden bubble out of the layout, so it adds nothing to the scroll width', () => {
    const css = readFileSync(resolve(__dirname, 'Tip.module.css'), 'utf8');
    const base = /\.tip::before,\s*\.tip::after\s*\{([^}]*)\}/.exec(css)![1]!;
    expect(base).toMatch(/display:\s*none/);
    expect(base).not.toMatch(/opacity/);
  });
});
