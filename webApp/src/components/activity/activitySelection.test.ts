/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { isHistorical, revisionParam, selectionFor } from './activitySelection.ts';
import type { LifecycleNode, LifecycleRevision } from './lifecycleGraphTypes.ts';

function rev(n: number): LifecycleRevision {
  return { n, outcome: 'approved' };
}

function node(
  id: string,
  state: LifecycleNode['state'],
  revisions: readonly LifecycleRevision[] = []
): LifecycleNode {
  return { id, kind: 'dispatch', title: id, phase: 'phase', state, dependsOn: [], revisions };
}

void test('a task the activity does not have falls back to the default task', () => {
  const nodes = [node('srs', 'done', [rev(1)]), node('srsReview', 'awaitingHuman', [rev(1)])];
  // `awaitingHuman` is the default-task rule's first choice (defaultTask.ts).
  assert.deepEqual(selectionFor(nodes, 'noSuchTask', undefined), {
    taskId: 'srsReview',
    revision: 1,
  });
});

void test('a revision the task does not have falls back to its latest', () => {
  const nodes = [node('srs', 'done', [rev(1), rev(2)])];
  assert.deepEqual(selectionFor(nodes, 'srs', 9), { taskId: 'srs', revision: 2 });
  assert.deepEqual(selectionFor(nodes, 'srs', 1), { taskId: 'srs', revision: 1 });
});

void test('a task that has never run selects revision 0, not 1', () => {
  const nodes = [node('srs', 'pending')];
  assert.deepEqual(selectionFor(nodes, 'srs', undefined), { taskId: 'srs', revision: 0 });
  // Even when the URL insists on one: there is no revision 1 to read.
  assert.deepEqual(selectionFor(nodes, 'srs', 1), { taskId: 'srs', revision: 0 });
});

void test('an activity with no tasks selects nothing', () => {
  assert.equal(selectionFor([], 'srs', 1), undefined);
});

void test('navigating to the latest revision writes no ?rev at all', () => {
  const nodes = [node('srs', 'done', [rev(1), rev(2)])];
  assert.equal(revisionParam(nodes, 'srs', 2), undefined);
  // Nor anything above it: there is no such revision to address.
  assert.equal(revisionParam(nodes, 'srs', 3), undefined);
});

void test('picking an older revision writes it', () => {
  const nodes = [node('srs', 'done', [rev(1), rev(2), rev(3)])];
  assert.equal(revisionParam(nodes, 'srs', 1), 1);
  assert.equal(revisionParam(nodes, 'srs', 2), 2);
});

void test('a task that has never run, and a task the graph does not carry, write no ?rev', () => {
  const nodes = [node('srs', 'pending'), node('stp', 'done', [rev(1)])];
  assert.equal(revisionParam(nodes, 'srs', 0), undefined);
  assert.equal(revisionParam(nodes, 'stp', 0), undefined);
  assert.equal(revisionParam(nodes, 'gone', 1), undefined);
});

void test('a fresh revision arriving while ?rev is absent keeps the reader on the latest', () => {
  // The whole point of omitting `rev`: the poll that lands revision 3 moves the
  // reader onto it instead of stranding them in a read-only history of 2.
  const before = [node('srs', 'running', [rev(1), rev(2)])];
  const after = [node('srs', 'awaitingHuman', [rev(1), rev(2), rev(3)])];
  assert.deepEqual(selectionFor(before, 'srs', undefined), { taskId: 'srs', revision: 2 });
  assert.deepEqual(selectionFor(after, 'srs', undefined), { taskId: 'srs', revision: 3 });
  assert.equal(isHistorical(after, { taskId: 'srs', revision: 3 }), false);
});

void test('back to latest clears ?rev', () => {
  // The control passes the task's latest, and the latest writes no param —
  // which is what puts the reader back on the head.
  const nodes = [node('srs', 'done', [rev(1), rev(2)])];
  assert.equal(revisionParam(nodes, 'srs', 2), undefined);
  assert.deepEqual(selectionFor(nodes, 'srs', undefined), { taskId: 'srs', revision: 2 });
});

void test('history is a revision below the latest — and never the never-run zero', () => {
  const nodes = [node('srs', 'done', [rev(1), rev(2)]), node('stp', 'pending')];
  assert.equal(isHistorical(nodes, { taskId: 'srs', revision: 1 }), true);
  assert.equal(isHistorical(nodes, { taskId: 'srs', revision: 2 }), false);
  assert.equal(isHistorical(nodes, { taskId: 'stp', revision: 0 }), false);
  assert.equal(isHistorical(nodes, { taskId: 'gone', revision: 1 }), false);
});
