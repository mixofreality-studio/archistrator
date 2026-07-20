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
 *  - 2026-07-16 (F-QA2-50): the FAILURE gates (refused / draftFailed) must keep
 *    watching. Retry resumes the SAME session server-side (failed → redrafting →
 *    awaitingReview within seconds, no new CI job); with the failed stage treated
 *    as a stop state, the retry mutation's SINGLE invalidation refetch was the
 *    only escape, and losing that race froze a stale generating view for 4+
 *    minutes until a hard reload while the server sat at the review gate.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { ApiError } from '../contracts/errors.ts';
import {
  DEGRADED_POLL_INTERVAL_MS,
  GATE_POLL_INTERVAL_MS,
  NO_SESSION_POLL_INTERVAL_MS,
  POLL_INTERVAL_MS,
  isNoSessionError,
  isSessionAbsent,
  sessionPollIntervalMs,
} from './sessionPolling.ts';

void test('live stages keep the 2s poll', () => {
  for (const stage of ['drafting', 'redrafting'] as const) {
    assert.equal(sessionPollIntervalMs(stage, null), POLL_INTERVAL_MS, stage);
  }
});

void test('F-QA2-48: the review gate is NOT terminal — it watches at the slow gate cadence', () => {
  // Verified live: with the gate treated as a stop state, a server-side stage
  // change (another tab's decision, or a Send back whose 503 response lost a
  // DELIVERED signal) never rendered until a hard reload.
  assert.equal(sessionPollIntervalMs('awaitingReview', null), GATE_POLL_INTERVAL_MS);
});

void test('REST stages (committed / withdrawn) stop polling', () => {
  for (const stage of ['committed', 'withdrawn'] as const) {
    assert.equal(sessionPollIntervalMs(stage, null), false, stage);
  }
});

void test('F-QA2-50: the failure gates (refused / draftFailed) watch at the slow gate cadence', () => {
  // Verified live: with draftFailed treated as a stop state, the Retry click's
  // single invalidation refetch raced the server's in-place resume and the SPA
  // froze on a stale generating view until a hard reload.
  for (const stage of ['refused', 'draftFailed'] as const) {
    assert.equal(sessionPollIntervalMs(stage, null), GATE_POLL_INTERVAL_MS, stage);
  }
});

void test('F-QA2-50: failed → retry → server-says-awaitingReview renders the gate without a reload', () => {
  // The pinned regression as a cadence walk: at EVERY evaluation the query can
  // reach between the Retry click and the review gate — the parked failed stage,
  // the brief server-side redrafting transient, and the gate itself — the
  // interval decision must be a live number (never false). One false is
  // permanent (staleTime Infinity, no focus refetch), which is exactly how the
  // stale REDRAFTING… view survived 4+ minutes of server-side awaitingReview.
  const walk = ['draftFailed', 'redrafting', 'awaitingReview'] as const;
  for (const stage of walk) {
    const interval = sessionPollIntervalMs(stage, null);
    assert.notEqual(interval, false, `${stage} must keep a live interval`);
    assert.equal(typeof interval, 'number', stage);
  }
  // Even when the invalidation refetch itself faults transiently mid-transition,
  // the failed stage keeps probing (degraded), so the flip still renders.
  assert.equal(
    sessionPollIntervalMs('draftFailed', new ApiError(500, 'internal', 'blip')),
    DEGRADED_POLL_INTERVAL_MS
  );
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

void test('an error with a REST last-known stage still stops polling', () => {
  for (const stage of ['committed', 'withdrawn'] as const) {
    assert.equal(sessionPollIntervalMs(stage, new ApiError(500, 'internal', 'boom')), false, stage);
    assert.equal(
      sessionPollIntervalMs(stage, new ApiError(404, 'not_found', 'gone')),
      false,
      `${stage} + 404`
    );
  }
});

void test('F-QA2-50: an error at a failure gate degrades / re-probes, never stops', () => {
  // The old rule ran the terminal check BEFORE the error branch, so a transient
  // fault landing while the last-known stage was draftFailed latched the query
  // dead. The failure gates now fall through to the error cadences.
  for (const stage of ['refused', 'draftFailed'] as const) {
    assert.equal(
      sessionPollIntervalMs(stage, new ApiError(500, 'internal', 'boom')),
      DEGRADED_POLL_INTERVAL_MS,
      stage
    );
    assert.equal(
      sessionPollIntervalMs(stage, new ApiError(404, 'not_found', 'gone')),
      NO_SESSION_POLL_INTERVAL_MS,
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

void test('QA 2026-07-19: a 404 with NO cached session is authoritative absence', () => {
  // The probe never found a session — rendering "No draft yet / Request draft"
  // discards nothing.
  const gone = new ApiError(404, 'not_found', 'no active design session');
  assert.equal(isSessionAbsent(false, gone), true);
});

void test('QA 2026-07-19: a 404 arriving while a session view is cached must NOT read as absence', () => {
  // Verified live (gtdapp kind=4): the local stack's Temporal was replaced by a
  // foreign dev server on the same port; every poll 404'd and the SPA reset the
  // founder's use-case wizard to the beginning. With data cached, a 404 refetch
  // keeps the working view and the 4s no-session probe keeps watching.
  const gone = new ApiError(404, 'not_found', 'no active design session');
  assert.equal(isSessionAbsent(true, gone), false);
});

void test('isSessionAbsent: non-404 errors and healthy fetches never read as absence', () => {
  assert.equal(isSessionAbsent(false, new ApiError(500, 'internal', 'boom')), false);
  assert.equal(isSessionAbsent(false, null), false);
  assert.equal(isSessionAbsent(true, null), false);
});
