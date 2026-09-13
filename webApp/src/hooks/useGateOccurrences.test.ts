/**
 * The occurrence store's wiring to a REAL QueryClient (tasks round 2 minor: count a
 * read from when it was requested). gateOccurrences.test.ts pins the fold; this
 * pins that the store notes each session fetch's START from the query cache, so an
 * occurrence's `requestedAt` is the request, not the arrival — the time the
 * decision's evidence rule (decisionFlow.ts) reads.
 */
/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { QueryClient } from '@tanstack/react-query';

import { gateOccurrenceStoreFor } from './useGateOccurrences.ts';
import { occurrenceKey } from './gateOccurrences.ts';
import { readRequestedAt } from './readRequestTimes.ts';

const sleep = (ms: number): Promise<void> =>
  new Promise((r) => {
    setTimeout(r, ms);
  });

function session(activityId: string, stage: string): Record<string, unknown> {
  return { projectId: 'p', activityId, stage, view: {} };
}

void test('the store notes when a session read was REQUESTED, not only when it arrived', async () => {
  const client = new QueryClient();
  const store = gateOccurrenceStoreFor(client);
  let answer: (v: unknown) => void = () => undefined;
  const asked = Date.now();
  const read = client.fetchQuery({
    queryKey: ['constructionSession', 'p', 'A'],
    queryFn: () =>
      new Promise((r) => {
        answer = r;
      }),
  });
  // The read is in flight for a while before it answers.
  await sleep(40);
  const answered = Date.now();
  answer(session('A', 'awaitingApproval'));
  await read;
  const occ = store.snapshot.get(occurrenceKey('p', 'A'));
  assert.ok(occ);
  assert.equal(occ.stage, 'awaitingApproval');
  assert.ok(
    occ.requestedAt >= asked && occ.requestedAt < answered,
    `requestedAt ${String(occ.requestedAt)} is not the request (${String(asked)}…${String(answered)})`
  );
  assert.ok(occ.seenAt >= answered);
  client.clear();
});

void test('a read already cached before the store subscribed has no request time: 0, never newer', () => {
  const client = new QueryClient();
  client.setQueryData(['constructionSession', 'p', 'B'], session('B', 'pipelineRunning'));
  const store = gateOccurrenceStoreFor(client);
  assert.equal(store.snapshot.get(occurrenceKey('p', 'B'))?.requestedAt, 0);
  client.clear();
});

// Tasks-lens merge round: ONE request-time store. The gate occurrences take a read's
// request time from readRequestTimes, the store the Begin hold reads, so the two
// cannot disagree about when the same session read was asked for.
void test('the occurrence and the Begin hold read the SAME request time for one session read', async () => {
  const client = new QueryClient();
  const store = gateOccurrenceStoreFor(client);
  const key = ['constructionSession', 'p', 'C'];
  let answer: (v: unknown) => void = () => undefined;
  const read = client.fetchQuery({
    queryKey: key,
    queryFn: () =>
      new Promise((r) => {
        answer = r;
      }),
  });
  await sleep(30);
  answer(session('C', 'awaitingApproval'));
  await read;
  const occ = store.snapshot.get(occurrenceKey('p', 'C'));
  assert.ok(occ);
  assert.ok(occ.requestedAt > 0);
  assert.equal(occ.requestedAt, readRequestedAt(client, key));
  // Hand-written data has no request, in both readers. (A later millisecond: the
  // fold ignores a read no newer than the last.)
  await sleep(5);
  client.setQueryData(key, session('C', 'pipelineRunning'));
  assert.equal(store.snapshot.get(occurrenceKey('p', 'C'))?.stage, 'pipelineRunning');
  assert.equal(store.snapshot.get(occurrenceKey('p', 'C'))?.requestedAt, 0);
  assert.equal(readRequestedAt(client, key), 0);
  client.clear();
});

void test('a cancelled session fetch lends its start to no occurrence', async () => {
  const client = new QueryClient();
  const store = gateOccurrenceStoreFor(client);
  const key = ['constructionSession', 'p', 'D'];
  const first = client.fetchQuery({ queryKey: key, queryFn: () => new Promise(() => undefined) });
  first.catch(() => undefined);
  await sleep(20);
  const cancelledStart = Date.now() - 20;
  await client.cancelQueries({ queryKey: key });
  await sleep(20);
  const secondStart = Date.now();
  await client.fetchQuery({ queryKey: key, queryFn: () => session('D', 'awaitingApproval') });
  const occ = store.snapshot.get(occurrenceKey('p', 'D'));
  assert.ok(occ);
  assert.ok(
    occ.requestedAt >= secondStart,
    `requestedAt ${String(occ.requestedAt)} came from the cancelled fetch (~${String(cancelledStart)})`
  );
  client.clear();
});

// The one store's time, whichever store a client made first: a session read cached
// after the request-time store subscribed, but before the occurrence store did, keeps
// the request time that store paired to it — never 0 for a read it saw requested.
void test("an occurrence store made after a read takes that read's request time from the one store", async () => {
  const client = new QueryClient();
  const key = ['constructionSession', 'p', 'E'];
  readRequestedAt(client, key); // the request-time store subscribes first
  await client.fetchQuery({ queryKey: key, queryFn: () => session('E', 'awaitingApproval') });
  const requested = readRequestedAt(client, key);
  assert.ok(requested > 0);
  const store = gateOccurrenceStoreFor(client);
  assert.equal(store.snapshot.get(occurrenceKey('p', 'E'))?.requestedAt, requested);
  client.clear();
});
