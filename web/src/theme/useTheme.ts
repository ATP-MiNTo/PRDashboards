'use client';

import { useEffect } from 'react';
import { useSaveSettings, useSettings } from '@/api/queries';
import { msUntilSwitch, resolveMode, type ThemeSetting } from './theme';

/** Applies the stored theme to the document and follows the clock while it is Auto. */
export function useTheme(): {
  theme: ThemeSetting | undefined;
  setTheme: (t: ThemeSetting) => void;
} {
  const settings = useSettings();
  const save = useSaveSettings();
  const theme = settings.data?.theme;

  useEffect(() => {
    if (theme === undefined) return;
    const root = document.documentElement;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const apply = () => {
      const now = new Date();
      root.dataset.mode = resolveMode(theme, now);
      if (theme === 'auto') timer = setTimeout(apply, msUntilSwitch(now) + 1000);
    };
    apply();
    return () => clearTimeout(timer);
  }, [theme]);

  const setTheme = (t: ThemeSetting) => {
    if (settings.data) save.mutate({ ...settings.data, theme: t });
  };
  return { theme, setTheme };
}
