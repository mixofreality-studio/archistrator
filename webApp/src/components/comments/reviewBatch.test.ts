/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  toWireEntries,
  decisionFeedbackFor,
  foldCommentsIntoNotes,
  freeformNotesFrom,
  isQuestion,
  pendingQuestionsFrom,
} from './reviewBatch.ts';
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

// ── pendingQuestionsFrom: the "Ask" payload (comment-margin task 5b) ────────
// A question thread is the conversational case, so a follow-up staged against an
// answered question must reach AskQuestions carrying the thread it answers. Before
// this task the question mapper had nowhere to put `replyTo` and the container
// hardcoded '' — so a follow-up dispatched as a brand-new thread.

void test('a follow-up staged on a question thread carries replyTo into the Ask payload', () => {
  const followUp: PostedComment = {
    text: 'That does not answer the cost objective',
    anchor: null,
    commentType: 'question',
    addressee: 'architect',
    replyTo: 'r1c0',
  };
  assert.deepEqual(pendingQuestionsFrom([followUp]), [
    {
      addressee: 'architect',
      jsonPath: '',
      anchorText: '',
      text: 'That does not answer the cost objective',
      replyTo: 'r1c0',
    },
  ]);
});

void test('a fresh anchored question still asks with an empty replyTo (a new thread)', () => {
  const question: PostedComment = {
    text: 'Which region?',
    anchor: ANCHOR,
    commentType: 'question',
    addressee: 'pm',
  };
  assert.deepEqual(pendingQuestionsFrom([question]), [
    {
      addressee: 'pm',
      jsonPath: ANCHOR.jsonPath,
      anchorText: ANCHOR.label,
      text: 'Which region?',
      replyTo: '',
    },
  ]);
});

void test('change-requests never ride the Ask payload, replies included', () => {
  const changeRequestReply: PostedComment = { text: 'still vague', anchor: null, replyTo: 'r1c1' };
  const note: PostedComment = { text: 'a plain note', anchor: null };
  assert.equal(pendingQuestionsFrom([changeRequestReply, note]).length, 0);
});

// I2: `SubmitSDPDecision` carries ONE feedback field, so the M0 approve folds the
// anchored comments into it. Before the fold they were staged, counted on the bar
// and then cleared by `reset()` — the approval recorded none of them.

void test('the fold puts the free-form notes first, then one anchored line each', () => {
  assert.equal(
    foldCommentsIntoNotes('Cost is the binding constraint.', [
      {
        jsonPath: '$.options[kind=compressedSolution]',
        anchorText: 'Compressed',
        text: 'why not this one?',
        replyTo: '',
      },
      {
        jsonPath: '$.activities[name="C-review-engine"]',
        anchorText: 'C-review-engine',
        text: '15 days is the estimate?',
        replyTo: '',
      },
    ]),
    'Cost is the binding constraint.\n' +
      '$.options[kind=compressedSolution] — why not this one?\n' +
      '$.activities[name="C-review-engine"] — 15 days is the estimate?'
  );
});

void test('the fold with no notes produces no leading blank line, and an empty batch is empty', () => {
  assert.equal(
    foldCommentsIntoNotes('', [
      {
        jsonPath: '$.options[kind=normalSolution]',
        anchorText: 'Normal',
        text: 'agreed',
        replyTo: '',
      },
    ]),
    '$.options[kind=normalSolution] — agreed'
  );
  assert.equal(foldCommentsIntoNotes('', []), '');
  assert.equal(foldCommentsIntoNotes('   ', []), '');
});

void test('a reply (no jsonPath) folds as its own text, and an empty comment is dropped', () => {
  assert.equal(
    foldCommentsIntoNotes('', [
      { jsonPath: '', anchorText: '', text: 'answering the earlier point', replyTo: 'm0r1c1' },
      { jsonPath: '$.options[kind=normalSolution]', anchorText: 'Normal', text: '  ', replyTo: '' },
    ]),
    'answering the earlier point'
  );
});

void test('the fold loses nothing: every staged comment text appears in the result', () => {
  const comments = [
    { jsonPath: '$.a', anchorText: 'a', text: 'first', replyTo: '' },
    { jsonPath: '$.b', anchorText: 'b', text: 'second', replyTo: '' },
    { jsonPath: '', anchorText: '', text: 'third', replyTo: 'x1' },
  ];
  const folded = foldCommentsIntoNotes('notes', comments);
  for (const c of comments) assert.ok(folded.includes(c.text), `${c.text} survived the fold`);
  assert.equal(folded.split('\n').length, 4);
});

// Fix round 2: the M0 approve used to send `comments: toWire()` like every other
// decision. `SubmitSDPDecision` has no comments array, and its Phase-2 ledger
// refuses any batch carrying a replyTo (pdCheckNoReplyTo, RULING P13) — while the M0
// gate OFFERS replies. So "architect replies in an M0 thread, then presses Approve"
// was a 400. These pin the shape rule, not just the fold arithmetic.

void test('an optionId decision sends NO comments array, and the key is absent not empty', () => {
  const body = decisionFeedbackFor({
    fold: true,
    notes: 'Committing the compressed option.',
    comments: [
      {
        jsonPath: '$.options[kind=compressedSolution]',
        anchorText: 'C',
        text: 'why?',
        replyTo: '',
      },
    ],
  });
  // Absent, not []: the wire distinguishes them and the Manager reads feedback.Comments.
  assert.equal('comments' in body, false);
  assert.match(body.notes, /Committing the compressed option\./);
  assert.match(body.notes, /why\?/);
});

void test('a REPLY in an M0 batch survives in the notes and never reaches the wire array', () => {
  // The exact 400 scenario: a margin reply carries replyTo and no jsonPath.
  const body = decisionFeedbackFor({
    fold: true,
    notes: '',
    comments: [
      { jsonPath: '', anchorText: '', text: 'answering the cost question', replyTo: 'm0r1c1' },
    ],
  });
  assert.equal('comments' in body, false, 'no array means pdCheckNoReplyTo cannot fire');
  assert.equal(body.notes, 'answering the cost question', 'and the reply is not dropped');
});

void test('every OTHER decision keeps its comments as structure, replyTo included', () => {
  const comments = [
    { jsonPath: '$.mission', anchorText: 'Mission', text: 'tighten this', replyTo: '' },
    { jsonPath: '', anchorText: '', text: 'as discussed', replyTo: 'c7' },
  ];
  const body = decisionFeedbackFor({ fold: false, notes: 'see comments', comments });
  assert.deepEqual(body.comments, comments);
  assert.equal(body.notes, 'see comments');
});

void test('a comments-only non-M0 batch synthesizes notes, so a reject is never empty', () => {
  const body = decisionFeedbackFor({
    fold: false,
    notes: '',
    comments: [
      { jsonPath: '$.a', anchorText: 'a', text: 'first', replyTo: '' },
      { jsonPath: '$.b', anchorText: 'b', text: 'second', replyTo: '' },
    ],
  });
  assert.equal(body.notes, 'first\nsecond');
});

void test('the returned comments array is a COPY — a later stage edit cannot mutate the sent body', () => {
  const comments = [{ jsonPath: '$.a', anchorText: 'a', text: 'first', replyTo: '' }];
  const body = decisionFeedbackFor({ fold: false, notes: 'n', comments });
  assert.notEqual(body.comments, comments);
  assert.deepEqual(body.comments, comments);
});
