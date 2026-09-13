/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import {
  OCCURRENCE_LIMIT,
  observeGate,
  withOccurrence,
  type GateOccurrence,
} from './gateOccurrences.ts';

void test('each entry into awaitingApproval is a new occurrence; staying there is not', () => {
  let o: GateOccurrence | undefined;
  o = observeGate(o, 'awaitingApproval', 1, 0);
  assert.equal(o.epoch, 1);
  o = observeGate(o, 'awaitingApproval', 2, 1);
  assert.equal(o.epoch, 1);
  o = observeGate(o, 'pipelineRunning', 3, 2); // a send-back's redraft
  assert.equal(o.epoch, 1);
  assert.equal(o.leftAt, 3);
  o = observeGate(o, 'pipelineRunning', 4, 3);
  assert.equal(o.leftAt, 3); // first seen away, not latest
  o = observeGate(o, 'awaitingApproval', 5, 4); // round 2
  assert.equal(o.epoch, 2);
  assert.equal(o.leftAt, undefined);
});

void test('a read no newer than the last one folded changes nothing', () => {
  const o = observeGate(undefined, 'awaitingApproval', 10, 9);
  assert.equal(observeGate(o, 'pipelineRunning', 10, 10), o);
  assert.equal(observeGate(o, 'pipelineRunning', 9, 10), o);
});

void test('a session that no longer exists has left the gate', () => {
  const o = observeGate(observeGate(undefined, 'awaitingApproval', 1, 0), null, 2, 1);
  assert.equal(o.stage, null);
  assert.equal(o.leftAt, 2);
  assert.equal(o.epoch, 1);
});

void test('an occurrence carries when its latest read was REQUESTED, not only when it arrived', () => {
  // Asked for at 100, arrived at 900: the evidence rule counts the 100.
  const o = observeGate(undefined, 'awaitingApproval', 900, 100);
  assert.equal(o.seenAt, 900);
  assert.equal(o.requestedAt, 100);
  assert.equal(observeGate(o, 'awaitingApproval', 1_000, 950).requestedAt, 950);
});

void test('the store keeps only the most recently observed activities', () => {
  const occ = (n: number): GateOccurrence => observeGate(undefined, 'pipelineRunning', n, n);
  let store: ReadonlyMap<string, GateOccurrence> = new Map();
  for (const k of ['a', 'b', 'c']) store = withOccurrence(store, k, occ(1), 2);
  assert.deepEqual([...store.keys()], ['b', 'c']);
  // Observing an activity again moves it to the newest end, so it is not dropped.
  store = withOccurrence(store, 'b', occ(2), 2);
  store = withOccurrence(store, 'd', occ(3), 2);
  assert.deepEqual([...store.keys()], ['b', 'd']);
  // The input is never mutated.
  const before = new Map([['x', occ(1)]]);
  withOccurrence(before, 'y', occ(2), 1);
  assert.deepEqual([...before.keys()], ['x']);
  // The default limit applies when none is given.
  let big: ReadonlyMap<string, GateOccurrence> = new Map();
  for (let i = 0; i < OCCURRENCE_LIMIT + 10; i += 1)
    big = withOccurrence(big, `k${String(i)}`, occ(i));
  assert.equal(big.size, OCCURRENCE_LIMIT);
  assert.equal(big.has('k0'), false);
  assert.equal(big.has(`k${String(OCCURRENCE_LIMIT + 9)}`), true);
});
