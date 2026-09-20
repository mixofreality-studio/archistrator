/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { toWireEntries, freeformNotesFrom, isQuestion } from './reviewBatch.ts';
import type { PostedComment } from './commentContextTypes.ts';

const ANCHOR = {
  kind: 'node' as const,
  label: 'GtdManager',
  source: 'Architecture · C4',
  jsonPath: '$.components[id=GtdManager]',
};

void test('a margin reply (anchor: null, replyTo set) appears in toWire() with replyTo intact and empty jsonPath/anchorText', () => {
  const reply: PostedComment = { text: 'Still too vague', anchor: null, replyTo: 'r1c1' };
  const wire = toWireEntries([reply]);
  assert.equal(wire.length, 1);
  assert.deepEqual(wire[0], {
    jsonPath: '',
    anchorText: '',
    text: 'Still too vague',
    replyTo: 'r1c1',
  });
});

void test('that same margin reply does NOT appear in freeformNotes()', () => {
  const reply: PostedComment = { text: 'Still too vague', anchor: null, replyTo: 'r1c1' };
  assert.equal(freeformNotesFrom([reply]), '');
});

void test('an ordinary free-form note (no anchor, no replyTo) still appears in freeformNotes() and NOT in toWire()', () => {
  const note: PostedComment = { text: 'Objective 3 is vague', anchor: null };
  assert.equal(freeformNotesFrom([note]), 'Objective 3 is vague');
  assert.equal(toWireEntries([note]).length, 0);
});

void test('an anchored comment (no replyTo) behaves exactly as before: in toWire(), never in freeformNotes()', () => {
  const anchored: PostedComment = { text: 'rename this', anchor: ANCHOR };
  const wire = toWireEntries([anchored]);
  assert.equal(wire.length, 1);
  assert.deepEqual(wire[0], {
    jsonPath: ANCHOR.jsonPath,
    anchorText: ANCHOR.label,
    text: 'rename this',
    replyTo: '',
  });
  assert.equal(freeformNotesFrom([anchored]), '');
});

void test('a reply never double-rides a batch: exactly one wire entry, zero freeform notes', () => {
  const reply: PostedComment = { text: 'Address the pump defect', anchor: null, replyTo: 'r9' };
  assert.equal(toWireEntries([reply]).length, 1);
  assert.equal(freeformNotesFrom([reply]), '');
});

void test('a question is excluded from both toWire() and freeformNotes() (unchanged, out of this fix scope)', () => {
  const question: PostedComment = {
    text: 'Which region?',
    anchor: null,
    commentType: 'question',
    addressee: 'pm',
  };
  assert.equal(toWireEntries([question]).length, 0);
  assert.equal(freeformNotesFrom([question]), '');
  assert.equal(isQuestion(question), true);
});
