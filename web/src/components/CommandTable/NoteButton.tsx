import { notePreview } from '@/lib/notes';
import { Tip } from '../Tip/Tip';
import styles from './Notes.module.css';

function NoteIcon() {
  return (
    <svg
      width="16"
      height="16"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      focusable="false"
    >
      <path d="M12 20h9" />
      <path d="M16.5 3.500a2.100 2.100 0 0 1 3 3L7 19l-4 1 1-4Z" />
    </svg>
  );
}

/**
 * The square ghost button that opens the note of a pull request. With a note it is a primary
 * outline with a dot, and the tooltip previews the first characters of the note.
 */
export function NoteButton({
  number,
  note,
  onOpen,
}: {
  number: number;
  note: string | undefined;
  onOpen: () => void;
}) {
  const has = note !== undefined;
  const preview = has ? notePreview(note) : '';
  return (
    <Tip tip={has ? preview : 'Add a note'} className={styles.wrap}>
      <button
        type="button"
        className={has ? `${styles.button} ${styles.has}` : styles.button}
        aria-label={has ? `Edit the note on #${number}: ${preview}` : `Add a note to #${number}`}
        aria-haspopup="dialog"
        onClick={onOpen}
      >
        <NoteIcon />
        {has && <span className={styles.dot} aria-hidden="true" />}
      </button>
    </Tip>
  );
}
