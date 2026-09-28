/**
 * The verbs table after the Manager merge: one op surface, and what each verb
 * SENDS rather than which rail it rides.
 *
 * The asymmetry tests are still the point of this file, and stage 4b1 INVERTED
 * them rather than deleting them: `deliveryManager` now answers comment-status,
 * questions, withdraw and task dispatch on the CONSTRUCTION rail, so these
 * assertions are what stops a later change from hiding buttons that work. The two
 * refusals that SURVIVE are asserted by name — the construction stale exit (a
 * semantic refusal: no slot, no basis flag) and M0's send-back and re-run (the plan
 * is derived and re-derived) — because a refusal nobody asserts is a refusal the
 * next reader deletes by accident.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  REVIEW_ADVANCE,
  REVIEW_APPROVE,
  REVIEW_REJECT,
  REVIEW_SET_COMMENT_STATUS,
  verbsFor,
} from './activityVerbs.ts';

const service = {
  type: 'service',
  taskId: 'detailedDesignReview',
  lifecyclePhase: 'detailedDesign',
  artifactKind: 'DetailedDesign',
};

void test('the wire ordinals are the appended ones, not re-numbered', () => {
  // ReviewDecision was WIDENED by appending: approve/reject keep 1/2 and the two
  // new members take 4/5. A renumber here would silently re-point every gate.
  assert.equal(REVIEW_APPROVE, 1);
  assert.equal(REVIEW_REJECT, 2);
  assert.equal(REVIEW_ADVANCE, 4);
  assert.equal(REVIEW_SET_COMMENT_STATUS, 5);
});

void test('a construction gate approves and sends back through the one decision op', () => {
  const v = verbsFor(service);
  assert.deepEqual(v.approve, { kind: 'decision', decision: REVIEW_APPROVE });
  assert.deepEqual(v.sendBack, { kind: 'decision', decision: REVIEW_REJECT });
  assert.equal(v.allowSendBack, true);
});

void test('a construction re-run is a real DISPATCH now — the redraft that re-opens the judged pair', () => {
  // Not `{ kind: 'override' }`: the override kept the variance loop and gained the
  // re-open, and neither of those is "run this gate's work again".
  assert.deepEqual(verbsFor(service).rerun, { kind: 'dispatch' });
});

void test('a construction thread resolves and reopens through the one comment-status op', () => {
  const v = verbsFor(service);
  assert.deepEqual(v.commentStatus, { kind: 'commentStatus' });
});

void test('a construction gate offers the ask verb — the questions land on the round', () => {
  const v = verbsFor(service);
  assert.deepEqual(v.ask, { kind: 'ask' });
  // No fold: `foldReplies` is the Phase-2 ledger's rule (pdCheckNoReplyTo) and a
  // construction round's thread routes a replyTo like the design rails do.
  assert.equal(v.ask.kind === 'ask' && v.ask.foldReplies, undefined);
});

void test('a construction gate still offers NO stale acknowledgement, and the reason is not a stage', () => {
  const v = verbsFor(service);
  assert.equal(v.acknowledgeStale.kind, 'none');
  // `assert.equal` is an assertion signature, so `reason` is reachable from here.
  assert.match(v.acknowledgeStale.reason, /design activity that owns the slot/);
  assert.doesNotMatch(
    v.acknowledgeStale.reason,
    /stage 4b/,
    'the refusal is semantic — a stage that has arrived cannot be the reason'
  );
  // The AMEND exit is the redraft this rail now has.
  assert.deepEqual(v.reconcileStale, { kind: 'dispatch' });
});

void test('a requirements review decides with the approve ordinal and carries no kind', () => {
  const v = verbsFor({
    type: 'requirements',
    taskId: 'missionReview',
    lifecyclePhase: 'mission',
    artifactKind: 'Mission',
  });
  assert.deepEqual(v.approve, { kind: 'decision', decision: REVIEW_APPROVE });
  assert.deepEqual(v.sendBack, { kind: 'decision', decision: REVIEW_REJECT });
  // The kind is the server's to resolve now — nothing in the target names one.
  assert.equal('artifactKind' in v.approve, false);
});

void test('a design review resolves its threads, asks, redrafts and acknowledges', () => {
  const v = verbsFor({
    type: 'architecture',
    taskId: 'architectureReview',
    lifecyclePhase: 'architecture',
    artifactKind: 'System',
  });
  assert.deepEqual(v.commentStatus, { kind: 'commentStatus' });
  assert.deepEqual(v.ask, { kind: 'ask' });
  assert.deepEqual(v.rerun, { kind: 'dispatch' });
  assert.deepEqual(v.acknowledgeStale, { kind: 'acknowledgeStale' });
  assert.deepEqual(v.reconcileStale, { kind: 'dispatch' });
});

void test('a design gate whose artifact kind the SPA does not know is refused, not guessed', () => {
  // 'SRS' is a construction artifact: it names no slot. A design activity that
  // somehow presents one must lose its verbs rather than address an artifact the
  // screen cannot render.
  const v = verbsFor({
    type: 'requirements',
    taskId: 'srsReview',
    lifecyclePhase: 'srs',
    artifactKind: 'SRS',
  });
  assert.equal(v.approve.kind, 'none');
  assert.equal(v.allowSendBack, false);
});

void test('a design gate with NO resolved kind is refused too', () => {
  const v = verbsFor({ type: 'requirements', taskId: 'missionReview', lifecyclePhase: 'mission' });
  assert.equal(v.approve.kind, 'none');
  assert.equal(v.ask.kind, 'none');
});

void test('projectDesign approves an OPTION, advances after, offers no send-back, keeps M0 wording', () => {
  const v = verbsFor({ type: 'projectDesign', taskId: 'sdpReview', lifecyclePhase: 'sdp' });
  assert.deepEqual(v.approve, {
    kind: 'decision',
    decision: REVIEW_APPROVE,
    needsOption: true,
  });
  assert.equal(v.advanceAfterApprove, true);
  assert.equal(v.sendBack.kind, 'none');
  assert.equal(v.allowSendBack, false);
  assert.match(v.approveCopy?.label ?? '', /Approve plan & cost/);
});

void test('the M0 gate resolves its own threads and asks — Phase-2 has both verbs, and a reply folds', () => {
  const v = verbsFor({ type: 'projectDesign', taskId: 'sdpReview', lifecyclePhase: 'sdp' });
  assert.deepEqual(v.commentStatus, { kind: 'commentStatus' });
  // foldReplies is what keeps "reply to an answered M0 question, then Ask" off
  // pdCheckNoReplyTo's 400 — the question-side twin of the decision's needsOption.
  assert.deepEqual(v.ask, { kind: 'ask', foldReplies: true });
  assert.deepEqual(v.acknowledgeStale, { kind: 'acknowledgeStale' });
});

void test('a DESIGN rail ask does not fold: that rail routes a replyTo', () => {
  const v = verbsFor({
    type: 'architecture',
    taskId: 'architectureReview',
    lifecyclePhase: 'architecture',
    artifactKind: 'System',
  });
  assert.equal(v.ask.kind === 'ask' && v.ask.foldReplies, undefined);
});

void test('the M0 gate has no re-run: the plan is re-derived, not re-dispatched', () => {
  const v = verbsFor({ type: 'projectDesign', taskId: 'sdpReview', lifecyclePhase: 'sdp' });
  assert.equal(v.rerun.kind, 'none');
  assert.match(v.rerun.reason, /re-derived/);
});

void test('every construction type offers the ask and the comment-status, and so do both design rails', () => {
  const constructionTypes = [
    'service',
    'frontend',
    'testing',
    'deployment',
    'documentation',
    'uiDesign',
    'integration',
  ];
  for (const type of constructionTypes) {
    const v = verbsFor({ type, taskId: 't', lifecyclePhase: 'p', artifactKind: 'Construction' });
    assert.equal(v.ask.kind, 'ask', `${type} must offer ask`);
    assert.equal(v.commentStatus.kind, 'commentStatus', `${type} must offer comment-status`);
    assert.equal(v.rerun.kind, 'dispatch', `${type} must offer a real re-run`);
  }
  for (const type of ['requirements', 'architecture']) {
    const v = verbsFor({ type, taskId: 't', lifecyclePhase: 'p', artifactKind: 'Mission' });
    assert.equal(v.ask.kind, 'ask', `${type} must offer ask`);
  }
  const m0 = verbsFor({ type: 'projectDesign', taskId: 'sdpReview', lifecyclePhase: 'sdp' });
  assert.equal(m0.ask.kind, 'ask');
});

void test('the two M0 refusals SURVIVE stage 4b1: no send-back and no re-run', () => {
  // Asserted here as a pair rather than inherited from the two cases above, because
  // these are the only refusals left in the table and they are refusals of PRODUCT,
  // not of plumbing: M0's plan is derived (so it is amended, never returned) and the
  // SDP is re-derived from the committed plan (so it is not re-dispatched).
  const v = verbsFor({ type: 'projectDesign', taskId: 'sdpReview', lifecyclePhase: 'sdp' });
  assert.equal(v.sendBack.kind, 'none');
  assert.match(v.sendBack.reason, /amending the Architecture/);
  assert.equal(v.rerun.kind, 'none');
  assert.match(v.rerun.reason, /re-derived/);
  assert.equal(v.allowSendBack, false);
  // Neither explains itself by a STAGE: the stage arrived, and these two did not go
  // with it, so a sentence naming one would be a promise nobody is keeping.
  assert.doesNotMatch(v.sendBack.reason, /stage 4b/);
  assert.doesNotMatch(v.rerun.reason, /stage 4b/);
});

void test('a testing activity is a construction activity — variant does not change the rail', () => {
  const v = verbsFor({
    type: 'testing',
    variant: 'systemTest',
    taskId: 'stpReview',
    lifecyclePhase: 'stp',
    artifactKind: 'STP',
  });
  assert.deepEqual(v.approve, { kind: 'decision', decision: REVIEW_APPROVE });
  assert.equal(v.ask.kind, 'ask');
  assert.equal(v.acknowledgeStale.kind, 'none');
});
