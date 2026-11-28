import { fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { Marquee } from './Marquee';

function overflowing(el: HTMLElement) {
  Object.defineProperty(el, 'scrollWidth', { value: 300, configurable: true });
  Object.defineProperty(el, 'clientWidth', { value: 100, configurable: true });
}

describe('Marquee', () => {
  afterEach(() => vi.unstubAllGlobals());

  it('carries the full text as its title', () => {
    render(<Marquee title="A very long title">A very long title</Marquee>);
    expect(screen.getByTitle('A very long title')).toBeInTheDocument();
  });

  it('slides on hover and on focus of a control inside, and back on leave and blur', () => {
    vi.stubGlobal('matchMedia', (q: string) => ({ matches: q.includes('reduce'), media: q }));
    render(
      <Marquee title="t">
        <a href="https://example.com">link</a>
      </Marquee>,
    );
    const box = screen.getByTitle('t');
    overflowing(box);

    fireEvent.mouseEnter(box);
    expect(box.scrollLeft).toBe(200);
    expect(box).toHaveAttribute('data-sliding');
    fireEvent.mouseLeave(box);
    expect(box.scrollLeft).toBe(0);
    expect(box).not.toHaveAttribute('data-sliding');

    fireEvent.focus(screen.getByRole('link'));
    expect(box.scrollLeft).toBe(200);
    fireEvent.blur(screen.getByRole('link'));
    expect(box.scrollLeft).toBe(0);
  });

  it('does nothing when the text fits', () => {
    render(<Marquee title="t">short</Marquee>);
    const box = screen.getByTitle('t');
    fireEvent.mouseEnter(box);
    expect(box).not.toHaveAttribute('data-sliding');
  });
});
