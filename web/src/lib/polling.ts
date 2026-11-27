/**
 * The polling intervals the field steps through, in seconds. The API (`PollingSeconds` in
 * api/openapi.yaml) accepts any multiple of 15 from 30 to 3600. The design steps by 15 seconds
 * from 30 to 300, and the field keeps two long steps after that, 15 minutes and 1 hour.
 */
export const POLLING_MIN = 30;
export const POLLING_MAX = 3600;

export const POLLING_STEPS: readonly number[] = [
  ...Array.from({ length: 19 }, (_, i) => POLLING_MIN + i * 15),
  900,
  POLLING_MAX,
];

export type PollingStep = number;

/** The interval in words, for a screen reader. */
export function pollingLabel(sec: number): string {
  if (sec === 3600) return '1 hour';
  if (sec >= 60 && sec % 60 === 0) return `${sec / 60} minutes`.replace(/^1 minutes$/, '1 minute');
  return `${sec} seconds`;
}

/**
 * The next step up (`dir` 1) or down (`dir` -1) from `sec`, or the same value at an end of the
 * range. A value between two steps, such as 600 stored by another client, moves to the next one in
 * that direction.
 */
export function stepPolling(sec: number, dir: 1 | -1): number {
  const next =
    dir === 1
      ? POLLING_STEPS.find((s) => s > sec)
      : [...POLLING_STEPS].reverse().find((s) => s < sec);
  return next ?? sec;
}
