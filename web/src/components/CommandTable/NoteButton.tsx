import { notePreview } from '@/lib/notes';
import { Tip } from '../Tip/Tip';
import styles from './Notes.module.css';

/** A document with a plus: no note yet. The outline of the design's "Add note" glyph. */
function NoteEmptyIcon() {
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
      <path d="M14 3H6a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V9z" />
      <path d="M14 3v6h6" />
      <path d="M12 12v6M9 15h6" />
    </svg>
  );
}

/** A filled document with text lines: the pull request has a note. */
function NoteFullIcon() {
  return (
    <svg
      width="16"
      height="16"
      viewBox="0 0 24 24"
      fill="currentColor"
      aria-hidden="true"
      focusable="false"
    >
      <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8zm-1 1.5L18.5 9H13zM8 13h8v1.6H8zm0 3.4h6V18H8z" />
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
        {has ? <NoteFullIcon /> : <NoteEmptyIcon />}
        {has && <span className={styles.dot} aria-hidden="true" />}
      </button>
    </Tip>
  );
}
