/// <reference types="node" />
/**
 * Unit tests for callTrail's visitedSeqsForPath: the walked-calls set the
 * Dynamic lens accretes as the walkthrough advances (founder QA round 4).
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { visitedSeqsForPath, type TrailCall } from './callTrail.ts';

/** A miniature chain: step n1 authors calls 1-2, n2 authors 3, n4 authors 4-5. */
const CALLS: TrailCall[] = [
  { seq: 1, stepNodeId: 'n1' },
  { seq: 2, stepNodeId: 'n1' },
  { seq: 3, stepNodeId: 'n2' },
  { seq: 4, stepNodeId: 'n4' },
  { seq: 5, stepNodeId: 'n4' },
];

void test('the entry chooser (empty path) has walked past nothing', () => {
  assert.deepEqual(visitedSeqsForPath(CALLS, []), new Set());
});

void test('standing on the first step leaves the trail empty — nothing is BEHIND you', () => {
  assert.deepEqual(visitedSeqsForPath(CALLS, ['n1']), new Set());
});

void test('every call of every step already left is on the trail', () => {
  assert.deepEqual(visitedSeqsForPath(CALLS, ['n1', 'n2']), new Set([1, 2]));
  assert.deepEqual(visitedSeqsForPath(CALLS, ['n1', 'n2', 'n4']), new Set([1, 2, 3]));
});

void test('call-less path nodes contribute nothing but do not break the trail', () => {
  // n3 authors no calls at all (a merge); walking through it keeps n1+n2 lit.
  assert.deepEqual(visitedSeqsForPath(CALLS, ['n1', 'n2', 'n3']), new Set([1, 2, 3]));
});

void test('the trail shrinks when the reader steps Back (path-derived, no state)', () => {
  const full = ['n1', 'n2', 'n4'];
  assert.deepEqual(visitedSeqsForPath(CALLS, full), new Set([1, 2, 3]));
  assert.deepEqual(visitedSeqsForPath(CALLS, full.slice(0, -1)), new Set([1, 2]));
  assert.deepEqual(visitedSeqsForPath(CALLS, full.slice(0, -2)), new Set());
});

void test('a loop back onto a visited node keeps that node on the trail too', () => {
  // regenerate/escalate arc: n1 → n2 → n1. n1 is both behind you and current.
  assert.deepEqual(visitedSeqsForPath(CALLS, ['n1', 'n2', 'n1']), new Set([1, 2, 3]));
});

void test('a chain with no calls yields an empty trail whatever the path', () => {
  assert.deepEqual(visitedSeqsForPath([], ['n1', 'n2', 'n3']), new Set());
});

void test('path nodes that author nothing anywhere in the chain are simply absent', () => {
  assert.deepEqual(visitedSeqsForPath(CALLS, ['zz', 'yy', 'n2']), new Set());
});
