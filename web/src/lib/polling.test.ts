import { describe, expect, it } from 'vitest';
import { POLLING_STEPS, pollingLabel, stepPolling, type PollingStep } from './polling';

describe('stepPolling', () => {
  it('goes up and down through the allowed values', () => {
    expect(stepPolling(60, 1)).toBe(120);
    expect(stepPolling(120, 1)).toBe(300);
    expect(stepPolling(300, 1)).toBe(900);
    expect(stepPolling(900, 1)).toBe(3600);
    expect(stepPolling(3600, -1)).toBe(900);
    expect(stepPolling(120, -1)).toBe(60);
  });

  it('stays at the ends of the range', () => {
    expect(stepPolling(60, -1)).toBe(60);
    expect(stepPolling(3600, 1)).toBe(3600);
  });

  it('visits every value in order', () => {
    let v: PollingStep = POLLING_STEPS[0];
    const seen: number[] = [v];
    for (let i = 0; i < POLLING_STEPS.length - 1; i++) seen.push((v = stepPolling(v, 1)));
    expect(seen).toEqual([...POLLING_STEPS]);
  });
});

describe('pollingLabel', () => {
  it('names the allowed values in words', () => {
    expect(pollingLabel(60)).toBe('1 minute');
    expect(pollingLabel(300)).toBe('5 minutes');
    expect(pollingLabel(3600)).toBe('1 hour');
  });

  it('falls back to seconds', () => {
    expect(pollingLabel(45)).toBe('45 seconds');
  });
});
