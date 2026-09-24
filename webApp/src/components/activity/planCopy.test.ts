/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  LENS_LABEL,
  M0_DIVIDER,
  criticalPathNote,
  planEmptyState,
  unplacedTilesNote,
} from './planCopy.ts';

void test('the lens labels name all three lenses', () => {
  assert.equal(LENS_LABEL.list, 'List');
  assert.equal(LENS_LABEL.graph, 'Graph');
  assert.equal(LENS_LABEL.tasks, 'Tasks');
});

void test('M0_DIVIDER names M0 and says construction begins', () => {
  assert.ok(M0_DIVIDER.includes('M0'));
  assert.ok(M0_DIVIDER.includes('construction begins'));
});

void test('unplacedTilesNote sorts its ids and says why nothing is drawn', () => {
  assert.equal(
    unplacedTilesNote(['C-b', 'C-a']),
    'Not drawn: C-a, C-b. These activities build no layered component, so the plan has no row for them — they are listed above.'
  );
});

void test('unplacedTilesNote does not mutate the array it is given', () => {
  const ids = ['C-b', 'C-a'];
  unplacedTilesNote(ids);
  assert.deepEqual(ids, ['C-b', 'C-a']);
});

void test('planEmptyState has one sentence per reason, and each names its own cause', () => {
  assert.equal(
    planEmptyState('noPlan'),
    'No activity list is committed yet. Approve the Project Design activity (M0) and the plan appears here.'
  );
  assert.equal(planEmptyState('noRows'), 'The committed activity list is empty.');
});

void test('criticalPathNote is singular at one and plural otherwise, the live count included', () => {
  assert.equal(criticalPathNote(1), '1 activity on the critical path');
  assert.equal(criticalPathNote(0), '0 activities on the critical path');
  assert.equal(criticalPathNote(15), '15 activities on the critical path');
});
