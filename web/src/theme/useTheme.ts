'use client';

import { useCallback, useEffect, useState } from 'react';
import { useSaveSettings, useSettings } from '@/api/queries';
import { reducedMotion } from '@/lib/storage';
import { daisyTheme, msUntilSwitch, resolveMode, type Mode, type ThemeSetting } from './theme';
import { applyMode } from './transition';

/** Applies the stored theme to the document and follows the clock while it is Auto. */
export function useTheme(): {
  theme: ThemeSetting | undefined;
  /** The mode in effect now: the explicit choice, or what the clock gives in Auto. */
  mode: Mode | undefined;
  setTheme: (t: ThemeSetting) => void;
} {
  const settings = useSettings();
  const save = useSaveSettings();
  const theme = settings.data?.theme;
  const [resolved, setResolved] = useState<Mode>();

  useEffect(() => {
    if (theme === undefined) return;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const apply = () => {
      const now = new Date();
      const m = resolveMode(theme, now);
      applyMode(document, { mode: m, theme: daisyTheme(m) }, reducedMotion());
      setResolved(m);
      if (theme === 'auto') timer = setTimeout(apply, msUntilSwitch(now) + 1000);
    };
    apply();
    return () => clearTimeout(timer);
  }, [theme]);

  const { mutate } = save;
  const current = settings.data;
  const setTheme = useCallback(
    (t: ThemeSetting) => {
      if (current) mutate({ ...current, theme: t });
    },
    [current, mutate],
  );
  return { theme, mode: theme === undefined ? undefined : resolved, setTheme };
}
