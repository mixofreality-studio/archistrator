/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import type { ConstructionSessionState } from '../contracts/types.ts';
import { erroredProbesFor, sessionsByActivity } from './constructionSessions.ts';

function session(activityId: string): ConstructionSessionState {
  return {
    projectId: 'p',
    activityId,
    stage: 'awaitingApproval',
    view: { projectId: 'p', activityId, stage: 4 },
  };
}

void test('pairs each probe with its activity id, by position', () => {
  const a = session('A');
  const c = session('C');
  const got = sessionsByActivity(['A', 'B', 'C'], [{ data: a }, { data: null }, { data: c }]);
  assert.equal(got['A'], a);
  assert.equal(got['C'], c);
});

void test('keeps an established absence (null) apart from "not answered yet"', () => {
  const got = sessionsByActivity(['A', 'B'], [{ data: null }, {}]);
  assert.equal(got['A'], null);
  assert.equal('A' in got, true);
  // Unanswered is ABSENT — a caller must not read it as "no session".
  assert.equal('B' in got, false);
});

void test('a probe that failed without answering is errored; pending and answered ones are not', () => {
  const got = erroredProbesFor(
    ['A', 'B', 'C', 'D'],
    [
      { status: 'error' }, // failed, never answered
      { status: 'pending' }, // first fetch in flight
      { status: 'error', data: session('C') }, // answered once, errored since: keeps its answer
      { status: 'success', data: null }, // established absence
    ]
  );
  assert.deepEqual(got, ['A']);
});

void test('a short results list never invents entries', () => {
  assert.deepEqual(sessionsByActivity(['A', 'B'], [{ data: null }]), { A: null });
  assert.deepEqual(sessionsByActivity([], []), {});
});
