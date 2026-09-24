/// <reference types="node" />
/**
 * Unit tests for the revision helpers in lifecycleGraphTypes.ts: the one line a
 * revision is described by (the graph's menu and the body's select share it),
 * and which revision a CLICK across to another task lands on.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  backEdgesOf,
  latestRevision,
  revisionLine,
  revisionOnNavigate,
  type LifecycleNode,
  type LifecycleRevision,
} from './lifecycleGraphTypes.ts';

function node(id: string, group: string | undefined, revisions: number): LifecycleNode {
  return {
    id,
    kind: id.endsWith('Review') ? 'review' : 'dispatch',
    title: id,
    phase: 'p',
    state: 'done',
    dependsOn: [],
    revisions: Array.from(
      { length: revisions },
      (_, i): LifecycleRevision => ({ n: i + 1, outcome: 'x' })
    ),
    revisionGroup: group,
  };
}

// Draft+review pairs share a group; the second pair's review is one revision behind
// (its draft is re-running after a send-back).
const NODES = [
  node('archDraft', 'arch', 3),
  node('archReview', 'arch', 3),
  node('opsDraft', 'ops', 2),
  node('opsReview', 'ops', 1),
  node('spike', undefined, 2),
];

void test('a revision reads as one line, and only the latest says so', () => {
  const r: LifecycleRevision = { n: 2, outcome: 'sent back', commentCount: 4, at: 'Sep 12' };
  assert.equal(revisionLine(r, false), 'Revision 2 · sent back · 4 comments · Sep 12');
  assert.equal(
    revisionLine({ n: 3, outcome: 'awaiting you' }, true),
    'Revision 3 · awaiting you (latest)'
  );
  assert.equal(
    revisionLine({ n: 1, outcome: 'succeeded', detail: '14m · 312k tok', commentCount: 1 }, false),
    'Revision 1 · succeeded · 14m · 312k tok · 1 comment'
  );
});

void test('latestRevision is the highest n, and 0 for a task that never ran', () => {
  assert.equal(latestRevision(NODES[0] ?? node('x', undefined, 0)), 3);
  assert.equal(latestRevision(node('never', undefined, 0)), 0);
});

void test('clicking across a draft↔review pair keeps the revision being read', () => {
  assert.equal(revisionOnNavigate(NODES, { nodeId: 'archDraft', revision: 2 }, 'archReview'), 2);
  assert.equal(revisionOnNavigate(NODES, { nodeId: 'archReview', revision: 1 }, 'archDraft'), 1);
});

void test('reading the LATEST revision carries nothing — a click opens the latest', () => {
  // opsDraft's latest is 2; its review only has 1. Latest → latest, not "2, else…".
  assert.equal(revisionOnNavigate(NODES, { nodeId: 'opsDraft', revision: 2 }, 'opsReview'), 1);
  assert.equal(revisionOnNavigate(NODES, { nodeId: 'archDraft', revision: 3 }, 'archReview'), 3);
});

void test('a revision the target never had falls back to its latest', () => {
  const nodes = [node('d', 'g', 3), node('dReview', 'g', 1)];
  assert.equal(revisionOnNavigate(nodes, { nodeId: 'd', revision: 2 }, 'dReview'), 1);
});

void test('a different group, or no group at all, always opens the latest', () => {
  assert.equal(revisionOnNavigate(NODES, { nodeId: 'archDraft', revision: 1 }, 'opsDraft'), 2);
  assert.equal(revisionOnNavigate(NODES, { nodeId: 'spike', revision: 1 }, 'archDraft'), 3);
  assert.equal(revisionOnNavigate(NODES, { nodeId: 'archDraft', revision: 1 }, 'spike'), 2);
});

void test('re-clicking the task you are on, or an unknown task, is the latest / nothing', () => {
  assert.equal(revisionOnNavigate(NODES, { nodeId: 'archDraft', revision: 1 }, 'archDraft'), 3);
  assert.equal(revisionOnNavigate(NODES, { nodeId: 'archDraft', revision: 1 }, 'ghost'), 0);
});

void test('a return edge exists IFF the review has sent work back', () => {
  // arch: 3 revisions ⇒ sent back twice. ops: draft on rev 2, review still on rev 1 ⇒ once.
  assert.deepEqual(backEdgesOf(NODES), [
    { from: 'archReview', to: 'archDraft', revisions: 3 },
    { from: 'opsReview', to: 'opsDraft', revisions: 2 },
  ]);
  // approved first time / never ran / no review at all ⇒ nothing
  assert.deepEqual(backEdgesOf([node('d', 'g', 1), node('dReview', 'g', 1)]), []);
  assert.deepEqual(backEdgesOf([node('d', 'g', 0), node('dReview', 'g', 0)]), []);
  assert.deepEqual(backEdgesOf([node('spike', undefined, 3)]), []);
});

void test('a review sitting in sentBack has its arc before the redraft begins', () => {
  const review: LifecycleNode = { ...node('dReview', 'g', 1), state: 'sentBack' };
  assert.deepEqual(backEdgesOf([node('d', 'g', 1), review]), [
    { from: 'dReview', to: 'd', revisions: 2 },
  ]);
});

void test('a review with no dispatch task in its group has nothing to point back at', () => {
  assert.deepEqual(backEdgesOf([node('gateReview', 'gate', 2)]), []);
});
