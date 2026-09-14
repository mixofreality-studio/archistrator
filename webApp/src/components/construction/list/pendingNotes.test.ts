/**
 * pendingNotes.ts — the pending operator-note line (B1 fix round, item 12), and the
 * wire's pending count it reads (wire.ts pendingOperatorNoteCount).
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  NOTE_PENDING_TOOLTIP,
  NOTE_UNDELIVERED_TOOLTIP,
  pendingNoteLineFor,
} from './pendingNotes.ts';
import { pendingOperatorNoteCount } from '../../../contracts/wire.ts';
import type { components } from '../../../contracts/schema.ts';
import type { RowState } from './activityRowPresentation.ts';

type WireNote = components['schemas']['SystemDesignOperatorNote'];

const note = (over: Partial<WireNote>): WireNote => ({
  noteId: 'C-x:note:r:1',
  kind: 1,
  text: 'tighten it',
  recordedAt: '2026-09-14T10:00:00Z',
  ...over,
});

void test('the pending count: an absent or null list is 0', () => {
  assert.equal(pendingOperatorNoteCount(undefined), 0);
  assert.equal(pendingOperatorNoteCount(null), 0);
  assert.equal(pendingOperatorNoteCount([]), 0);
});

void test('the pending count excludes delivered notes and skip notes', () => {
  const notes = [
    note({ noteId: 'a', kind: 1 }),
    note({ noteId: 'b', kind: 2 }),
    note({ noteId: 'c', kind: 6 }),
    note({
      noteId: 'd',
      kind: 2,
      deliveredToAttemptId: 'C-x:srs:1',
      deliveredAt: '2026-09-14T10:01:00Z',
    }),
    note({ noteId: 'e', kind: 5 }),
  ];
  assert.equal(pendingOperatorNoteCount(notes), 3);
  assert.equal(pendingOperatorNoteCount([note({ kind: 5 })]), 0, 'a skip note is never pending');
  assert.equal(
    pendingOperatorNoteCount([note({ deliveredToAttemptId: 'C-x:srs:1' })]),
    0,
    'a delivered note is not pending'
  );
});

void test('no pending note, no line — in every state', () => {
  for (const s of ['notStarted', 'failed', 'passed', 'awaitingHuman'] as RowState[]) {
    assert.equal(pendingNoteLineFor(0, s), undefined);
    assert.equal(pendingNoteLineFor(undefined, s), undefined);
  }
});

void test('work left and nothing running: the last dispatch did not start', () => {
  for (const s of ['awaitingHuman', 'failed', 'waiting', 'notStarted', 'unknown'] as RowState[]) {
    const one = pendingNoteLineFor(1, s);
    assert.deepEqual(one, {
      kind: 'didNotStart',
      count: 1,
      text: 'Note pending — the last dispatch did not start',
      tooltip: NOTE_PENDING_TOOLTIP,
    });
    assert.equal(
      pendingNoteLineFor(3, s)?.text,
      '3 notes pending — the last dispatch did not start'
    );
  }
  assert.equal(
    NOTE_PENDING_TOOLTIP,
    "The operator's note has not reached an agent yet. It rides this activity's next dispatch."
  );
});

void test('an agent running now: the note is queued, nothing is said', () => {
  assert.equal(pendingNoteLineFor(2, 'running'), undefined);
});

void test('Done or Skipped: the note was never delivered', () => {
  for (const s of ['passed', 'skipped'] as RowState[]) {
    assert.deepEqual(pendingNoteLineFor(1, s), {
      kind: 'neverDelivered',
      count: 1,
      text: 'Note never delivered — no agent ran after it',
      tooltip: NOTE_UNDELIVERED_TOOLTIP,
    });
    assert.equal(
      pendingNoteLineFor(2, s)?.text,
      '2 notes never delivered — no agent ran after them'
    );
  }
  assert.equal(
    NOTE_UNDELIVERED_TOOLTIP,
    "Recorded after the last agent run. It rides this activity's next dispatch if the activity is re-queued."
  );
});
