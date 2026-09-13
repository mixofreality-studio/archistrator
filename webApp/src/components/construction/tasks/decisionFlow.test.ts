/**
 * decisionFlow — what the console says after a human decides a gate (Stage C
 * Task 5). Spec §6: "confirmation is EVIDENCE of the resume, not acknowledgement
 * of the click" — the row says "resumed" only once the workflow has left the gate,
 * and a decision that lands nowhere is loud.
 */
/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import {
  RESUME_TIMEOUT_MS,
  RESUMED_LINGER_MS,
  decisionNoteFor,
  decisionViewFor,
  paneDecisionApplies,
  sendBackReady,
  type DecisionRecord,
} from './decisionFlow.ts';

const T0 = 1_000_000;

function rec(over: Partial<DecisionRecord> = {}): DecisionRecord {
  return { key: 'C-a:designReview:1', activityId: 'C-a', decision: 'approve', ...over };
}

void test('before the server answers, the decision is sending', () => {
  assert.equal(decisionViewFor(rec(), { stage: 'awaitingApproval' }, T0).kind, 'sending');
});

void test('an accepted decision is not "done" while the gate still waits', () => {
  const r = rec({ sentAt: T0 });
  assert.equal(
    decisionViewFor(r, { stage: 'awaitingApproval' }, T0 + 2_000).kind,
    'awaitingResume'
  );
  // No session answer yet is not evidence either.
  assert.equal(decisionViewFor(r, { stage: undefined }, T0 + 2_000).kind, 'awaitingResume');
});

void test('still at the gate past the timeout: the decision did not land — loudly', () => {
  const r = rec({ sentAt: T0 });
  const view = decisionViewFor(r, { stage: 'awaitingApproval' }, T0 + RESUME_TIMEOUT_MS + 1);
  assert.equal(view.kind, 'notLanded');
  const note = decisionNoteFor(view, r.decision);
  assert.ok(note);
  assert.equal(note.tone, 'danger');
  assert.match(note.text, /did not land/);
});

void test('the workflow leaving the gate is the evidence: resumed, naming where it went', () => {
  const r = rec({ sentAt: T0 });
  const view = decisionViewFor(
    r,
    { stage: 'pipelineRunning', lifecyclePhase: 'construction' },
    T0 + 3_000
  );
  assert.deepEqual(view, { kind: 'resumed', lifecyclePhase: 'construction' });
  assert.deepEqual(decisionNoteFor(view, 'approve'), {
    tone: 'ok',
    text: 'Resumed — now in Construction',
  });
  // A session that no longer exists (the activity finished) is a resume too.
  assert.equal(decisionViewFor(r, { stage: null }, T0 + 3_000).kind, 'resumed');
  // A late landing after "did not land" still turns into resumed.
  assert.equal(
    decisionViewFor(r, { stage: 'pipelineRunning' }, T0 + RESUME_TIMEOUT_MS + 5_000).kind,
    'resumed'
  );
});

void test('the resumed row lingers, then leaves', () => {
  const r = rec({ sentAt: T0 });
  assert.equal(
    decisionViewFor(r, { stage: 'pipelineRunning' }, T0 + RESUMED_LINGER_MS - 1).kind,
    'resumed'
  );
  assert.equal(
    decisionViewFor(r, { stage: 'pipelineRunning' }, T0 + RESUMED_LINGER_MS + 1).kind,
    'done'
  );
});

void test('a 4xx is a rejection; a 5xx or no answer is an unknown outcome (the fix-C rule)', () => {
  const rejected = decisionViewFor(
    rec({ error: { status: 400, message: 'unknown phase' } }),
    { stage: 'awaitingApproval' },
    T0
  );
  assert.ok(rejected.kind === 'failed');
  assert.equal(rejected.outcome.kind, 'rejected');
  assert.match(decisionNoteFor(rejected, 'approve')?.text ?? '', /unknown phase/);

  const unknown = decisionViewFor(
    rec({ error: { message: 'network' } }),
    { stage: 'awaitingApproval' },
    T0
  );
  assert.equal(unknown.kind === 'failed' ? unknown.outcome.kind : '', 'unknown');
  assert.equal(decisionNoteFor(unknown, 'approve')?.tone, 'danger');
  // An unknown outcome keeps watching: if the gate clears, the row says resumed.
  const cleared = decisionViewFor(
    rec({ error: { status: 502, message: 'bad gateway' } }),
    { stage: 'pipelineRunning' },
    T0
  );
  assert.equal(cleared.kind, 'resumed');
});

void test('send back needs a note — typed, or anchored comments', () => {
  assert.equal(sendBackReady('', 0), false);
  assert.equal(sendBackReady('   \n', 0), false);
  assert.equal(sendBackReady('the ops list drops refund', 0), true);
  assert.equal(sendBackReady('', 2), true);
});

void test('notes name the decision while it is in flight', () => {
  assert.match(decisionNoteFor({ kind: 'sending' }, 'approve')?.text ?? '', /approval/i);
  assert.match(decisionNoteFor({ kind: 'sending' }, 'sendBack')?.text ?? '', /send/i);
  assert.equal(decisionNoteFor({ kind: 'awaitingResume' }, 'approve')?.tone, 'progress');
  assert.equal(decisionNoteFor({ kind: 'done' }, 'approve'), undefined);
});

void test('the pane offers the decision only on the gated activity, phase or gate task', () => {
  const gate = { lifecyclePhase: 'detailed_design', gateTask: 'designReview' };
  assert.equal(paneDecisionApplies(gate, { activityId: 'C-a' }), true);
  assert.equal(
    paneDecisionApplies(gate, { activityId: 'C-a', lifecyclePhase: 'detailed_design' }),
    true
  );
  assert.equal(
    paneDecisionApplies(gate, {
      activityId: 'C-a',
      lifecyclePhase: 'detailed_design',
      task: 'designReview',
    }),
    true
  );
  // Another task of the gated phase, or another phase, is not where this gate is decided.
  assert.equal(
    paneDecisionApplies(gate, {
      activityId: 'C-a',
      lifecyclePhase: 'detailed_design',
      task: 'detailedDesign',
    }),
    false
  );
  assert.equal(
    paneDecisionApplies(gate, { activityId: 'C-a', lifecyclePhase: 'construction' }),
    false
  );
});
