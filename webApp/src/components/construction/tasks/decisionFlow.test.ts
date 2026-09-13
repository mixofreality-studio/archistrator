/**
 * decisionFlow — what the console says after a human decides a gate (Stage C
 * Task 5). Spec §6: "confirmation is EVIDENCE of the resume, not acknowledgement
 * of the click" — the row says "resumed" only once the workflow has left the gate,
 * and a decision that lands nowhere is loud. A decision answers ONE gate
 * occurrence (review C2) and stays busy until the evidence is in (review I1/I5).
 */
/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import {
  RESUME_TIMEOUT_MS,
  RESUMED_LINGER_MS,
  decisionBusy,
  decisionNoteFor,
  decisionViewFor,
  observedGateFor,
  paneDecisionApplies,
  sendBackReady,
  type DecisionRecord,
  type ObservedGate,
} from './decisionFlow.ts';

const T0 = 1_000_000;

function rec(over: Partial<DecisionRecord> = {}): DecisionRecord {
  return {
    key: 'C-a:designReview:1',
    activityId: 'C-a',
    decision: 'approve',
    epoch: 1,
    gatedPhase: 'detailed_design',
    ...over,
  };
}

/** The gate as observed: occurrence 1 unless said otherwise. */
function at(stage: ObservedGate['stage'], over: Partial<ObservedGate> = {}): ObservedGate {
  return { stage, epoch: 1, ...over };
}

void test('before the server answers, the decision is sending', () => {
  assert.equal(decisionViewFor(rec(), at('awaitingApproval'), T0).kind, 'sending');
});

void test('an accepted decision is not "done" while the gate still waits', () => {
  const r = rec({ sentAt: T0 });
  assert.equal(decisionViewFor(r, at('awaitingApproval'), T0 + 2_000).kind, 'awaitingResume');
  // No session answer yet is not evidence either.
  assert.equal(decisionViewFor(r, { stage: undefined }, T0 + 2_000).kind, 'awaitingResume');
});

void test('an accepted decision stays busy until the resume evidence arrives (review I5)', () => {
  const r = rec({ sentAt: T0 });
  assert.equal(decisionBusy(decisionViewFor(r, at('awaitingApproval'), T0 + 2_000)), true);
  assert.equal(decisionBusy(decisionViewFor(rec(), at('awaitingApproval'), T0)), true);
  assert.equal(decisionBusy(decisionViewFor(r, at('pipelineRunning'), T0 + 2_000)), false);
  assert.equal(decisionBusy(undefined), false);
});

void test('still at the gate past the timeout: the decision did not land — loudly', () => {
  const r = rec({ sentAt: T0 });
  const view = decisionViewFor(r, at('awaitingApproval'), T0 + RESUME_TIMEOUT_MS + 1);
  assert.equal(view.kind, 'notLanded');
  const note = decisionNoteFor(view, r.decision);
  assert.ok(note);
  assert.equal(note.tone, 'danger');
  assert.match(note.text, /did not land/);
});

void test('a later occurrence of the gate retires the record: never "did not land" (review C2)', () => {
  // Send back → redraft → round 2, or approve → the next phase's gate: the session
  // is at awaitingApproval again, past the timeout, but it is a NEW gate.
  for (const decision of ['approve', 'sendBack'] as const) {
    const r = rec({ sentAt: T0, decision });
    const view = decisionViewFor(
      r,
      at('awaitingApproval', { epoch: 2 }),
      T0 + RESUME_TIMEOUT_MS + 1
    );
    assert.equal(view.kind, 'done');
    assert.equal(decisionBusy(view), false);
  }
  // A 4xx on the old occurrence retires too.
  const rejected = rec({ sentAt: T0, error: { status: 409, message: 'x' } });
  assert.equal(decisionViewFor(rejected, at('awaitingApproval', { epoch: 2 }), T0).kind, 'done');
});

void test('the workflow leaving the gate is the evidence: resumed, naming what was approved', () => {
  const r = rec({ sentAt: T0 });
  const view = decisionViewFor(r, at('pipelineRunning'), T0 + 3_000);
  assert.deepEqual(view, { kind: 'resumed', gatedPhase: 'detailed_design', escalated: false });
  assert.deepEqual(decisionNoteFor(view, 'approve'), {
    tone: 'ok',
    text: 'Resumed — Detailed Design approved',
  });
  // A session that no longer exists (the activity finished) is a resume too.
  assert.equal(decisionViewFor(r, at(null), T0 + 3_000).kind, 'resumed');
  // A late landing after "did not land" still turns into resumed.
  assert.equal(
    decisionViewFor(r, at('pipelineRunning'), T0 + RESUME_TIMEOUT_MS + 5_000).kind,
    'resumed'
  );
});

