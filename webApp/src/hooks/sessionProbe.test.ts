/// <reference types="node" />
/**
 * Regression tests for the session-absence model (QA 2026-07-19 REOPENED —
 * gtdapp kind=4, the walkthrough-reset recording).
 *
 * ROOT CAUSE PINNED HERE: react-query v5's fetchState() resets a query to
 * `{ status: 'pending', error: null }` whenever a refetch starts while
 * `data === undefined`. A "no session" probe that models the 404 as an ERROR
 * therefore never holds data, so EVERY poll tick flips the query through
 * pending — the containers momentarily see sessionMissing=false/loading=true,
 * swap the committed-artifact panel for a skeleton, and remount it ~ms later,
 * wiping all local state (use-case walkthrough step, carousel index, scroll).
 * The founder saw his walkthrough snap back to step 1 on every 4s probe.
 *
 * THE FIX: absence is a VALUE, not an error. sessionProbeQueryFn converts the
 * no-session 404 into `null` (or the cached last-good view — fd14c80's
 * keep-last-good rule, now enforced inside the probe). A query that holds data
 * (null IS data) never re-enters pending, so the poll can never unmount the
 * view. These tests drive a real QueryClient/QueryObserver — the exact state
 * machine the SPA rides — with no React involved.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { QueryClient, QueryObserver } from '@tanstack/react-query';
import { ApiError } from '../contracts/errors.ts';
import { sessionProbeQueryFn } from './sessionPolling.ts';

interface View {
  stage: string;
  step: number;
}

function noSession(): ApiError {
  return new ApiError(404, 'not_found', 'no active design session');
}

function makeClient(): QueryClient {
  return new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
}

/** Resolves once the observer's current fetch settles (fetchStatus idle). */
function waitIdle<T>(observer: QueryObserver<T>): Promise<void> {
  return new Promise((resolve) => {
    if (observer.getCurrentResult().fetchStatus === 'idle') {
      resolve();
      return;
    }
    const unsub = observer.subscribe((r) => {
      if (r.fetchStatus === 'idle') {
        unsub();
        resolve();
      }
    });
  });
}

void test('MECHANISM (react-query v5): refetching a data-less errored query re-enters pending', async () => {
  // This is the library behavior the old code mis-assumed away ("a 404 refetch
  // keeps the query in error state — never back to pending"): with data
  // undefined, fetchState() resets error to null and status to 'pending' the
  // moment a refetch starts. Pinned so nobody reverts absence back to an error
  // model believing the old comment.
  const client = makeClient();
  const observer = new QueryObserver<View, Error>(client, {
    queryKey: ['mechanism'],
    queryFn: async (): Promise<View> => Promise.reject(noSession()),
    retry: false,
    staleTime: Infinity,
  });
  const unsubHold = observer.subscribe(() => undefined);
  await waitIdle(observer);
  assert.equal(observer.getCurrentResult().status, 'error');

  const seen: string[] = [];
  const unsubRecord = observer.subscribe((r) => {
    seen.push(r.status);
  });
  await observer.refetch();
  unsubRecord();
  unsubHold();
  client.clear();
  assert.ok(
    seen.includes('pending'),
    'a data-less errored query re-enters pending on refetch — the remount trigger'
  );
});

void test('QA 2026-07-19 REOPENED: established absence NEVER re-enters pending across 404 polls', async () => {
  // The founder's recording: Core Use Cases COMMITTED (no active session — the
  // 404 is legitimate), walkthrough advanced to step 2, next 4s probe arrives,
  // walkthrough snaps back to step 1. The probe must hold `null` as DATA so a
  // refetch can never flip the query through pending / sessionMissing=false.
  const client = makeClient();
  const key = ['sessionState', 'gtdapp', 'coreUseCases'] as const;
  const queryFn = sessionProbeQueryFn<View>({
    fetch: async () => Promise.reject(noSession()),
    getCached: () => client.getQueryData<View | null>(key),
  });
  const observer = new QueryObserver<View | null, Error>(client, {
    queryKey: key,
    queryFn,
    retry: false,
    staleTime: Infinity,
  });
  const unsubHold = observer.subscribe(() => undefined);
  await waitIdle(observer);
  let r = observer.getCurrentResult();
  assert.equal(r.status, 'success', 'absence is a VALUE — the probe settles success');
  assert.equal(r.data, null, 'absence is established as null data');

  const emissions: { status: string; data: View | null | undefined }[] = [];
  const unsubRecord = observer.subscribe((res) => {
    emissions.push({ status: res.status, data: res.data });
  });
  await observer.refetch();
  await observer.refetch();
  unsubRecord();
  for (const e of emissions) {
    assert.notEqual(
      e.status,
      'pending',
      'no pending re-entry — pending unmounts the committed-artifact view and wipes its state'
    );
    assert.equal(e.data, null, 'absence stays established across polls');
  }
  r = observer.getCurrentResult();
  assert.equal(r.status, 'success');
  assert.equal(r.data, null);
  unsubHold();
  client.clear();
});

