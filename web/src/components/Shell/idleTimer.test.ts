import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { createIdleTimer } from './idleTimer';

beforeEach(() => vi.useFakeTimers());
afterEach(() => vi.useRealTimers());

describe('createIdleTimer', () => {
  const make = (held = () => false) => {
    const onIdle = vi.fn();
    const timer = createIdleTimer({ delayMs: 30_000, onIdle, isHeld: held });
    return { onIdle, timer };
  };

  it('fires once after the delay', () => {
    const { onIdle, timer } = make();
    timer.arm();
    expect(timer.isArmed()).toBe(true);
    vi.advanceTimersByTime(29_999);
    expect(onIdle).not.toHaveBeenCalled();
    vi.advanceTimersByTime(1);
    expect(onIdle).toHaveBeenCalledTimes(1);
    expect(timer.isArmed()).toBe(false);
    vi.advanceTimersByTime(60_000);
    expect(onIdle).toHaveBeenCalledTimes(1);
  });

  it('does not fire after cancel', () => {
    const { onIdle, timer } = make();
    timer.arm();
    vi.advanceTimersByTime(20_000);
    timer.cancel();
    vi.advanceTimersByTime(60_000);
    expect(onIdle).not.toHaveBeenCalled();
    expect(timer.isArmed()).toBe(false);
  });

  it('arming again restarts the delay', () => {
    const { onIdle, timer } = make();
    timer.arm();
    vi.advanceTimersByTime(20_000);
    timer.arm();
    vi.advanceTimersByTime(20_000);
    expect(onIdle).not.toHaveBeenCalled();
    vi.advanceTimersByTime(10_000);
    expect(onIdle).toHaveBeenCalledTimes(1);
  });

  it('starts over while held and fires once released', () => {
    let held = true;
    const { onIdle, timer } = make(() => held);
    timer.arm();
    vi.advanceTimersByTime(30_000);
    expect(onIdle).not.toHaveBeenCalled();
    expect(timer.isArmed()).toBe(true);
    held = false;
    vi.advanceTimersByTime(30_000);
    expect(onIdle).toHaveBeenCalledTimes(1);
  });
});
