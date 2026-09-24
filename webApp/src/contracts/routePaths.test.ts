/// <reference types="node" />
/**
 * The plan / activity route paths and their search codecs. These literals are
 * registered verbatim by router.tsx, and every container and component that
 * navigates reads them from here — so a drift between the two is a dead link,
 * which is what the first test pins.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { ACTIVITY_PATH, activitySearch, PLAN_PATH, planSearch } from './routePaths.ts';

void test('the path literals are exactly what the router registers', () => {
  assert.equal(PLAN_PATH, '/project/$projectId/plan');
  assert.equal(ACTIVITY_PATH, '/project/$projectId/activity/$activityId');
});

void test('an unknown lens falls back to list, and the default is still emitted', () => {
  assert.deepEqual(planSearch('graph'), { lens: 'graph' });
  assert.deepEqual(planSearch('nonsense'), { lens: 'list' });
  assert.deepEqual(planSearch(undefined), { lens: 'list' });
});

void test('a junk revision is dropped rather than coerced to NaN', () => {
  assert.deepEqual(activitySearch('srsReview', '2'), { task: 'srsReview', rev: 2 });
  assert.deepEqual(activitySearch('srsReview', 'x'), { task: 'srsReview' });
  assert.deepEqual(activitySearch('srsReview', '0'), { task: 'srsReview' });
  assert.deepEqual(activitySearch('srsReview', '1.5'), { task: 'srsReview' });
  assert.deepEqual(activitySearch('', undefined), {});
});