void test('fd14c80 preserved: a 404 while a session view is cached keeps the last-good view', async () => {
  // A session store genuinely wiped under a live view (2026-07-19 foreign-Temporal
  // incident) must keep rendering the last-good view; the probe keeps watching and
  // any successor session replaces it. The keep-last-good rule now lives INSIDE
  // the probe (it returns the cached view instead of surfacing the 404).
  const client = makeClient();
  const key = ['sessionState', 'gtdapp', 'mission'] as const;
  const view: View = { stage: 'awaitingReview', step: 5 };
  let calls = 0;
  const queryFn = sessionProbeQueryFn<View>({
    fetch: async () => {
      calls += 1;
      return calls === 1 ? Promise.resolve(view) : Promise.reject(noSession());
    },
    getCached: () => client.getQueryData<View | null>(key),
  });
  const observer = new QueryObserver<View | null, Error>(client, {
    queryKey: key,
    queryFn,
    retry: false,
    staleTime: Infinity,
  });
  const unsubHold = observer.subscribe(() => undefined);
  await waitIdle(observer);
  assert.deepEqual(observer.getCurrentResult().data, view);

  const emissions: string[] = [];
  const unsubRecord = observer.subscribe((res) => {
    emissions.push(res.status);
  });
  await observer.refetch();
  unsubRecord();
  const r = observer.getCurrentResult();
  assert.equal(r.status, 'success');
  assert.deepEqual(r.data, view, 'the cached view survives the 404 — nothing to reset into');
  assert.ok(!emissions.includes('pending'), 'no pending re-entry with a cached view either');
  unsubHold();
  client.clear();
});

void test('a successor session replaces established absence', async () => {
  // The 2026-07-15 incident rule still holds: an approve auto-starts the next
  // step's session server-side, and the gentle probe must discover it.
  const client = makeClient();
  const key = ['sessionState', 'gtdapp', 'glossary'] as const;
  const view: View = { stage: 'drafting', step: 1 };
  let calls = 0;
  const queryFn = sessionProbeQueryFn<View>({
    fetch: async () => {
      calls += 1;
      return calls === 1 ? Promise.reject(noSession()) : Promise.resolve(view);
    },
    getCached: () => client.getQueryData<View | null>(key),
  });
  const observer = new QueryObserver<View | null, Error>(client, {
    queryKey: key,
    queryFn,
    retry: false,
    staleTime: Infinity,
  });
  const unsubHold = observer.subscribe(() => undefined);
  await waitIdle(observer);
  assert.equal(observer.getCurrentResult().data, null);
  await observer.refetch();
  assert.deepEqual(observer.getCurrentResult().data, view);
  unsubHold();
  client.clear();
});

void test('non-404 faults still surface as errors (degraded-poll path preserved)', async () => {
  // F-QA2-28's transient-fault handling is untouched: only the deterministic
  // no-session 404 becomes a value; a 5xx / network fault stays an error so the
  // degraded 5s cadence and error surfaces keep working.
  const client = makeClient();
  const key = ['sessionState', 'gtdapp', 'volatilities'] as const;
  const queryFn = sessionProbeQueryFn<View>({
    fetch: async () => Promise.reject(new ApiError(500, 'internal', 'boom')),
    getCached: () => client.getQueryData<View | null>(key),
  });
  const observer = new QueryObserver<View | null, Error>(client, {
    queryKey: key,
    queryFn,
    retry: false,
    staleTime: Infinity,
  });
  const unsubHold = observer.subscribe(() => undefined);
  await waitIdle(observer);
  const r = observer.getCurrentResult();
  assert.equal(r.status, 'error');
  assert.ok(r.error instanceof ApiError && r.error.status === 500);
  unsubHold();
  client.clear();
});