void test('"now in <phase>" only from a project read taken after the gate was left', () => {
  const occ = {
    stage: 'pipelineRunning' as const,
    epoch: 1,
    seenAt: T0 + 3_000,
    leftAt: T0 + 3_000,
  };
  // The read predates the resume: it still names the gated phase — no claim.
  const stale = observedGateFor(occ, { at: T0 + 1_000, lifecyclePhase: 'detailed_design' });
  assert.equal(stale.lifecyclePhase, undefined);
  const fresh = observedGateFor(occ, { at: T0 + 4_000, lifecyclePhase: 'construction' });
  assert.equal(fresh.lifecyclePhase, 'construction');
  const view = decisionViewFor(rec({ sentAt: T0 }), fresh, T0 + 4_000);
  assert.equal(decisionNoteFor(view, 'approve')?.text, 'Resumed — now in Construction');
  // Never at the gate's left side yet: nothing to claim.
  const still = observedGateFor(
    { stage: 'awaitingApproval', epoch: 1, seenAt: T0 },
    { at: T0 + 9_000, lifecyclePhase: 'construction' }
  );
  assert.equal(still.lifecyclePhase, undefined);
  assert.deepEqual(observedGateFor(undefined, { at: T0, lifecyclePhase: 'x' }), {
    stage: undefined,
  });
});

void test('leaving the gate for an operator steer reads "Escalated", not "Resumed"', () => {
  const view = decisionViewFor(rec({ sentAt: T0 }), at('awaitingTakeover'), T0 + 3_000);
  const note = decisionNoteFor(view, 'approve');
  assert.match(note?.text ?? '', /^Escalated/);
  assert.equal(note?.tone, 'danger');
});

void test('the resumed row lingers, then leaves', () => {
  const r = rec({ sentAt: T0 });
  assert.equal(
    decisionViewFor(r, at('pipelineRunning'), T0 + RESUMED_LINGER_MS - 1).kind,
    'resumed'
  );
  assert.equal(decisionViewFor(r, at('pipelineRunning'), T0 + RESUMED_LINGER_MS + 1).kind, 'done');
});

void test('a 4xx is a rejection; a 5xx or no answer is an unknown outcome (the fix-C rule)', () => {
  const rejected = decisionViewFor(
    rec({ sentAt: T0, error: { status: 400, message: 'unknown phase' } }),
    at('awaitingApproval'),
    T0
  );
  assert.ok(rejected.kind === 'failed');
  assert.equal(rejected.outcome.kind, 'rejected');
  assert.match(decisionNoteFor(rejected, 'approve')?.text ?? '', /unknown phase/);
  // A rejection decided nothing: the human may decide again at once.
  assert.equal(decisionBusy(rejected), false);

  const unknown = decisionViewFor(
    rec({ sentAt: T0, error: { message: 'network' } }),
    at('awaitingApproval'),
    T0
  );
  assert.equal(unknown.kind === 'failed' ? unknown.outcome.kind : '', 'unknown');
  assert.equal(decisionNoteFor(unknown, 'approve')?.tone, 'danger');
  // An unknown outcome keeps watching: if the gate clears, the row says resumed.
  const cleared = decisionViewFor(
    rec({ sentAt: T0, error: { status: 502, message: 'bad gateway' } }),
    at('pipelineRunning'),
    T0
  );
  assert.equal(cleared.kind, 'resumed');
});

void test('a 4xx whose gate then clears is still a rejection, never "Resumed" (review I5)', () => {
  const view = decisionViewFor(
    rec({ sentAt: T0, error: { status: 409, message: 'not at that gate' } }),
    at('pipelineRunning'),
    T0 + 3_000
  );
  assert.equal(view.kind, 'failed');
  assert.match(decisionNoteFor(view, 'approve')?.text ?? '', /^Rejected/);
});

void test('an unknown outcome keeps the decision off until a newer session read (review I1)', () => {
  const r = rec({ sentAt: T0, error: { status: 503, message: 'unavailable' } });
  const before = decisionViewFor(r, at('awaitingApproval', { seenAt: T0 - 1 }), T0 + 1_000);
  assert.ok(before.kind === 'failed' && before.watching);
  assert.equal(decisionBusy(before), true);
  const noRead = decisionViewFor(r, at('awaitingApproval'), T0 + 1_000);
  assert.equal(decisionBusy(noRead), true);
  // A read newer than the failure still shows the gate: the human may decide again.
  const after = decisionViewFor(r, at('awaitingApproval', { seenAt: T0 + 3_000 }), T0 + 3_000);
  assert.ok(after.kind === 'failed' && !after.watching);
  assert.equal(decisionBusy(after), false);
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
