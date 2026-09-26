/**
 * The verbs table after the Manager merge: one op surface, and what each verb
 * SENDS rather than which rail it rides.
 *
 * The asymmetry tests are the point of this file. Stage 4a was expected to close
 * R2/GAP-6, and it did NOT: `deliveryManager` refuses comment-status, questions,
 * stale-basis and task dispatch on the CONSTRUCTION rail until stage 4b. These
 * assertions are what stops a later change from offering those buttons and
 * dispatching a guaranteed 400.
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

void test('a construction re-run is an Override, because DispatchActivityTask refuses the rail', () => {
  assert.deepEqual(verbsFor(service).rerun, { kind: 'override' });
});

void test('a construction thread offers no comment-status verb, and says why', () => {
  const v = verbsFor(service);
  assert.equal(v.commentStatus.kind, 'none');
  // `assert.equal` is an assertion signature, so `reason` is reachable from here.
  assert.match(v.commentStatus.reason, /comment-status.*stage 4b/);
});

void test('a construction gate offers no ask verb either — the same refusal', () => {
  const v = verbsFor(service);
  assert.equal(v.ask.kind, 'none');
  assert.match(v.ask.reason, /stage 4b/);
});

void test('a construction gate offers neither stale exit — no stale-basis verb on that rail', () => {
  const v = verbsFor(service);
  assert.equal(v.acknowledgeStale.kind, 'none');
  assert.equal(v.reconcileStale.kind, 'none');
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

void test('the M0 gate resolves its own threads and asks — Phase-2 has both verbs', () => {
  const v = verbsFor({ type: 'projectDesign', taskId: 'sdpReview', lifecyclePhase: 'sdp' });
  assert.deepEqual(v.commentStatus, { kind: 'commentStatus' });
  assert.deepEqual(v.ask, { kind: 'ask' });
  assert.deepEqual(v.acknowledgeStale, { kind: 'acknowledgeStale' });
});

void test('the M0 gate has no re-run: the plan is re-derived, not re-dispatched', () => {
  const v = verbsFor({ type: 'projectDesign', taskId: 'sdpReview', lifecyclePhase: 'sdp' });
  assert.equal(v.rerun.kind, 'none');
  assert.match(v.rerun.reason, /re-derived/);
});

void test('every construction type refuses the ask, and the two design rails offer it', () => {
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
    assert.equal(v.ask.kind, 'none', `${type} must not offer ask before stage 4b`);
    assert.equal(v.commentStatus.kind, 'none', `${type} must not offer comment-status`);
  }
  for (const type of ['requirements', 'architecture']) {
    const v = verbsFor({ type, taskId: 't', lifecyclePhase: 'p', artifactKind: 'Mission' });
    assert.equal(v.ask.kind, 'ask', `${type} must offer ask`);
  }
  const m0 = verbsFor({ type: 'projectDesign', taskId: 'sdpReview', lifecyclePhase: 'sdp' });
  assert.equal(m0.ask.kind, 'ask');
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
  assert.equal(v.ask.kind, 'none');
});
