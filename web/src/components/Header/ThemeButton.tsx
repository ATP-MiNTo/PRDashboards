'use client';

import { useRef } from 'react';
import { DARK_FROM_HOUR, LIGHT_FROM_HOUR, type Mode, type ThemeSetting } from '@/theme/theme';
import { useToast } from '../Toast/Toast';
import styles from './Header.module.css';
import { Icon } from './Icon';

const hh = (h: number) => String(h).padStart(2, '0');

/** What the person is told after turning Auto on. */
export const AUTO_TOAST = `Theme is Auto: light from ${hh(LIGHT_FROM_HOUR)}:00 to ${hh(DARK_FROM_HOUR)}:00`;

/** The tooltip, and the accessible description, of the button: how to reach Auto without a mouse too. */
export const THEME_TITLE =
  'Click: light or dark. Auto: right-click, Shift+click, Shift+Enter or the Menu key';

const SUN = (
  <>
    <circle cx="12" cy="12" r="4" />
    <path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4" />
  </>
);
const MOON = <path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8z" />;
const AUTO = (
  <>
    <circle cx="12" cy="12" r="9" />
    <path d="M12 3a9 9 0 0 1 0 18z" fill="currentColor" />
    <path d="M12 3v18" />
  </>
);

/**
 * The theme circle. A click switches between light and dark (a click in Auto leaves Auto). A
 * right-click, the context-menu key, Shift+click or Shift+Enter (a keyboard "click" carries the
 * Shift state) turns Auto on, or off again with the mode that
 * was chosen before. The icon is the mode a click would switch to: a sun in the dark, a moon in the
 * light, and a half-filled circle in Auto.
 */
export function ThemeButton({
  theme,
  mode,
  onTheme,
}: {
  theme: ThemeSetting | undefined;
  mode: Mode | undefined;
  onTheme: (t: ThemeSetting) => void;
}) {
  const toast = useToast();
  const before = useRef<Mode | null>(null);
  const loading = theme === undefined || mode === undefined;

  const toggleAuto = () => {
    if (loading) return;
    if (theme === 'auto') {
      const back = before.current ?? mode;
      onTheme(back);
      toast.show(`Theme is ${back} again`);
    } else {
      before.current = mode;
      onTheme('auto');
      toast.show(AUTO_TOAST);
    }
  };

  const state = theme === 'auto' ? 'auto' : mode === 'dark' ? 'dark' : 'light';
  return (
    <button
      type="button"
      className={styles.ctlButton}
      aria-label={`Theme: ${state}`}
      title={THEME_TITLE}
      disabled={loading}
      onClick={(e) => {
        if (e.shiftKey) toggleAuto();
        else if (!loading) onTheme(mode === 'dark' ? 'light' : 'dark');
      }}
      onContextMenu={(e) => {
        e.preventDefault();
        toggleAuto();
      }}
    >
      <span key={state} className={styles.spinIn}>
        <Icon>{state === 'auto' ? AUTO : state === 'dark' ? SUN : MOON}</Icon>
      </span>
    </button>
  );
}
