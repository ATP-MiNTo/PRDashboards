import type { ReactNode } from 'react';
import styles from './CommandTable.module.css';

export type Tone = 'good' | 'warn' | 'bad' | 'info' | 'mute' | 'stack';

export function Pill({
  tone,
  children,
  title,
}: {
  tone: Tone;
  children: ReactNode;
  title?: string;
}) {
  return (
    <span className={`${styles.pill} ${styles[tone]}`} title={title}>
      {children}
    </span>
  );
}
