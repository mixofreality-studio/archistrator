/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  ACTIVITY_LOADING,
  ACTIVITY_NOT_IN_PLAN,
  activityReadFailed,
  ADVANCE_ANYWAY,
  ADVANCE_RETRY,
  advanceFailed,
  AMEND_ARCHITECTURE,
  artifactNotOfThisPhase,
  artifactUnavailable,
  CONSTRUCTION_THREAD_READ_ONLY,
  CONTRACT_BY_DESIGN,
  CONTRACT_MISSING,
  CONTRACT_UNRESOLVED,
  dispatchRoleLine,
  DISPATCH_JOB_NOTE,
  eyebrowFor,
  historyBanner,
  HISTORY_ARTIFACT_CAPTION,
  NO_ARTIFACT_KIND,
  NO_CONSTRUCTION_RECORD,
  NO_EPISODE_CAPTURED,
  notDispatchedYet,
  planIndexFor,
  RECONCILE_RATIONALE,
  REVISION_NOTE_LABEL,
  reviewerChipLabel,
  reviewSetRefused,
  rosterSeatLabel,
  subAttemptsLine,
  verdictLine,
} from './activityCopy.ts';

void test('the eyebrow names the activity and its type, or its plan position when it has one', () => {
  assert.equal(eyebrowFor({ activityId: 'R-github', type: 'deployment' }), 'R-GITHUB · DEPLOYMENT');
  assert.equal(
    eyebrowFor({ activityId: 'architecture', type: 'architecture', planIndex: 2 }),
    'ACTIVITY 2 · ARCHITECTURE'
  );
  assert.equal(
    eyebrowFor({ activityId: 'N-STP', type: 'testing', variant: 'plan' }),
    'N-STP · TESTING:PLAN'
  );
});

void test('the history banner counts from one and says read-only', () => {
  assert.equal(historyBanner(2, 3), 'Revision 2 of 3 — read-only');
});

void test('the sub-attempt line says nothing at one attempt or none', () => {
  assert.equal(subAttemptsLine(0), '');
  assert.equal(subAttemptsLine(1), '');
  assert.equal(subAttemptsLine(3), '3 attempts before this revision reached the gate');
});

void test('an unavailable artifact names what is missing and what is still there', () => {
  assert.equal(
    artifactUnavailable('deployment'),
    'No artifact view for a deployment activity yet. Its episodes and review history are below.'
  );
});

void test('the engine’s refusal is quoted, not paraphrased', () => {
  assert.equal(
    reviewSetRefused('unknown artifact kind "detailed_design"'),
    'The review engine could not propose reviewers: unknown artifact kind "detailed_design"'
  );
});

void test('the standing sentences say the thing they exist to say', () => {
  assert.match(HISTORY_ARTIFACT_CAPTION, /^Showing the current artifact\./);
  assert.match(HISTORY_ARTIFACT_CAPTION, /not readable yet/);
  assert.match(CONSTRUCTION_THREAD_READ_ONLY, /cannot be resolved, reopened or replied to/);
  assert.match(ACTIVITY_NOT_IN_PLAN, /committed activity list/);
  assert.match(ACTIVITY_LOADING, /Reading this activity/);
  assert.match(NO_EPISODE_CAPTURED, /No episode was captured/);
  assert.match(NO_ARTIFACT_KIND, /names no artifact kind/);
  assert.match(NO_CONSTRUCTION_RECORD, /Nothing has been recorded/);
  assert.equal(AMEND_ARCHITECTURE, 'Amend Architecture');
});

void test('an artifact that belongs to another phase is a DIFFERENT sentence from one with no view', () => {
  assert.equal(
    artifactNotOfThisPhase('service'),
    "A service activity's artifact belongs to another phase of its lifecycle, so this task has none to show. Its review history is below."
  );
  assert.notEqual(artifactNotOfThisPhase('service'), artifactUnavailable('service'));
});

