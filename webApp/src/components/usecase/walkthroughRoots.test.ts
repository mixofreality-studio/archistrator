/// <reference types="node" />
/**
 * Unit tests for walkthroughRoots: which nodes are legal entry points into an
 * activity diagram. Covers the single-start case (today's only entry), the
 * multi-root case an edge-less event node introduces, and the no-start
 * fallback to the graph's in-degree-0 node.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { walkthroughRoots } from './walkthroughRoots.ts';

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
