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
  decidedChipLabel,
  decidedFor,
  decisionBusy,
  decisionLeadFor,
  decisionNoteFor,
  decisionViewFor,
  gateControlFor,
  observedGateFor,
  PREVIOUS_STILL_SENDING,
  paneDecisionApplies,
  sendBackCaptionFor,
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
    requestedAt: T0 + 2_900,
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
    { stage: 'awaitingApproval', epoch: 1, requestedAt: T0 },
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
  const before = decisionViewFor(r, at('awaitingApproval', { requestedAt: T0 - 1 }), T0 + 1_000);
  assert.ok(before.kind === 'failed' && before.watching);
  assert.equal(decisionBusy(before), true);
  const noRead = decisionViewFor(r, at('awaitingApproval'), T0 + 1_000);
  assert.equal(decisionBusy(noRead), true);
  // A read asked for after the failure still shows the gate: decide again.
  const after = decisionViewFor(r, at('awaitingApproval', { requestedAt: T0 + 3_000 }), T0 + 3_000);
  assert.ok(after.kind === 'failed' && !after.watching);
  assert.equal(decisionBusy(after), false);
});

void test('a read counts from when it was REQUESTED: one in flight at the failure is not evidence (round 2)', () => {
  const r = rec({ sentAt: T0, error: { status: 503, message: 'unavailable' } });
  // Requested before the failure came back, even if it arrives later: it describes
  // the gate from before the decision, so the buttons stay off.
  const stale = decisionViewFor(r, at('awaitingApproval', { requestedAt: T0 - 50 }), T0 + 5_000);
  assert.ok(stale.kind === 'failed' && stale.watching);
  assert.equal(decisionBusy(stale), true);
  // Requested at the very instant of the failure is not after it either.
  const same = decisionViewFor(r, at('awaitingApproval', { requestedAt: T0 }), T0 + 5_000);
  assert.equal(decisionBusy(same), true);
  // An unknown request time (0) never counts.
  const unknownTime = decisionViewFor(r, at('awaitingApproval', { requestedAt: 0 }), T0 + 5_000);
  assert.equal(decisionBusy(unknownTime), true);
});

