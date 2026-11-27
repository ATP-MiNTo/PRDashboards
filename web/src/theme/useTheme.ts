'use client';

import { useEffect, useState } from 'react';
import { useSaveSettings, useSettings } from '@/api/queries';
import { daisyTheme, msUntilSwitch, resolveMode, type Mode, type ThemeSetting } from './theme';

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
    const root = document.documentElement;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const apply = () => {
      const now = new Date();
      const m = resolveMode(theme, now);
      root.dataset.mode = m;
      root.dataset.theme = daisyTheme(m);
      setResolved(m);
      if (theme === 'auto') timer = setTimeout(apply, msUntilSwitch(now) + 1000);
    };
    apply();
    return () => clearTimeout(timer);
  }, [theme]);

  const setTheme = (t: ThemeSetting) => {
    if (settings.data) save.mutate({ ...settings.data, theme: t });
  };
  return { theme, mode: theme === undefined ? undefined : resolved, setTheme };
}
