import { useId, type KeyboardEvent } from 'react';
import styles from './RulesModal.module.css';

export interface ChipFieldProps {
  label: string;
  values: readonly string[];
  /** Whether a chip has a remove button. Default true. */
  removable?: boolean;
  /** A single value: once there is one, nothing more can be added. */
  single?: boolean;
  /** A list to add from, as a select that starts with "+ Add". Without it a text field is shown. */
  options?: readonly string[];
  /** The placeholder and the suggestions of the text field. */
  placeholder?: string;
  suggestions?: readonly string[];
  /** Replaces "+ Add" as the first option of the select. */
  addText?: string;
  /** The accessible name of the add control. Default "Add <label>". */
  addLabel?: string;
  onAdd: (value: string) => void;
  onRemove: (value: string) => void;
}

/**
 * The design's chip field: a small label, the values as dark chips with a remove button ("Any"
 * when there is none) and a "+ Add" select, or a text field for a free value.
 */
export function ChipField({
  label,
  values,
  removable = true,
  single = false,
  options,
  placeholder,
  suggestions,
  addText = '+ Add',
  addLabel = `Add ${label}`,
  onAdd,
  onRemove,
}: ChipFieldProps) {
  const list = useId();
  const full = single && values.length > 0;
  const choices = options?.filter((o) => !values.includes(o)) ?? [];
  const commit = (input: HTMLInputElement) => {
    const v = input.value.trim();
    input.value = '';
    if (v && !values.includes(v)) onAdd(v);
  };
  const onKey = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key !== 'Enter') return;
    e.preventDefault();
    commit(e.currentTarget);
  };
  return (
    <div className={styles.field} role="group" aria-label={label}>
      <span className={styles.label}>{label}</span>
      <div className={styles.chips}>
        {values.length === 0 ? (
          <span className={styles.any}>Any</span>
        ) : (
          values.map((v) => (
            <span key={v} className={styles.chip}>
              {v}
              {removable && (
                <button
                  type="button"
                  className={styles.chipX}
                  aria-label={`Remove ${v}`}
                  onClick={() => onRemove(v)}
                >
                  ×
                </button>
              )}
            </span>
          ))
        )}
        {!full &&
          (options ? (
            choices.length > 0 && (
              <select
                className={styles.selectXs}
                aria-label={addLabel}
                value=""
                onChange={(e) => e.target.value && onAdd(e.target.value)}
              >
                <option value="">{addText}</option>
                {choices.map((o) => (
                  <option key={o} value={o}>
                    {o}
                  </option>
                ))}
              </select>
            )
          ) : (
            <>
              <input
                type="text"
                className={styles.inputXs}
                aria-label={addLabel}
                placeholder={placeholder}
                list={suggestions ? list : undefined}
                onKeyDown={onKey}
                onBlur={(e) => commit(e.currentTarget)}
              />
              {suggestions && (
                <datalist id={list}>
                  {suggestions.map((s) => (
                    <option key={s} value={s} />
                  ))}
                </datalist>
              )}
            </>
          ))}
      </div>
    </div>
  );
}
