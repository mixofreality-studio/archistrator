/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import { verbsFor } from './activityVerbs.ts';

void test('a construction gate approves through the phase decision, keyed on the task own lifecycle phase', () => {
  const v = verbsFor({
    type: 'service',
    taskId: 'designReview',
    lifecyclePhase: 'detailed_design',
  });
  assert.deepEqual(v.approve, {
    kind: 'constructionPhaseDecision',
    lifecyclePhase: 'detailed_design',
  });
  assert.equal(v.allowSendBack, true);
});

void test('a construction thread offers no comment-status verb, and says why', () => {
  const v = verbsFor({
    type: 'service',
    taskId: 'designReview',
    lifecyclePhase: 'detailed_design',
  });
  assert.equal(v.commentStatus.kind, 'none');
  assert.match((v.commentStatus as { reason: string }).reason, /cannot yet be resolved/);
});

void test('a construction gate offers no ask verb either — the same missing op', () => {
  const v = verbsFor({ type: 'frontend', taskId: 'codeReview', lifecyclePhase: 'construction' });
  assert.equal(v.ask.kind, 'none');
  assert.deepEqual(v.rerun, { kind: 'constructionPhaseDecision', lifecyclePhase: 'construction' });
});

void test('a requirements review decides on its artifact kind, not on a phase', () => {
  // 'Glossary' is what `taskFactsFor` RESOLVED for `glossaryReview` — through
  // its `reviews: 'glossaryDraft'` hop, since the review task carries no kind
  // of its own. Passing the raw `task.artifactKind` here would pass undefined.
  const v = verbsFor({
    type: 'requirements',
    taskId: 'glossaryReview',
    lifecyclePhase: 'glossary',
    artifactKind: 'Glossary',
  });
  assert.deepEqual(v.approve, { kind: 'designReviewDecision', artifactKind: 'glossary' });
});

void test('a design review resolves its threads and asks its questions on the same rail', () => {
  const v = verbsFor({
    type: 'requirements',
    taskId: 'missionReview',
    lifecyclePhase: 'mission',
    artifactKind: 'Mission',
  });
  assert.deepEqual(v.commentStatus, { kind: 'designReviewDecision', artifactKind: 'mission' });
  assert.deepEqual(v.ask, { kind: 'designReviewDecision', artifactKind: 'mission' });
  assert.deepEqual(v.rerun, { kind: 'designReviewDecision', artifactKind: 'mission' });
});

void test('the resolved kind is what the caller passes — the raw table has none for a review', () => {
  // Guards the seam between Task 2 and Task 9: verbsFor must be fed the
  // RESOLVED kind, and it must refuse to invent one when it is missing.
  const v = verbsFor({
    type: 'architecture',
    taskId: 'architectureReview',
    lifecyclePhase: 'architecture',
  });
  assert.equal(v.approve.kind, 'none');
  assert.match((v.approve as { reason: string }).reason, /artifact kind/);
  assert.equal(v.allowSendBack, false);
  const resolved = verbsFor({
    type: 'architecture',
    taskId: 'architectureReview',
    lifecyclePhase: 'architecture',
    artifactKind: 'System',
  });
  assert.deepEqual(resolved.approve, { kind: 'designReviewDecision', artifactKind: 'system' });
});

void test('a construction artifact kind has no app-string counterpart and is refused, not guessed', () => {
  // 'SRS' / 'DetailedDesign' / 'Construction' / 'Integration' / 'STP' are
  // construction kinds; ARTIFACT_KIND_APP_STRINGS holds only the 17 slot kinds.
  const v = verbsFor({
    type: 'service',
    taskId: 'srsReview',
    lifecyclePhase: 'requirements',
    artifactKind: 'SRS',
  });
  assert.equal(
    v.approve.kind,
    'constructionPhaseDecision',
    'a service gate never routes through the design op'
  );
});

void test('projectDesign approves the SDP, offers no send-back, and carries the M0 wording', () => {
  const v = verbsFor({ type: 'projectDesign', taskId: 'sdpReview', lifecyclePhase: 'sdp' });
  assert.deepEqual(v.approve, { kind: 'sdpDecision' });
  assert.equal(v.sendBack.kind, 'none');
  assert.equal(v.allowSendBack, false);
  assert.equal(v.approveCopy?.label, 'Approve plan & cost — start construction');
});

void test('the M0 gate still resolves its own threads — Phase-2 has that op', () => {
  const v = verbsFor({
    type: 'projectDesign',
    taskId: 'sdpReview',
    lifecyclePhase: 'sdp',
    artifactKind: 'SdpReview',
  });
  assert.deepEqual(v.commentStatus, { kind: 'sdpDecision' });
  assert.equal(v.ask.kind, 'none');
  assert.equal(v.rerun.kind, 'none');
});

void test('a testing activity is a construction activity — variant does not change the rail', () => {
  const v = verbsFor({
    type: 'testing',
    variant: 'systemTest',
    taskId: 'testing',
    lifecyclePhase: 'integration',
  });
  assert.deepEqual(v.approve, { kind: 'constructionPhaseDecision', lifecyclePhase: 'integration' });
});
