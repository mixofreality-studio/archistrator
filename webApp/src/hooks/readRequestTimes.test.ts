/// <reference types="node" />
/**
 * The request-time store (readRequestTimes.ts), against a real QueryClient: the
 * read a query shows carries the time its fetch BEGAN, never the time it arrived,
 * and a fetch that was cancelled never lends its time to a read (orchestrator, from
 * the tasks-lens review: evidence counts a read from its request).
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { QueryClient } from '@tanstack/react-query';
import { readRequestedAt } from './readRequestTimes.ts';
import { orderedNow } from '../utilities/orderedNow.ts';

function deferred<T>(): { promise: Promise<T>; resolve: (value: T) => void } {
  let resolve: (value: T) => void = () => undefined;
  const promise = new Promise<T>((r) => {
    resolve = r;
  });
  return { promise, resolve };
}

const sleep = (ms: number): Promise<void> =>
  new Promise((r) => {
    setTimeout(r, ms);
  });

void test('the shown read carries when its fetch BEGAN, not when it arrived', async () => {
  const client = new QueryClient();
  const key = ['project', 'p1'];
  assert.equal(readRequestedAt(client, key), 0, 'nothing read yet');
  const answer = deferred<string>();
  const before = Date.now();
  const fetched = client.fetchQuery({ queryKey: key, queryFn: () => answer.promise });
  const started = Date.now();
  await sleep(30);
  answer.resolve('v1');
  await fetched;
  const requestedAt = readRequestedAt(client, key);
  // The ordered clock may sit a microsecond past the wall clock's millisecond.
  assert.ok(
    requestedAt >= before && requestedAt < started + 1,
    `request time ${String(requestedAt)} is the fetch start`
  );
  const arrivedAt = client.getQueryState(key)?.dataUpdatedAt ?? 0;
  assert.ok(arrivedAt - requestedAt >= 25, 'arrival is later, and is not what is recorded');
});

void test('a cancelled fetch never lends its start to a read; the one that lands does', async () => {
  const client = new QueryClient();
  const key = ['project', 'p2'];
  readRequestedAt(client, key);
  const first = deferred<string>();
  void client.fetchQuery({ queryKey: key, queryFn: () => first.promise }).catch(() => undefined);
  await sleep(20);
  await client.cancelQueries({ queryKey: key });
  assert.equal(readRequestedAt(client, key), 0, 'the cancelled fetch shows no read');
  const secondStart = Date.now();
  await client.fetchQuery({ queryKey: key, queryFn: () => Promise.resolve('v2') });
  assert.ok(readRequestedAt(client, key) >= secondStart, 'the read shown is the second fetch');
  // The cancelled fetch's late answer changes nothing.
  first.resolve('stale');
  await sleep(5);
  assert.ok(readRequestedAt(client, key) >= secondStart);
});

void test('a newer fetch of the same content still moves the request time on', async () => {
  const client = new QueryClient();
  const key = ['project', 'p3'];
  readRequestedAt(client, key);
  await client.fetchQuery({ queryKey: key, queryFn: () => Promise.resolve({ same: true }) });
  const firstAt = readRequestedAt(client, key);
  await sleep(10);
  const secondStart = Date.now();
  await client.fetchQuery({
    queryKey: key,
    queryFn: () => Promise.resolve({ same: true }),
    staleTime: 0,
  });
  const secondAt = readRequestedAt(client, key);
  assert.ok(secondAt >= secondStart && secondAt > firstAt);
});

void test('no request, no time: hand-written data, a read from before the store, a removed query', async () => {
  const client = new QueryClient();
  const early = ['project', 'early'];
  // Cached before the store subscribed: never seen starting.
  await client.fetchQuery({ queryKey: early, queryFn: () => Promise.resolve('x') });
  assert.equal(readRequestedAt(client, early), 0);

  const manual = ['project', 'manual'];
  client.setQueryData(manual, 'by hand');
  assert.equal(readRequestedAt(client, manual), 0, 'setQueryData has no request');

  const gone = ['project', 'gone'];
  await client.fetchQuery({ queryKey: gone, queryFn: () => Promise.resolve('y') });
  assert.ok(readRequestedAt(client, gone) > 0);
  client.removeQueries({ queryKey: gone });
  assert.equal(readRequestedAt(client, gone), 0, 'a removed query forgets its time');
});

// Fix I: the Begin hold's records and every read's request share ONE ordered
// clock, so a fetch started right after a record is strictly later than it —
// the refresh a success requests can count as its pickup even in the same ms.
void test('a fetch started right after a record is strictly later than it, even in the same millisecond', async () => {
  const client = new QueryClient();
  const key = ['project', 'ordered'];
  readRequestedAt(client, key);
  for (let i = 0; i < 50; i++) {
    const recordedAt = orderedNow();
    await client.fetchQuery({ queryKey: key, queryFn: () => Promise.resolve(i), staleTime: 0 });
    const requestedAt = readRequestedAt(client, key);
    assert.ok(
      requestedAt > recordedAt,
      `round ${String(i)}: the read requested after the record (${String(requestedAt)}) is not after it (${String(recordedAt)})`
    );
  }
});

void test('each QueryClient keeps its own times', async () => {
  const a = new QueryClient();
  const b = new QueryClient();
  const key = ['project', 'shared-key'];
  readRequestedAt(a, key);
  readRequestedAt(b, key);
  await a.fetchQuery({ queryKey: key, queryFn: () => Promise.resolve('a') });
  assert.ok(readRequestedAt(a, key) > 0);
  assert.equal(readRequestedAt(b, key), 0);
});
