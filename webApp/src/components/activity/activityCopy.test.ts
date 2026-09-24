/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  ACTIVITY_NOT_IN_PLAN,
  artifactUnavailable,
  CONSTRUCTION_THREAD_READ_ONLY,
  eyebrowFor,
  historyBanner,
  HISTORY_ARTIFACT_CAPTION,
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
});
