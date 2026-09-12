import { test } from 'node:test';
import assert from 'node:assert/strict';
import type { ConstructionSessionState } from '../contracts/types.ts';
import { constructionStartedFrom, type SessionProbe } from './constructionStarted.ts';

const LIVE: ConstructionSessionState = {
  projectId: 'p',
  activityId: 'C-x',
  stage: 'awaitingApproval',
  view: { projectId: 'p', activityId: 'C-x', stage: 7 },
};

const absent: SessionProbe = { data: null, isError: false };
const pending: SessionProbe = { data: undefined, isError: false };
const failed: SessionProbe = { data: undefined, isError: true };
const live: SessionProbe = { data: LIVE, isError: false };

void test('every probe answering "no session" is notStarted — the only Begin', () => {
  assert.equal(constructionStartedFrom([absent, absent, absent]), 'notStarted');
});

void test('one real session is started, even while other probes are still in flight', () => {
  assert.equal(constructionStartedFrom([absent, pending, live]), 'started');
});

void test('an unanswered probe is loading — the label must not commit to Begin before it lands', () => {
  assert.equal(constructionStartedFrom([absent, pending]), 'loading');
});

void test('a failed probe with no session found anywhere is unknown, never notStarted', () => {
  assert.equal(constructionStartedFrom([absent, failed]), 'unknown');
});

void test('no activities to probe is notStarted (nothing has a session)', () => {
  assert.equal(constructionStartedFrom([]), 'notStarted');
});
