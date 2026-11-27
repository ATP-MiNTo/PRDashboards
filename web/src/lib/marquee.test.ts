import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { slideBack, slideDuration, slideOut, slideTo, stopSlide } from './marquee';

function line(scrollWidth: number, clientWidth: number): HTMLElement {
  const el = document.createElement('span');
  Object.defineProperty(el, 'scrollWidth', { value: scrollWidth });
  Object.defineProperty(el, 'clientWidth', { value: clientWidth });
  return el;
}

function reduce(on: boolean) {
  vi.stubGlobal(
    'matchMedia',
    vi.fn().mockReturnValue({ matches: on, addEventListener() {}, removeEventListener() {} }),
  );
}

describe('slideDuration', () => {
  it('goes out at 45px per second, but never in under 0.8s', () => {
    expect(slideDuration(90, true)).toBe(2000);
    expect(slideDuration(10, true)).toBe(800);
  });

  it('comes back at 12ms per pixel, but never in under 0.5s', () => {
    expect(slideDuration(100, false)).toBe(1200);
    expect(slideDuration(10, false)).toBe(500);
  });
});

describe('sliding', () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ['requestAnimationFrame', 'cancelAnimationFrame', 'performance'] });
    reduce(false);
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it('does nothing for text that fits', () => {
    const el = line(100, 100);
    slideOut(el);
    expect(el).not.toHaveAttribute('data-sliding');
    expect(el.scrollLeft).toBe(0);
  });

  it('slides to the end and marks the line while it is away from the start', () => {
    const el = line(300, 100);
    slideOut(el);
    expect(el).toHaveAttribute('data-sliding');
    vi.advanceTimersByTime(10_000);
    expect(el.scrollLeft).toBe(200);
    expect(el).toHaveAttribute('data-sliding');
  });

  it('slides back to the start and clears the mark', () => {
    const el = line(300, 100);
    slideOut(el);
    vi.advanceTimersByTime(10_000);
    slideBack(el);
    vi.advanceTimersByTime(10_000);
    expect(el.scrollLeft).toBe(0);
    expect(el).not.toHaveAttribute('data-sliding');
  });

  it('turns around from where it is when the pointer leaves half way', () => {
    const el = line(300, 100);
    slideOut(el);
    vi.advanceTimersByTime(1000);
    const mid = el.scrollLeft;
    expect(mid).toBeGreaterThan(0);
    expect(mid).toBeLessThan(200);
    slideBack(el);
    vi.advanceTimersByTime(10_000);
    expect(el.scrollLeft).toBe(0);
  });

  it('leaves a line that never moved alone when the pointer leaves', () => {
    const el = line(300, 100);
    slideBack(el);
    expect(el.scrollLeft).toBe(0);
  });

  it('stops where it is', () => {
    const el = line(300, 100);
    slideOut(el);
    vi.advanceTimersByTime(1000);
    const at = el.scrollLeft;
    stopSlide(el);
    vi.advanceTimersByTime(10_000);
    expect(el.scrollLeft).toBe(at);
  });

  it('jumps under reduced motion', () => {
    reduce(true);
    const el = line(300, 100);
    slideOut(el);
    expect(el.scrollLeft).toBe(200);
    slideTo(el, 0);
    expect(el.scrollLeft).toBe(0);
    expect(el).not.toHaveAttribute('data-sliding');
  });
});
