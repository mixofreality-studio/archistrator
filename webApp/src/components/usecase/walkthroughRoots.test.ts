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
import { walkthroughRoots, walkthroughNavFloor, walkthroughPathTo } from './walkthroughRoots.ts';

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

// --- walkthroughPathTo: the deep-link seed path ----------------------------
//
//   s1 → a1 → d1 ─[yes]→ a2 → e1
//                 └[no]─→ a3 → e1
// plus an edge-less event root (te1 → a3) so the multi-root + shortest-path
// tie-breaks are covered by the same fixture.
const PATH_NODES = [
  { id: 's1', kind: 'start' as const },
  { id: 'a1', kind: 'action' as const },
  { id: 'd1', kind: 'decision' as const },
  { id: 'a2', kind: 'action' as const },
  { id: 'a3', kind: 'action' as const },
  { id: 'e1', kind: 'end' as const },
  { id: 'te1', kind: 'timeEvent' as const },
];
const PATH_EDGES = [
  { from: 's1', to: 'a1' },
  { from: 'a1', to: 'd1' },
  { from: 'd1', to: 'a2' },
  { from: 'd1', to: 'a3' },
  { from: 'a2', to: 'e1' },
  { from: 'a3', to: 'e1' },
  { from: 'te1', to: 'a3' },
];

void test('the path to a mid-diagram node is the route from its root', () => {
  assert.deepEqual(walkthroughPathTo(PATH_NODES, PATH_EDGES, 'a2'), ['s1', 'a1', 'd1', 'a2']);
});

void test('a root resolves to a single-node path', () => {
  assert.deepEqual(walkthroughPathTo(PATH_NODES, PATH_EDGES, 's1'), ['s1']);
  assert.deepEqual(walkthroughPathTo(PATH_NODES, PATH_EDGES, 'te1'), ['te1']);
});

void test('the SHORTEST route wins when several roots reach the target', () => {
  // s1 reaches a3 in 4 nodes; the te1 event root reaches it in 2 — BFS takes
  // the short one, so the reader lands on the fewest steps that explain it.
  assert.deepEqual(walkthroughPathTo(PATH_NODES, PATH_EDGES, 'a3'), ['te1', 'a3']);
});

void test('equal-length routes break ties by authored edge order', () => {
  // Single-root variant of the fixture (no event entry): both decision arms
  // reach e1 in the same number of hops, and d1→a2 is authored first.
  const nodes = PATH_NODES.filter((n) => n.id !== 'te1');
  const edges = PATH_EDGES.filter((e) => e.from !== 'te1');
  assert.deepEqual(walkthroughPathTo(nodes, edges, 'e1'), ['s1', 'a1', 'd1', 'a2', 'e1']);
});

void test('an unknown or unreachable target has no path', () => {
  assert.equal(walkthroughPathTo(PATH_NODES, PATH_EDGES, 'nope'), undefined);
  assert.equal(walkthroughPathTo(PATH_NODES, PATH_EDGES, ''), undefined);
  const orphan = [...PATH_NODES, { id: 'x1', kind: 'action' as const }];
  // x1 has in-degree 0, so it is itself a root — reachable as a 1-node path.
  assert.deepEqual(walkthroughPathTo(orphan, PATH_EDGES, 'x1'), ['x1']);
  // A node reachable only from an unreachable one stays unreachable.
  const cyclic = [
    { id: 'c1', kind: 'action' as const },
    { id: 'c2', kind: 'action' as const },
    { id: 'c3', kind: 'action' as const },
  ];
  const cyclicEdges = [
    { from: 'c1', to: 'c2' },
    { from: 'c2', to: 'c1' },
    { from: 'c3', to: 'c3' },
  ];
  // No start and no in-degree-0 node: the degenerate fallback roots at the
  // first node, so c3 (its own cycle) is not reachable from it.
  assert.deepEqual(walkthroughPathTo(cyclic, cyclicEdges, 'c2'), ['c1', 'c2']);
  assert.equal(walkthroughPathTo(cyclic, cyclicEdges, 'c3'), undefined);
});

void test('an empty diagram has no paths', () => {
  assert.equal(walkthroughPathTo([], [], 'a1'), undefined);
});