void test('the three contract absences are three different facts', () => {
  assert.match(CONTRACT_MISSING, /No service contract is recorded/);
  assert.match(CONTRACT_BY_DESIGN, /resource or a utility/);
  assert.match(CONTRACT_UNRESOLVED, /do not place this activity/);
  assert.equal(new Set([CONTRACT_MISSING, CONTRACT_BY_DESIGN, CONTRACT_UNRESOLVED]).size, 3);
});

void test('a reviewer chip says whether they may amend, in words', () => {
  assert.equal(
    reviewerChipLabel({ role: 'architect', perspective: 'layering', mayAmend: true }),
    'architect · layering · may amend'
  );
  assert.equal(
    reviewerChipLabel({ role: 'qaEngineer', perspective: '', mayAmend: false }),
    'qaEngineer · advises only'
  );
});

void test('a roster seat names the actor who filled it and whether it could be skipped', () => {
  assert.equal(
    rosterSeatLabel({ role: 'architect', actor: 'system-architect', required: true }),
    'architect · system-architect · required'
  );
  assert.equal(
    rosterSeatLabel({ role: 'productManager', actor: '', required: false }),
    'productManager · optional'
  );
});

void test('a verdict line is the record verbatim, and omits what the record does not carry', () => {
  assert.equal(
    verdictLine({
      reviewerRole: 'architect',
      verdict: 'approve',
      summary: 'layering holds',
      at: '2026-09-12T10:00:00Z',
    }),
    'architect · approve · layering holds · 2026-09-12T10:00:00Z'
  );
  assert.equal(
    verdictLine({ reviewerRole: 'qaEngineer', verdict: 'abstain', at: '' }),
    'qaEngineer · abstain'
  );
});

void test('only the three design activities carry a plan position', () => {
  assert.equal(planIndexFor('requirements'), 1);
  assert.equal(planIndexFor('architecture'), 2);
  assert.equal(planIndexFor('projectDesign'), 3);
  assert.equal(planIndexFor('service'), undefined);
  assert.equal(planIndexFor('testing'), undefined);
});

void test('a failed activity read quotes the detail and says it is retrying', () => {
  assert.equal(
    activityReadFailed('502 Bad Gateway'),
    'Could not read this activity: 502 Bad Gateway. Retrying.'
  );
});

void test('the dispatch role line names the charter and the task, and seeds the avatar', () => {
  assert.deepEqual(dispatchRoleLine('junior-developer', 'Construction'), {
    seed: 'junior-developer',
    text: 'Junior developer is working on Construction',
  });
  // An id the label table does not know is shown verbatim, never de-hyphenated
  // into "Ui designer"-style nonsense.
  assert.equal(dispatchRoleLine('ui-designer', 'Flows').text, 'UI designer is working on Flows');
  assert.equal(dispatchRoleLine('future-role', 'X').text, 'future-role is working on X');
});

void test('the dispatch footer names no venue, because this read reports none', () => {
  assert.doesNotMatch(DISPATCH_JOB_NOTE, /GitHub|Actions/);
  assert.match(DISPATCH_JOB_NOTE, /keeps running if you close this screen/);
});

void test('an undispatched task says why, and a locked one says which why', () => {
  assert.match(notDispatchedYet(true), /^Locked/);
  assert.equal(notDispatchedYet(false), 'Nothing has been dispatched on this task yet.');
});

void test('a failed advance says the commit landed and construction did not start', () => {
  assert.equal(
    advanceFailed('slot activityList is stale'),
    'The plan was committed, but construction did not start: slot activityList is stale'
  );
  // The two exits are different sentences: one re-runs the advance as asked, the
  // other acknowledges the stale slots and seals over them.
  assert.notEqual(ADVANCE_RETRY, ADVANCE_ANYWAY);
  assert.match(ADVANCE_ANYWAY, /acknowledge/);
});

void test('the revision note is labelled as the reviewer’s own words', () => {
  assert.equal(REVISION_NOTE_LABEL, 'SEND-BACK NOTE');
});

void test('a reconcile amendment carries the same rationale as the design rail’s', () => {
  // ProjectDesignExperience.tsx's reconcileRationale, verbatim — one reconcile
  // must not read differently in the ledger for being launched from here.
  assert.equal(RECONCILE_RATIONALE, 'Reconcile with amended upstream basis.');
});
