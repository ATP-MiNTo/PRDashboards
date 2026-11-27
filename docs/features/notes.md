# Notes on pull requests

Status: Built. PRD: [PRD.md](../PRD.md). Spec: [TECH_SPEC.md](../TECH_SPEC.md) section 11. Related: [command-table.md](command-table.md), [storage.md](storage.md), [dialog-and-popover.md](dialog-and-popover.md).

## Purpose

A private note on a pull request, as the approved design has it: a square button on the card, a tooltip that previews the note, an editor dialog and a "Note" badge in the card's meta line. Notes are plain text kept in this browser only. They are not sent to the server, GitHub or Jira, and they are not shared between browsers. This feature is not in the PRD or the spec.

## Data model

No server data. `localStorage` key `prd-notes`: a JSON object from pull request id (the snapshot `id`) to the note text. A note is at most 2000 characters (`NOTE_MAX`), trimmed, and never empty: saving an empty text deletes the note. Every read and write is in `try/catch` (`readStored`, `writeStored` in [web/src/lib/storage.ts](../../web/src/lib/storage.ts)); when storage is blocked the notes last for the visit. Anything in the stored value that is not an object of non-empty strings is dropped.

## Code map

- [web/src/lib/notes.ts](../../web/src/lib/notes.ts): the pure helpers `parseNotes`, `pruneNotes`, `withNote`, `notePreview` and the constants `NOTES_KEY`, `NOTE_PREVIEW` (60), `NOTE_MAX`.
- [web/src/components/Toolbar/useListPrefs.ts](../../web/src/components/Toolbar/useListPrefs.ts): holds `notes` (pruned against the ids of the snapshot when read) and `setNote(id, text)`, which writes `prd-notes`.
- [NoteButton.tsx](../../web/src/components/CommandTable/NoteButton.tsx): the square ghost button (32px) with a document-plus icon (a filled document when the pull request has a note), inside a [Tip](../../web/src/components/Tip/Tip.tsx). [NoteDialog.tsx](../../web/src/components/CommandTable/NoteDialog.tsx): the editor in the shared [Modal](../../web/src/components/Modal/Modal.tsx). [Notes.module.css](../../web/src/components/CommandTable/Notes.module.css).
- [Card.tsx](../../web/src/components/CommandTable/Card.tsx) places the button in the first cell of the controls row, before the Hide checkbox, and the "Note" badge in the meta line. [List.tsx](../../web/src/components/CommandTable/List.tsx) passes `notes[id]` and `setNote`.

## Key flows

1. Without a note the button is a ghost square, named "Add a note to #N", and its tooltip says "Add a note". With a note it is a primary outline with a dot (the indicator), named "Edit the note on #N: <preview>", and its tooltip shows the first 60 characters of the note (`notePreview`).
2. The button opens "Note on <owner/repo> #N" with the pull request title, a textarea (plain text, `maxLength` 2000), Cancel and Save, and Delete when a note exists. Save stores the trimmed text; Delete (or saving an empty text) removes the note; Cancel and Escape change nothing.
3. A note whose pull request has left the snapshot is dropped when read (`pruneNotes`, never while the snapshot is still empty) and from storage at the next change of any note.

## Outside calls

None.

## Rules and invariants

- Notes are rendered as text, never as HTML (`dangerouslySetInnerHTML` is not used); the tooltip is a `data-tip` attribute and the button's accessible name carries the same text.
- Nothing about notes reaches the server, a log or a request.
- Storage failures never break the page.
- Classes only: no `style` attribute.

## Tests

- [notes.test.ts](../../web/src/lib/notes.test.ts): parsing bad input, pruning, setting and removing, the preview.
- [Notes.test.tsx](../../web/src/components/CommandTable/Notes.test.tsx): adding a note (preview, badge, stored value), editing, Cancel, Delete, pruning on the next change, blocked storage.
