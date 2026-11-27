import { useId, useRef, useState } from 'react';
import { ApiError, type SnapshotPr } from '@/api/client';
import { useRequestChanges } from '@/api/queries';
import { describeReviewProblem } from '@/lib/problems';
import { Modal } from '../Modal/Modal';
import modal from '../Modal/Modal.module.css';
import { useToast } from '../Toast/Toast';
import styles from './Dialogs.module.css';

/** The longest review text GitHub takes. */
const MAX_LENGTH = 65536;
const BODY_REQUIRED = describeReviewProblem('body_required');

/**
 * Asks for the text of a request for changes (GitHub needs one) and sends it with the head the card
 * showed. A refusal stays in the dialog as text, so the comment is not lost.
 */
export function RequestChangesDialog({ pr, onClose }: { pr: SnapshotPr; onClose: () => void }) {
  const titleId = useId();
  const fieldId = useId();
  const area = useRef<HTMLTextAreaElement>(null);
  const [text, setText] = useState('');
  const [error, setError] = useState<string | null>(null);
  const request = useRequestChanges();
  const toast = useToast();

  const send = () => {
    if (text.trim() === '') {
      setError(BODY_REQUIRED);
      area.current?.focus();
      return;
    }
    setError(null);
    request.mutate(
      { id: pr.id, body: { expectedHeadSha: pr.headSha, body: text } },
      {
        onSuccess: () => {
          toast.show(`You requested changes on #${pr.number}`, 'info');
          onClose();
        },
        onError: (e) =>
          setError(
            describeReviewProblem(
              e instanceof ApiError ? e.code : undefined,
              e instanceof ApiError ? e.detail : undefined,
            ),
          ),
      },
    );
  };

  return (
    <Modal labelledBy={titleId} onClose={onClose} dismissible={!request.isPending}>
      <h3 id={titleId} className={modal.title}>
        Request changes on {pr.repo.nameWithOwner} #{pr.number}
      </h3>
      <p className={modal.subtitle}>{pr.title}</p>
      <label htmlFor={fieldId} className={styles.fieldLabel}>
        Comment (required)
      </label>
      <textarea
        id={fieldId}
        ref={area}
        className={styles.textarea}
        value={text}
        maxLength={MAX_LENGTH}
        required
        aria-invalid={error === BODY_REQUIRED}
        placeholder="What should change?"
        onChange={(e) => {
          setText(e.target.value);
          if (error === BODY_REQUIRED) setError(null);
        }}
      />
      {error && (
        <p className={styles.error} role="alert">
          {error}
        </p>
      )}
      <div className={modal.actions}>
        <button type="button" className={modal.cancel} onClick={onClose}>
          Cancel
        </button>
        <button
          type="button"
          className={styles.primaryBtn}
          disabled={request.isPending}
          onClick={send}
        >
          {request.isPending ? 'Sending' : 'Request changes'}
        </button>
      </div>
    </Modal>
  );
}
