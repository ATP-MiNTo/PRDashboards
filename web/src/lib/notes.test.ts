import { describe, expect, it } from 'vitest';
import { NOTE_MAX, notePreview, parseNotes, pruneNotes, withNote } from './notes';

describe('parseNotes', () => {
  it('reads an object of strings and drops everything else', () => {
    expect(parseNotes('{"a":"one","b":2,"c":"  ","d":null,"e":"two"}')).toEqual({
      a: 'one',
      e: 'two',
    });
  });

  it('reads nothing from a missing, broken or wrongly shaped value', () => {
    expect(parseNotes(null)).toEqual({});
    expect(parseNotes('{broken')).toEqual({});
    expect(parseNotes('["a"]')).toEqual({});
    expect(parseNotes('"text"')).toEqual({});
    expect(parseNotes('null')).toEqual({});
  });

  it('keeps a key like __proto__ as plain data', () => {
    const notes = parseNotes('{"__proto__":"x"}');
    expect(Object.keys(notes)).toEqual(['__proto__']);
    expect(({} as Record<string, unknown>).x).toBeUndefined();
  });

  it('cuts a stored note that is too long', () => {
    const notes = parseNotes(JSON.stringify({ a: 'x'.repeat(NOTE_MAX + 50) }));
    expect(notes.a).toHaveLength(NOTE_MAX);
  });
});

describe('pruneNotes', () => {
  const notes = { a: 'one', b: 'two' };

  it('drops the notes of pull requests that are gone', () => {
    expect(pruneNotes(notes, ['b', 'c'])).toEqual({ b: 'two' });
  });

  it('returns the same object when nothing goes, so a render stays equal', () => {
    expect(pruneNotes(notes, ['a', 'b'])).toBe(notes);
  });

  it('prunes nothing while the list is still empty', () => {
    expect(pruneNotes(notes, [])).toBe(notes);
  });
});

describe('withNote', () => {
  it('sets, trims and replaces a note', () => {
    expect(withNote({}, 'a', '  hello  ')).toEqual({ a: 'hello' });
    expect(withNote({ a: 'old', b: 'keep' }, 'a', 'new')).toEqual({ a: 'new', b: 'keep' });
  });

  it('removes the note when the text is empty', () => {
    expect(withNote({ a: 'old', b: 'keep' }, 'a', '   ')).toEqual({ b: 'keep' });
  });

  it('does not change the notes it was given', () => {
    const notes = Object.freeze({ a: 'one' });
    withNote(notes, 'a', 'two');
    expect(notes).toEqual({ a: 'one' });
  });

  it('cuts a text that is too long', () => {
    expect(withNote({}, 'a', 'y'.repeat(NOTE_MAX + 1)).a).toHaveLength(NOTE_MAX);
  });
});

describe('notePreview', () => {
  it('is the first 60 characters of the trimmed note', () => {
    expect(notePreview('  short  ')).toBe('short');
    expect(notePreview('a'.repeat(100))).toBe('a'.repeat(60));
  });
});
