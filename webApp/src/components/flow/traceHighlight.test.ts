/// <reference types="node" />
/**
 * Unit tests for the pure call-chain -> activity-diagram trace projection
 * (src/components/flow/traceHighlight.ts), which drives the you-are-here
 * activity pane rendered beside the Architecture dynamic lens.
 *
 * Fixture: a three-step activity (validate → charge → notify) whose middle step
 * authors two calls, mirroring the step-keyed dynamic-view model where one
 * activity node owns an ordered call fragment.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { traceHighlight } from './traceHighlight.ts';

/** One call at global position `seq`, owned by activity node `stepNodeId`. */
function call(seq: number, stepNodeId: string): { seq: number; stepNodeId: string } {
  return { seq, stepNodeId };
}

const CHAIN = [
  call(1, 'n-validate'),
  call(2, 'n-charge'),
  call(3, 'n-charge'),
  call(4, 'n-notify'),
];

const ACTIVITY_EDGES = [
  { from: 'n-start', to: 'n-validate' },
  { from: 'n-validate', to: 'n-charge' },
  { from: 'n-charge', to: 'n-notify' },
  { from: 'n-notify', to: 'n-end' },
];

void test('the current call names the you-are-here step', () => {
  const hl = traceHighlight(CHAIN, 2, ACTIVITY_EDGES);
  assert.equal(hl?.current, 'n-charge');
});

void test('visited nodes are the steps of every call at or before the current one', () => {
  const first = traceHighlight(CHAIN, 1, ACTIVITY_EDGES);
  assert.deepEqual([...(first?.visitedNodes ?? [])], ['n-validate']);

  const mid = traceHighlight(CHAIN, 3, ACTIVITY_EDGES);
  assert.deepEqual([...(mid?.visitedNodes ?? [])], ['n-validate', 'n-charge']);

  const last = traceHighlight(CHAIN, 4, ACTIVITY_EDGES);
  assert.deepEqual([...(last?.visitedNodes ?? [])], ['n-validate', 'n-charge', 'n-notify']);
});

void test('both calls of one step hold the same you-are-here node', () => {
  assert.equal(traceHighlight(CHAIN, 2, ACTIVITY_EDGES)?.current, 'n-charge');
  assert.equal(traceHighlight(CHAIN, 3, ACTIVITY_EDGES)?.current, 'n-charge');
});

void test('an activity edge is walked only when BOTH endpoints are visited', () => {
  // n-start is not a realized step, so the start → validate edge never lights up;
  // validate → charge does, once the walk has reached charge.
  const first = traceHighlight(CHAIN, 1, ACTIVITY_EDGES);
  assert.deepEqual([...(first?.visitedEdges ?? [])], []);

  const mid = traceHighlight(CHAIN, 2, ACTIVITY_EDGES);
  assert.deepEqual([...(mid?.visitedEdges ?? [])], ['n-validate-n-charge']);

  const last = traceHighlight(CHAIN, 4, ACTIVITY_EDGES);
  assert.deepEqual([...(last?.visitedEdges ?? [])], ['n-validate-n-charge', 'n-charge-n-notify']);
});

void test('no current call (undefined seq) yields no highlight', () => {
  assert.equal(traceHighlight(CHAIN, undefined, ACTIVITY_EDGES), undefined);
});

void test('a seq no call carries yields no highlight', () => {
  assert.equal(traceHighlight(CHAIN, 99, ACTIVITY_EDGES), undefined);
});

void test('a synthetic view (blank stepNodeId) yields no highlight', () => {
  const synthetic = [call(1, ''), call(2, '')];
  assert.equal(traceHighlight(synthetic, 1, ACTIVITY_EDGES), undefined);
});

void test('an unowned call among owned ones never enters the visited set', () => {
  const mixed = [call(1, 'n-validate'), call(2, ''), call(3, 'n-notify')];
  const hl = traceHighlight(mixed, 3, ACTIVITY_EDGES);
  assert.deepEqual([...(hl?.visitedNodes ?? [])], ['n-validate', 'n-notify']);
  // …and it never becomes the you-are-here step either.
  assert.equal(traceHighlight(mixed, 2, ACTIVITY_EDGES), undefined);
});

void test('an empty chain yields no highlight', () => {
  assert.equal(traceHighlight([], 1, ACTIVITY_EDGES), undefined);
  assert.equal(traceHighlight([], undefined, ACTIVITY_EDGES), undefined);
});

void test('no activity edges yields a highlight with no walked edges', () => {
  const hl = traceHighlight(CHAIN, 4, []);
  assert.equal(hl?.current, 'n-notify');
  // …narrowed non-nullish by the assertion above.
  assert.equal(hl.visitedEdges.size, 0);
});
