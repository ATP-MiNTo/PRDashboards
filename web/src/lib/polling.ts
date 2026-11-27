/**
 * The polling intervals the API accepts, in seconds (`PollingSeconds` in api/openapi.yaml). The
 * design steps a number field by 15 seconds between 30 and 300, but the contract allows only
 * these five values, so the field steps through them.
 */
export const POLLING_STEPS = [60, 120, 300, 900, 3600] as const;

export type PollingStep = (typeof POLLING_STEPS)[number];

const LABELS: Record<PollingStep, string> = {
  60: '1 minute',
  120: '2 minutes',
  300: '5 minutes',
  900: '15 minutes',
  3600: '1 hour',
};

/** The interval in words, for a screen reader. */
export function pollingLabel(sec: number): string {
  return LABELS[sec as PollingStep] ?? `${sec} seconds`;
}

/** The next allowed interval up (`dir` 1) or down (`dir` -1), or the same one at an end of the range. */
export function stepPolling(sec: PollingStep, dir: 1 | -1): PollingStep {
  const i = POLLING_STEPS.indexOf(sec) + dir;
  return POLLING_STEPS[Math.min(POLLING_STEPS.length - 1, Math.max(0, i))] ?? sec;
}
