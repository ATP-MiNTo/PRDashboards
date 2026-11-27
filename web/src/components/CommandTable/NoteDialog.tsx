import { useId, useState } from 'react';
import type { SnapshotPr } from '@/api/client';
import { NOTE_MAX } from '@/lib/notes';
import { Modal } from '../Modal/Modal';
import modal from '../Modal/Modal.module.css';
import dialogs from './Dialogs.module.css';
import styles from './Notes.module.css';

/**
 * The editor of the note of one pull request. The note is plain text kept in this browser only.
 * Saving an empty text removes the note.
 */
export function NoteDialog({
  pr,
  note,
  onSave,
  onClose,
}: {
  pr: SnapshotPr;
  note: string | undefined;
  onSave: (text: string) => void;
  onClose: () => void;
}) {
  const titleId = useId();
  const fieldId = useId();
  const [text, setText] = useState(note ?? '');
  return (
    <Modal labelledBy={titleId} onClose={onClose}>
      <h3 id={titleId} className={modal.title}>
        Note on {pr.repo.nameWithOwner} #{pr.number}
      </h3>
      <p className={modal.subtitle}>{pr.title}</p>
      <label htmlFor={fieldId} className={dialogs.fieldLabel}>
        Note (kept in this browser only)
      </label>
      <textarea
        id={fieldId}
        className={dialogs.textarea}
        value={text}
        maxLength={NOTE_MAX}
        placeholder="Write a note for yourself"
        autoFocus
        onChange={(e) => setText(e.target.value)}
      />
      <div className={modal.actions}>
        {note !== undefined && (
          <>
            <button
              type="button"
              className={styles.delete}
              onClick={() => {
                onSave('');
                onClose();
              }}
            >
              Delete
            </button>
            <span className={styles.spacer} />
          </>
        )}
        <button type="button" className={modal.cancel} onClick={onClose}>
          Cancel
        </button>
        <button
          type="button"
          className={styles.save}
          onClick={() => {
            onSave(text);
            onClose();
          }}
        >
          Save
        </button>
      </div>
    </Modal>
  );
}
