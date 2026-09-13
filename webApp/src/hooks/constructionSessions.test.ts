/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import type { ConstructionSessionState } from '../contracts/types.ts';
import {
  ERRORED_PROBE_BACKOFF_MAX_MS,
  ERRORED_PROBE_BACKOFF_MS,
  erroredProbeBackoffMs,
  erroredProbesFor,
  retryingProbesFor,
  sessionsByActivity,
} from './constructionSessions.ts';

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
      { errorUpdateCount: 1, fetchStatus: 'idle' }, // failed, never answered
      { errorUpdateCount: 0, fetchStatus: 'fetching' }, // first fetch in flight
      { errorUpdateCount: 2, data: session('C') }, // answered once, errored since: keeps its answer
      { errorUpdateCount: 0, data: null }, // established absence
    ]
  );
  assert.deepEqual(got, ['A']);
});

void test('a failed probe stays failed while it is asked again (designer re-check B1)', () => {
  // TanStack reports a data-less query as `pending` again the moment it refetches;
  // only the error count remembers that it failed. It must not blink to "Checking".
  const refetching = { errorUpdateCount: 1, fetchStatus: 'fetching' as const };
  assert.deepEqual(erroredProbesFor(['A'], [refetching]), ['A']);
  // …and it is the one the lens says is being retried.
  assert.deepEqual(
    retryingProbesFor(
      ['A', 'B', 'C'],
      [
        refetching,
        { errorUpdateCount: 0, fetchStatus: 'fetching' }, // first fetch: pending, not a retry
        { errorUpdateCount: 2, fetchStatus: 'idle' }, // failed, not being asked right now
      ]
    ),
    ['A']
  );
  // Once it answers it is neither.
  const answered = { errorUpdateCount: 1, fetchStatus: 'fetching' as const, data: null };
  assert.deepEqual(erroredProbesFor(['A'], [answered]), []);
  assert.deepEqual(retryingProbesFor(['A'], [answered]), []);
});

void test('a probe that has only ever failed backs off: 10s, doubling, at most 60s (designer re-check B1)', () => {
  assert.equal(ERRORED_PROBE_BACKOFF_MS, 10_000);
  assert.equal(erroredProbeBackoffMs(1), 10_000);
  assert.equal(erroredProbeBackoffMs(2), 20_000);
  assert.equal(erroredProbeBackoffMs(3), 40_000);
  assert.equal(erroredProbeBackoffMs(4), ERRORED_PROBE_BACKOFF_MAX_MS);
  assert.equal(erroredProbeBackoffMs(40), ERRORED_PROBE_BACKOFF_MAX_MS);
  assert.equal(ERRORED_PROBE_BACKOFF_MAX_MS, 60_000);
  // Never faster than the first step, whatever the count says.
  assert.equal(erroredProbeBackoffMs(0), 10_000);
});

void test('a short results list never invents entries', () => {
  assert.deepEqual(sessionsByActivity(['A', 'B'], [{ data: null }]), { A: null });
  assert.deepEqual(sessionsByActivity([], []), {});
});
