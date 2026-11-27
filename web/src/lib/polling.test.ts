import { describe, expect, it } from 'vitest';
import { POLLING_STEPS, pollingLabel, stepPolling } from './polling';

describe('POLLING_STEPS', () => {
  it('are multiples of 15 from 30 to 3600, the contract of PollingSeconds', () => {
    for (const s of POLLING_STEPS) {
      expect(s % 15).toBe(0);
      expect(s).toBeGreaterThanOrEqual(30);
      expect(s).toBeLessThanOrEqual(3600);
    }
    expect(POLLING_STEPS[0]).toBe(30);
    expect(POLLING_STEPS.at(-1)).toBe(3600);
  });

  it('step by 15 up to 300, then 15 minutes and an hour', () => {
    expect(POLLING_STEPS.slice(0, 19)).toEqual(Array.from({ length: 19 }, (_, i) => 30 + i * 15));
    expect(POLLING_STEPS.slice(18)).toEqual([300, 900, 3600]);
    // The five values older versions offered are all still steps.
    for (const old of [60, 120, 300, 900, 3600]) expect(POLLING_STEPS).toContain(old);
  });
});

describe('stepPolling', () => {
  it('goes up and down by 15 seconds in the fine range', () => {
    expect(stepPolling(30, 1)).toBe(45);
    expect(stepPolling(60, 1)).toBe(75);
    expect(stepPolling(75, -1)).toBe(60);
    expect(stepPolling(285, 1)).toBe(300);
  });

  it('jumps to 15 minutes and an hour above 300', () => {
    expect(stepPolling(300, 1)).toBe(900);
    expect(stepPolling(900, 1)).toBe(3600);
    expect(stepPolling(3600, -1)).toBe(900);
    expect(stepPolling(900, -1)).toBe(300);
  });

  it('stays at the ends of the range', () => {
    expect(stepPolling(30, -1)).toBe(30);
    expect(stepPolling(3600, 1)).toBe(3600);
  });

  it('moves a value between two steps to the next one in that direction', () => {
    expect(stepPolling(600, 1)).toBe(900);
    expect(stepPolling(600, -1)).toBe(300);
  });

  it('visits every value in order', () => {
    let v = POLLING_STEPS[0]!;
    const seen = [v];
    for (let i = 0; i < POLLING_STEPS.length - 1; i++) seen.push((v = stepPolling(v, 1)));
    expect(seen).toEqual([...POLLING_STEPS]);
  });
});

describe('pollingLabel', () => {
  it('names minutes and the hour in words', () => {
    expect(pollingLabel(60)).toBe('1 minute');
    expect(pollingLabel(120)).toBe('2 minutes');
    expect(pollingLabel(300)).toBe('5 minutes');
    expect(pollingLabel(3600)).toBe('1 hour');
  });

  it('falls back to seconds', () => {
    expect(pollingLabel(45)).toBe('45 seconds');
    expect(pollingLabel(30)).toBe('30 seconds');
    expect(pollingLabel(90)).toBe('90 seconds');
  });
});
