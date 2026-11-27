import { act, render, screen } from '@testing-library/react';
import { renderToStaticMarkup } from 'react-dom/server';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fabPlacement, Footer, IDLE_MS } from './Footer';

describe('fabPlacement', () => {
  const slot = { left: 900, top: 760, width: 28 };
  const view = { width: 1000, height: 800 };

  it('floats at the bottom right while the footer is below the fold', () => {
    const p = fabPlacement({ top: 900, bottom: 936 }, slot, view);
    expect(p).toEqual({ snapped: false, x: 940, y: 736, size: 44 });
  });

  it('snaps into the slot once the footer is fully visible', () => {
    const p = fabPlacement({ top: 764, bottom: 800 }, slot, view);
    expect(p).toEqual({ snapped: true, x: 900, y: 760, size: 28 });
  });

  it('does not snap while the footer is cut off at the bottom', () => {
    expect(fabPlacement({ top: 770, bottom: 806 }, slot, view).snapped).toBe(false);
  });
});

describe('Footer', () => {
  it('renders no style attribute in the exported markup', () => {
    expect(renderToStaticMarkup(<Footer />)).not.toMatch(/style=/);
  });

  it('has the credit and a GitHub link that opens safely', () => {
    render(<Footer />);
    expect(screen.getByText(/Made with/)).toHaveTextContent('Made with 💚 by ATP-MiNTo');
    const link = screen.getByRole('link', { name: 'GitHub repository' });
    expect(link).toHaveAttribute('href', 'https://github.com/ATP-MiNTo/PRDashboards');
    expect(link).toHaveAttribute('target', '_blank');
    expect(link).toHaveAttribute('rel', 'noopener noreferrer');
  });

  it('is a footer landmark', () => {
    render(<Footer />);
    expect(screen.getByRole('contentinfo')).toBeInTheDocument();
  });
});

describe('Footer floating button', () => {
  let footerRect = { top: 900, bottom: 936 };
  beforeEach(() => {
    vi.useFakeTimers({
      toFake: ['setTimeout', 'clearTimeout', 'requestAnimationFrame', 'cancelAnimationFrame'],
    });
    footerRect = { top: 900, bottom: 936 };
    vi.stubGlobal('innerHeight', 800);
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (
      this: HTMLElement,
    ) {
      const r = this.tagName === 'FOOTER' ? footerRect : { top: 764, bottom: 792 };
      return { ...r, left: 900, right: 928, width: 28, height: 28, x: 900, y: r.top, toJSON() {} };
    });
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  const link = () => screen.getByRole('link', { name: 'GitHub repository' });

  it('sets its position through custom properties', () => {
    render(<Footer />);
    expect(link().style.getPropertyValue('--fab-size')).toBe('44px');
  });

  it('snaps into the footer slot when it scrolls into view and releases when scrolled up', () => {
    render(<Footer />);
    act(() => void vi.advanceTimersByTime(20));
    expect(link().className).not.toMatch(/snapped/);
    footerRect = { top: 764, bottom: 800 };
    act(() => {
      window.dispatchEvent(new Event('scroll'));
      vi.advanceTimersByTime(20);
    });
    expect(link().className).toMatch(/snapped/);
    expect(link().style.getPropertyValue('--fab-size')).toBe('28px');
    footerRect = { top: 900, bottom: 936 };
    act(() => {
      window.dispatchEvent(new Event('scroll'));
      vi.advanceTimersByTime(20);
    });
    expect(link().className).not.toMatch(/snapped/);
  });

  it('hides while scrolling and returns after the idle time', () => {
    render(<Footer />);
    act(() => void vi.advanceTimersByTime(20));
    act(() => void window.dispatchEvent(new Event('scroll')));
    expect(link().className).toMatch(/away/);
    act(() => void vi.advanceTimersByTime(IDLE_MS + 20));
    expect(link().className).not.toMatch(/away/);
  });

  it('stays put while scrolling once it is in the footer', () => {
    footerRect = { top: 764, bottom: 800 };
    render(<Footer />);
    act(() => void vi.advanceTimersByTime(20));
    act(() => void window.dispatchEvent(new Event('scroll')));
    expect(link().className).not.toMatch(/away/);
  });
});
