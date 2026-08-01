/// <reference types="node" />
/**
 * Unit tests for walkthroughRoots: which nodes are legal entry points into an
 * activity diagram. Covers the single-start case (today's only entry), the
 * multi-root case an edge-less event node introduces, the entry-kind
 * restriction that excludes edge-less non-event nodes (fix-round-1 FINDING 1
 * — an edge-less `note` must never become a walkthrough root just because
 * nothing points to it) with bill/operate-shaped fixtures, and the case where
 * no node qualifies at all (the degenerate fallback then lives in the
 * CALLERS, not here — walkthroughRoots legitimately returns no roots). Also
 * covers walkthroughNavFloor: the Back/Restart rewind floor, which must drop
 * to 0 for multi-root diagrams so the entry chooser is reachable again
 * (fix-round-1 FINDING 1).
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

void test('an edge-less acceptEvent alongside start is a second root, start first', () => {
  const nodes = [
    { id: 's1', kind: 'start' as const },
    { id: 'ae1', kind: 'acceptEvent' as const },
    { id: 'a1', kind: 'action' as const },
  ];
  const edges = [{ from: 's1', to: 'a1' }];
  assert.deepEqual(walkthroughRoots(nodes, edges), ['s1', 'ae1']);
});

void test('an edge-less note is NOT a root, even with in-degree 0', () => {
  const nodes = [
    { id: 's1', kind: 'start' as const },
    { id: 'a1', kind: 'action' as const },
    { id: 'n1', kind: 'note' as const },
  ];
  const edges = [{ from: 's1', to: 'a1' }];
  assert.deepEqual(walkthroughRoots(nodes, edges), ['s1']);
});

void test('with no start node, a bare event entry is still the sole root', () => {
  const nodes = [
    { id: 'te1', kind: 'timeEvent' as const },
    { id: 'a1', kind: 'action' as const },
  ];
  const edges = [{ from: 'te1', to: 'a1' }];
  assert.deepEqual(walkthroughRoots(nodes, edges), ['te1']);
});

void test('with no start node and no event entry, an in-degree-0 action does not root (no legal entry — the entry-chooser fallback lives in the callers, not here)', () => {
  const nodes = [
    { id: 'a1', kind: 'action' as const },
    { id: 'a2', kind: 'action' as const },
  ];
  const edges = [{ from: 'a1', to: 'a2' }];
  assert.deepEqual(walkthroughRoots(nodes, edges), []);
});

void test('bill-shaped fixture: the timeEvent entry is the sole root; the edge-less note dead-end is not a second one', () => {
  // Mirrors bill-the-user-for-usage post-Task-7: period-elapses (timeEvent,
  // in-degree 0) is the entry; customer-charged is an edge-less note (Task-7
  // fold C) that must not surface as a beginning.
  const nodes = [
    { id: 'period-elapses', kind: 'timeEvent' as const },
    { id: 'read-hosting-usage', kind: 'action' as const },
    { id: 'customer-charged', kind: 'note' as const },
  ];
  const edges = [{ from: 'period-elapses', to: 'read-hosting-usage' }];
  assert.deepEqual(walkthroughRoots(nodes, edges), ['period-elapses']);
});

void test('operate-shaped fixture: start + schedule-fires are the two legitimate roots; the two edge-less notes are not extra ones', () => {
  // Mirrors operate-a-delivered-system post-Task-7: the operator path keeps
  // `start`; the reconcile sweep enters on schedule-fires (timeEvent,
  // in-degree 0); in-flight and argo-reconcile are edge-less notes (Task-7 A5)
  // that must not inflate the entry chooser to 4 roots.
  const nodes = [
    { id: 'start', kind: 'start' as const },
    { id: 'publish-trigger', kind: 'action' as const },
    { id: 'schedule-fires', kind: 'timeEvent' as const },
    { id: 'read-app-set', kind: 'action' as const },
    { id: 'in-flight', kind: 'note' as const },
    { id: 'argo-reconcile', kind: 'note' as const },
  ];
  const edges = [
    { from: 'start', to: 'publish-trigger' },
    { from: 'schedule-fires', to: 'read-app-set' },
  ];
  assert.deepEqual(walkthroughRoots(nodes, edges), ['start', 'schedule-fires']);
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
  // x1 has in-degree 0 but is an action, not start/timeEvent/acceptEvent — it
  // is no longer a legal root (fix-round-1 FINDING 1), and PATH_NODES' real
  // roots (s1, te1) have no edge into it, so it is genuinely unreachable.
  assert.equal(walkthroughPathTo(orphan, PATH_EDGES, 'x1'), undefined);
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
