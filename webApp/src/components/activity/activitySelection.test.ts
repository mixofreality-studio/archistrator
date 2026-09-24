/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { isHistorical, selectionFor } from './activitySelection.ts';
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

void test('history is a revision below the latest — and never the never-run zero', () => {
  const nodes = [node('srs', 'done', [rev(1), rev(2)]), node('stp', 'pending')];
  assert.equal(isHistorical(nodes, { taskId: 'srs', revision: 1 }), true);
  assert.equal(isHistorical(nodes, { taskId: 'srs', revision: 2 }), false);
  assert.equal(isHistorical(nodes, { taskId: 'stp', revision: 0 }), false);
  assert.equal(isHistorical(nodes, { taskId: 'gone', revision: 1 }), false);
});
