import { afterEach, describe, expect, it, vi } from 'vitest';
import { applyMode, crossfade } from './transition';

interface Fade {
  startViewTransition?: (update: () => void) => unknown;
}

/** A document whose startViewTransition runs the update later, like a browser does. */
function docWith(fade: Fade['startViewTransition']): Document {
  const doc = document.implementation.createHTMLDocument('t');
  (doc as unknown as Fade).startViewTransition = fade;
  return doc;
}

afterEach(() => {
  delete document.documentElement.dataset.mode;
  delete document.documentElement.dataset.theme;
});

describe('crossfade', () => {
  it('runs the change inside a view transition', () => {
    const start = vi.fn((update: () => void) => update());
    const change = vi.fn();
    crossfade(docWith(start), change, false);
    expect(start).toHaveBeenCalledTimes(1);
    expect(change).toHaveBeenCalledTimes(1);
  });

  it('ignores a transition that the browser skips', async () => {
    const change = vi.fn();
    const skipped = Promise.reject(new DOMException('Transition was skipped', 'AbortError'));
    crossfade(
      docWith((update) => {
        update();
        return { ready: skipped };
      }),
      change,
      false,
    );
    await expect(skipped).rejects.toThrow('skipped');
    expect(change).toHaveBeenCalledTimes(1);
  });

  it('skips the transition when motion is reduced', () => {
    const start = vi.fn();
    const change = vi.fn();
    crossfade(docWith(start), change, true);
    expect(start).not.toHaveBeenCalled();
    expect(change).toHaveBeenCalledTimes(1);
  });

  it('changes at once when the browser has no View Transitions', () => {
    const change = vi.fn();
    crossfade(docWith(undefined), change, false);
    expect(change).toHaveBeenCalledTimes(1);
  });
});

describe('applyMode', () => {
  const dark = { mode: 'dark', theme: 'night' };
  const light = { mode: 'light', theme: 'winter' };

  it('applies the first mode without a transition', () => {
    const start = vi.fn();
    const doc = docWith(start);
    applyMode(doc, dark, false);
    expect(start).not.toHaveBeenCalled();
    expect(doc.documentElement.dataset.mode).toBe('dark');
    expect(doc.documentElement.dataset.theme).toBe('night');
  });

  it('cross-fades a change between light and dark', () => {
    const start = vi.fn((update: () => void) => update());
    const doc = docWith(start);
    applyMode(doc, light, false);
    applyMode(doc, dark, false);
    expect(start).toHaveBeenCalledTimes(1);
    expect(doc.documentElement.dataset.mode).toBe('dark');
    expect(doc.documentElement.dataset.theme).toBe('night');
  });

  it('does not transition when the mode stays', () => {
    const start = vi.fn((update: () => void) => update());
    const doc = docWith(start);
    applyMode(doc, light, false);
    applyMode(doc, light, false);
    expect(start).not.toHaveBeenCalled();
  });

  it('still switches under reduced motion', () => {
    const start = vi.fn();
    const doc = docWith(start);
    applyMode(doc, light, true);
    applyMode(doc, dark, true);
    expect(start).not.toHaveBeenCalled();
    expect(doc.documentElement.dataset.mode).toBe('dark');
  });
});
