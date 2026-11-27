import type { ReactNode } from 'react';
import styles from './Tip.module.css';

/**
 * The design's tooltip around one control. `tip` is shown on hover and while a control inside has
 * keyboard focus. It is only a visual hint: the control must also carry the text for assistive
 * technology (an `aria-describedby` to the same text, or a visible label).
 */
export function Tip({
  tip,
  side = 'top',
  className,
  children,
}: {
  tip: string;
  side?: 'top' | 'left';
  className?: string;
  children: ReactNode;
}) {
  const cls = [styles.tip, side === 'left' ? styles.left : '', className ?? '']
    .filter(Boolean)
    .join(' ');
  return (
    <span className={cls} data-tip={tip}>
      {children}
    </span>
  );
}
