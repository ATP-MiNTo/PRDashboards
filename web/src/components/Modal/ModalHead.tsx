import type { ReactNode } from 'react';
import { InfoTip } from '../InfoTip/InfoTip';
import styles from './Modal.module.css';

/**
 * The top of the design's dialogs: the blue Close (or Done) button pinned to the corner while the
 * content scrolls, then the title with its optional "i" note.
 */
export function ModalHead({
  titleId,
  title,
  tip,
  tipLabel,
  closeLabel = 'Close',
  onClose,
}: {
  titleId: string;
  title: ReactNode;
  /** The note of the "i" button after the title. */
  tip?: string;
  tipLabel?: string;
  closeLabel?: string;
  onClose: () => void;
}) {
  return (
    <>
      <div className={styles.closeRow}>
        <button type="button" className={styles.close} onClick={onClose}>
          {closeLabel}
        </button>
      </div>
      <div className={styles.titleRow}>
        <h3 id={titleId} className={styles.title}>
          {title}
        </h3>
        {tip && <InfoTip tip={tip} label={tipLabel} />}
      </div>
    </>
  );
}
