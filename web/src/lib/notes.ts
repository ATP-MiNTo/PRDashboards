/** Notes on pull requests: plain text kept in this browser only, keyed by the pull request id. */

export const NOTES_KEY = 'prd-notes';
/** The tooltip of a note shows this many characters. */
export const NOTE_PREVIEW = 60;
/** The most a note can hold. */
export const NOTE_MAX = 2000;

export type Notes = Readonly<Record<string, string>>;

export const NO_NOTES: Notes = {};

/** The notes in a stored JSON object. Anything that is not an object of non-empty strings is dropped. */
export function parseNotes(raw: string | null): Notes {
  if (raw === null) return NO_NOTES;
  let v: unknown;
  try {
    v = JSON.parse(raw);
  } catch {
    return NO_NOTES;
  }
  if (typeof v !== 'object' || v === null || Array.isArray(v)) return NO_NOTES;
  const entries: [string, string][] = [];
  for (const [id, text] of Object.entries(v)) {
    if (typeof text === 'string' && text.trim() !== '') entries.push([id, text.slice(0, NOTE_MAX)]);
  }
  return Object.fromEntries(entries);
}

/** The notes whose pull request is still listed. An empty list (still loading) prunes nothing. */
export function pruneNotes(notes: Notes, existing: readonly string[]): Notes {
  if (existing.length === 0) return notes;
  const keep = new Set(existing);
  const entries = Object.entries(notes).filter(([id]) => keep.has(id));
  return entries.length === Object.keys(notes).length ? notes : Object.fromEntries(entries);
}

/** The notes with one set. Text that is empty once trimmed removes the note. */
export function withNote(notes: Notes, id: string, text: string): Notes {
  const trimmed = text.trim().slice(0, NOTE_MAX);
  const rest = Object.entries(notes).filter(([k]) => k !== id);
  return Object.fromEntries(trimmed === '' ? rest : [...rest, [id, trimmed]]);
}

/** The first characters of a note, for its tooltip. */
export function notePreview(text: string): string {
  return text.trim().slice(0, NOTE_PREVIEW);
}
