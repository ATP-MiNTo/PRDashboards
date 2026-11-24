export type ThemeSetting = 'light' | 'dark' | 'auto';
export type Mode = 'light' | 'dark';

export const LIGHT_FROM_HOUR = 7;
export const DARK_FROM_HOUR = 19;

/** Auto is light from 07:00 to 19:00 local time and dark otherwise. */
export function autoMode(now: Date): Mode {
  const h = now.getHours();
  return h >= LIGHT_FROM_HOUR && h < DARK_FROM_HOUR ? 'light' : 'dark';
}

export function resolveMode(setting: ThemeSetting, now: Date): Mode {
  return setting === 'auto' ? autoMode(now) : setting;
}

/** Milliseconds until Auto changes mode, so a page left open follows the clock. */
export function msUntilSwitch(now: Date): number {
  const next = new Date(now);
  const h = now.getHours();
  const target = h >= LIGHT_FROM_HOUR && h < DARK_FROM_HOUR ? DARK_FROM_HOUR : LIGHT_FROM_HOUR;
  next.setHours(target, 0, 0, 0);
  if (next <= now) next.setDate(next.getDate() + 1);
  return next.getTime() - now.getTime();
}

export function isThemeSetting(v: unknown): v is ThemeSetting {
  return v === 'light' || v === 'dark' || v === 'auto';
}
