/// <reference types="node" />
/**
 * Unit tests for the session-state poll cadence (src/hooks/sessionPolling.ts) —
 * mirrors its decision table. Two QA incidents pinned here:
 *
 *  - 2026-07-15 (gtdapp:1): a no-session 404 must POLL GENTLY so an auto-started
 *    session (the phase workflow starts the next step's co-author session after a
 *    gate approve) is discovered, instead of being cached forever behind a dead
 *    "Request draft" card the founder clicks into a running session.
 *  - 2026-07-16 (F-QA2-28): a transient NON-404 error must NEVER stop the poll.
 *    The query is its own single refresh authority (staleTime Infinity, no focus
 *    refetch), so one no-poll decision is permanent — a single failed fetch froze
 *    a stale DRAFTING… view forever while the server had already failed the draft.
 *    Errors now poll DEGRADED (5s) and the view self-heals.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { ApiError } from '../contracts/errors.ts';
import {
  DEGRADED_POLL_INTERVAL_MS,
  NO_SESSION_POLL_INTERVAL_MS,
  POLL_INTERVAL_MS,
  isNoSessionError,
  sessionPollIntervalMs,
} from './sessionPolling.ts';

void test('live stages keep the 2s poll', () => {
  for (const stage of ['drafting', 'redrafting', 'awaitingReview'] as const) {
    assert.equal(sessionPollIntervalMs(stage, null), POLL_INTERVAL_MS, stage);
  }
});

void test('terminal stages stop polling', () => {
  for (const stage of ['committed', 'withdrawn', 'refused', 'draftFailed'] as const) {
    assert.equal(sessionPollIntervalMs(stage, null), false, stage);
  }
});

void test('INCIDENT 2026-07-15: a no-session 404 polls gently instead of stopping', () => {
  const noSession = new ApiError(404, 'not_found', 'no active design session');
  assert.equal(sessionPollIntervalMs(undefined, noSession), NO_SESSION_POLL_INTERVAL_MS);
});

void test('F-QA2-28: a transient error with a live last-known stage polls DEGRADED, never stops', () => {
  // react-query keeps the last good data on a failed refetch (verified against
  // v5.100.14: the 'error' reducer leaves state.data untouched), so stage stays
  // defined while state.error names the fault.
  for (const stage of ['drafting', 'redrafting', 'awaitingReview'] as const) {
    assert.equal(
      sessionPollIntervalMs(stage, new ApiError(500, 'internal', 'blip')),
      DEGRADED_POLL_INTERVAL_MS,
      stage
    );
    assert.equal(
      sessionPollIntervalMs(stage, new TypeError('Failed to fetch')),
      DEGRADED_POLL_INTERVAL_MS,
      `${stage} + network fault`
    );
  }
});

void test('F-QA2-28: a transient error with NO data polls DEGRADED (bootstrap retry)', () => {
  // The first fetch of a fresh query (step switch / page open) hitting a restarting
  // server used to latch the query dead forever — no data, non-404 error, no poll,
  // and nothing else ever refetches. It must keep probing so the view ever loads.
  assert.equal(
    sessionPollIntervalMs(undefined, new ApiError(500, 'internal', 'boom')),
    DEGRADED_POLL_INTERVAL_MS
  );
  assert.equal(
    sessionPollIntervalMs(undefined, new Error('network down')),
    DEGRADED_POLL_INTERVAL_MS
  );
});

void test('an error with a TERMINAL last-known stage still stops polling', () => {
  for (const stage of ['committed', 'withdrawn', 'refused', 'draftFailed'] as const) {
    assert.equal(sessionPollIntervalMs(stage, new ApiError(500, 'internal', 'boom')), false, stage);
    assert.equal(
      sessionPollIntervalMs(stage, new ApiError(404, 'not_found', 'gone')),
      false,
      `${stage} + 404`
    );
  }
});

void test('a 404 with a live last-known stage re-probes at the no-session cadence', () => {
  // The server says that session is GONE (restart lost it / it was withdrawn out of
  // band): the stale live stage is a lie — probe for its successor at the gentle 4s.
  const gone = new ApiError(404, 'not_found', 'no active design session');
  assert.equal(sessionPollIntervalMs('drafting', gone), NO_SESSION_POLL_INTERVAL_MS);
});

void test('no data + no error (initial mount) does not add an interval', () => {
  assert.equal(sessionPollIntervalMs(undefined, null), false);
  assert.equal(sessionPollIntervalMs(undefined, undefined), false);
});

void test('isNoSessionError recognizes only the 404 ApiError', () => {
  assert.equal(isNoSessionError(new ApiError(404, 'not_found', 'x')), true);
  assert.equal(isNoSessionError(new ApiError(409, 'failed_precondition', 'x')), false);
  assert.equal(isNoSessionError(new Error('404')), false);
  assert.equal(isNoSessionError(null), false);
});
