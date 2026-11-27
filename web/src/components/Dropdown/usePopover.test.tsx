import { render, screen } from '@testing-library/react';
import { useRef } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { usePopover, type PopoverPlacement } from './usePopover';

function Harness({ placement }: { placement: PopoverPlacement }) {
  const anchor = useRef<HTMLButtonElement>(null);
  const { place } = usePopover(true, anchor, () => {}, placement);
  return (
    <>
      <button ref={anchor} type="button">
        anchor
      </button>
      <div ref={place} data-testid="pop" />
    </>
  );
}

const rect = (r: Partial<DOMRect>) =>
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (
    this: HTMLElement,
  ) {
    return this.tagName === 'BUTTON'
      ? ({ top: 100, bottom: 120, left: 300, right: 360, width: 60, height: 20, ...r } as DOMRect)
      : ({ top: 0, bottom: 0, left: 0, right: 0, width: 0, height: 0 } as DOMRect);
  });

const size = (w: number, h: number) => {
  vi.spyOn(HTMLElement.prototype, 'offsetWidth', 'get').mockReturnValue(w);
  vi.spyOn(HTMLElement.prototype, 'offsetHeight', 'get').mockReturnValue(h);
};

afterEach(() => vi.restoreAllMocks());

describe('usePopover placement', () => {
  it('goes below the anchor at its left edge, at least as wide, by default', () => {
    rect({});
    size(100, 50);
    render(<Harness placement={{}} />);
    const pop = screen.getByTestId('pop');
    expect(pop.style.top).toBe('122px');
    expect(pop.style.left).toBe('300px');
    expect(pop.style.minWidth).toBe('60px');
  });

  it('can line up with the right edge, with a gap and no minimum width', () => {
    rect({});
    size(100, 50);
    render(<Harness placement={{ align: 'end', gap: 6, matchWidth: false }} />);
    const pop = screen.getByTestId('pop');
    expect(pop.style.left).toBe('260px');
    expect(pop.style.top).toBe('126px');
    expect(pop.style.minWidth).toBe('');
  });

  it('goes above when asked, or when auto and there is no room below', () => {
    rect({ top: 700, bottom: 720 });
    size(100, 80);
    const { unmount } = render(<Harness placement={{ side: 'above', gap: 4 }} />);
    expect(screen.getByTestId('pop').style.top).toBe('616px');
    unmount();
    render(<Harness placement={{ side: 'auto', gap: 4 }} />);
    expect(screen.getByTestId('pop').style.top).toBe('616px');
  });

  it('stays below when auto and there is room', () => {
    rect({});
    size(100, 80);
    render(<Harness placement={{ side: 'auto' }} />);
    expect(screen.getByTestId('pop').style.top).toBe('122px');
  });

  it('is kept inside the viewport sideways', () => {
    rect({ left: 1000, right: 1060 });
    size(200, 50);
    render(<Harness placement={{}} />);
    expect(Number.parseInt(screen.getByTestId('pop').style.left, 10)).toBeLessThanOrEqual(
      window.innerWidth - 200 - 8,
    );
    vi.restoreAllMocks();
    rect({ left: -50, right: 10, width: 60 });
    size(200, 50);
    render(<Harness placement={{ align: 'end' }} />);
    expect(screen.getAllByTestId('pop')[1]?.style.left).toBe('8px');
  });
});