void test('a POST still on the wire holds the gate after its record retires (round 2 I1)', () => {
  // The reviewer's repro: the approval is held on the wire; the gate is left and
  // re-entered, so the record (epoch 1) is retired by occurrence 2 — while the
  // request is still pending.
  const pending = rec();
  const retired = decisionViewFor(pending, at('awaitingApproval', { epoch: 2 }), T0 + 9_000);
  assert.equal(retired.kind, 'done');
  // Its own view alone would re-enable the buttons; the activity's pending request
  // keeps them off, and says why.
  assert.deepEqual(gateControlFor(retired, 'approve', true), {
    busy: true,
    note: PREVIOUS_STILL_SENDING,
  });
  assert.equal(PREVIOUS_STILL_SENDING.text, 'Previous decision still sending…');
  // No record for this gate at all, but another decision of the activity on the wire.
  assert.deepEqual(gateControlFor(undefined, undefined, true), {
    busy: true,
    note: PREVIOUS_STILL_SENDING,
  });
  // Once it settles, the new gate is a fresh decision.
  assert.deepEqual(gateControlFor(retired, 'approve', false), { busy: false });
  assert.deepEqual(gateControlFor(undefined, undefined, false), { busy: false });
  // A live view keeps its own line, and pending holds it busy even where the view
  // alone would not (a rejection).
  const rejected = decisionViewFor(
    rec({ sentAt: T0, error: { status: 400, message: 'x' } }),
    at('awaitingApproval'),
    T0
  );
  const held = gateControlFor(rejected, 'approve', true);
  assert.equal(held.busy, true);
  assert.match(held.note?.text ?? '', /^Rejected/);
  assert.equal(gateControlFor(rejected, 'approve', false).busy, false);
  const sending = gateControlFor(
    decisionViewFor(rec(), at('awaitingApproval'), T0),
    'approve',
    false
  );
  assert.equal(sending.busy, true);
  assert.match(sending.note?.text ?? '', /Sending your approval/);
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

void test('a send-back says its note was not delivered — before and after sending (designer P0-1)', () => {
  assert.equal(
    sendBackCaptionFor('detailed_design'),
    'Your note goes with the decision, but this redraft does not read it yet — the agent re-runs Detailed Design from its original brief.'
  );
  const view = decisionViewFor(
    rec({ sentAt: T0, decision: 'sendBack' }),
    at('pipelineRunning'),
    T0 + 3_000
  );
  assert.deepEqual(decisionNoteFor(view, 'sendBack'), {
    tone: 'ok',
    text: 'Sent back — re-running Detailed Design (your note was not delivered)',
  });
});

void test('the pane says what was decided while the record lives, and when (designer P1-4)', () => {
  const r = rec({ sentAt: T0, decidedAt: T0 - 500 });
  const accepted = decidedFor(r, decisionViewFor(r, at('awaitingApproval'), T0 + 1_000));
  assert.deepEqual(accepted, { decision: 'approve', at: T0 - 500 });
  // Still on the wire, the server may yet refuse it: not "Decided" yet (round 2).
  const onTheWire = rec({ decidedAt: T0 - 500 });
  assert.equal(
    decidedFor(onTheWire, decisionViewFor(onTheWire, at('awaitingApproval'), T0)),
    undefined
  );
  assert.ok(decidedFor(r, decisionViewFor(r, at('pipelineRunning'), T0 + 1_000)));
  // Louder states say their own thing; a retired record says nothing.
  assert.equal(
    decidedFor(r, decisionViewFor(r, at('awaitingApproval'), T0 + RESUME_TIMEOUT_MS + 1)),
    undefined
  );
  assert.equal(decidedFor(r, decisionViewFor(r, at('awaitingTakeover'), T0 + 1_000)), undefined);
  assert.equal(decidedFor(r, { kind: 'done' }), undefined);
  assert.equal(decidedFor(rec({ sentAt: T0 }), { kind: 'awaitingResume' }), undefined);
  assert.equal(decidedChipLabel('approve'), 'Decided · approved');
  assert.equal(decidedChipLabel('sendBack'), 'Sent back');
  const nine = new Date(2026, 8, 12, 9, 5).getTime();
  assert.equal(
    decisionLeadFor({ decision: 'approve', at: nine }),
    'You approved this at 09:05; gate decisions are not yet written to the task ledger.'
  );
  assert.equal(
    decisionLeadFor({ decision: 'sendBack', at: nine }),
    'You sent this back at 09:05; gate decisions are not yet written to the task ledger.'
  );
});

// Tasks round-2 review, minor: retiring on a newer occurrence ran BEFORE the error
// branch. The reviewer's repro: hold the POST, let the gate leave and re-open, then
// answer 500 — Approve came back enabled on the new gate 26ms later.
void test('an unknown outcome with no read asked for since its answer holds its activity on a newer occurrence', () => {
  const unknown = rec({
    sentAt: T0 + 5_000,
    error: { status: 500, message: 'Internal Server Error' },
  });
  // The gate left and re-opened while the POST was on the wire (epoch 2), and the
  // read showing it was asked for BEFORE the 500 came back.
  const reopened = at('awaitingApproval', { epoch: 2, requestedAt: T0 + 4_000 });
  const held = decisionViewFor(unknown, reopened, T0 + 5_026);
  assert.deepEqual(held, {
    kind: 'failed',
    outcome: { kind: 'unknown', message: 'Internal Server Error' },
    watching: true,
  });
  assert.equal(decisionBusy(held), true);
  assert.equal(gateControlFor(held, 'approve', false).busy, true);
  // The same instant is not newer.
  const same = at('awaitingApproval', { epoch: 2, requestedAt: T0 + 5_000 });
  assert.equal(decisionViewFor(unknown, same, T0 + 6_000).kind, 'failed');
  // A read asked for after the answer: the record retires, and the new gate is a
  // fresh decision.
  const newer = at('awaitingApproval', { epoch: 2, requestedAt: T0 + 5_001 });
  assert.equal(decisionViewFor(unknown, newer, T0 + 6_000).kind, 'done');
  assert.deepEqual(gateControlFor(decisionViewFor(unknown, newer, T0 + 6_000), 'approve', false), {
    busy: false,
  });
  // Superseded and left again, still with no newer read: held, never "Resumed" for
  // a gate this record did not answer.
  const leftAgain = at('pipelineRunning', { epoch: 2, requestedAt: T0 + 4_000 });
  assert.equal(decisionViewFor(unknown, leftAgain, T0 + 6_000).kind, 'failed');
  // A 4xx decided nothing: it retires on a newer occurrence at once.
  const rejected: DecisionRecord = { ...unknown, error: { status: 409, message: 'no' } };
  assert.equal(decisionViewFor(rejected, reopened, T0 + 5_026).kind, 'done');
});
