/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  ACTIVITY_LOADING,
  ACTIVITY_NOT_IN_PLAN,
  activityReadFailed,
  artifactUnavailable,
  CONSTRUCTION_THREAD_READ_ONLY,
  dispatchRoleLine,
  DISPATCH_JOB_NOTE,
  eyebrowFor,
  historyBanner,
  HISTORY_ARTIFACT_CAPTION,
  NO_EPISODE_CAPTURED,
  notDispatchedYet,
  planIndexFor,
  REVIEW_BODY_NOT_YET,
  reviewSetRefused,
  subAttemptsLine,
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
  assert.match(REVIEW_BODY_NOT_YET, /not built yet/);
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
