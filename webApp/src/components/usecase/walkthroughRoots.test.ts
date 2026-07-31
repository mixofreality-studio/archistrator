/// <reference types="node" />
/**
 * Unit tests for walkthroughRoots: which nodes are legal entry points into an
 * activity diagram. Covers the single-start case (today's only entry), the
 * multi-root case an edge-less event node introduces, and the no-start
 * fallback to the graph's in-degree-0 node. Also covers walkthroughNavFloor:
 * the Back/Restart rewind floor, which must drop to 0 for multi-root diagrams
 * so the entry chooser is reachable again (fix-round-1 FINDING 1).
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { walkthroughRoots, walkthroughNavFloor } from './walkthroughRoots.ts';

void test('a single start node is the only root', () => {
  const nodes = [
    { id: 's1', kind: 'start' as const },
    { id: 'a1', kind: 'action' as const },
  ];
  const edges = [{ from: 's1', to: 'a1' }];
  assert.deepEqual(walkthroughRoots(nodes, edges), ['s1']);
});

void test('an edge-less timeEvent alongside start is a second root, start first', () => {
  const nodes = [
    { id: 's1', kind: 'start' as const },
    { id: 'te1', kind: 'timeEvent' as const },
    { id: 'a1', kind: 'action' as const },
  ];
  const edges = [{ from: 's1', to: 'a1' }];
  assert.deepEqual(walkthroughRoots(nodes, edges), ['s1', 'te1']);
});

void test('with no start node, the first in-degree-0 node is the root', () => {
  const nodes = [
    { id: 'a1', kind: 'action' as const },
    { id: 'a2', kind: 'action' as const },
  ];
  const edges = [{ from: 'a1', to: 'a2' }];
  assert.deepEqual(walkthroughRoots(nodes, edges), ['a1']);
});

void test('a single-root diagram floors Back/Restart at path length 1', () => {
  assert.equal(walkthroughNavFloor(0), 1);
  assert.equal(walkthroughNavFloor(1), 1);
});

void test('a multi-root diagram floors Back/Restart at path length 0 (the chooser)', () => {
  assert.equal(walkthroughNavFloor(2), 0);
  assert.equal(walkthroughNavFloor(5), 0);
});
