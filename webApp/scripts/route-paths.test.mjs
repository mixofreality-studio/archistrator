/**
 * `activitySearch` (contracts/routePaths.ts) — the activity route's
 * validateSearch codec. The file imports nothing, so node loads it directly
 * (type stripping); no bundle step.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { activitySearch } from '../src/contracts/routePaths.ts';

test('task and a positive-integer rev pass through', () => {
  assert.deepEqual(activitySearch('glossaryReview', '2'), { task: 'glossaryReview', rev: 2 });
});

test('junk task and rev are dropped, never coerced', () => {
  assert.deepEqual(activitySearch('', 'x'), {});
  assert.deepEqual(activitySearch(undefined, 0), {});
  assert.deepEqual(activitySearch(3, 1.5), {});
});

test('the architecture deep link (view + step) survives validation', () => {
  assert.deepEqual(activitySearch('architectureReview', undefined, 'uc-run', 3), {
    task: 'architectureReview',
    view: 'uc-run',
    step: 3,
  });
  assert.deepEqual(activitySearch('architectureReview', undefined, 'uc-run', '4'), {
    task: 'architectureReview',
    view: 'uc-run',
    step: 4,
  });
});

test('a blank view and a non-positive step are dropped', () => {
  assert.deepEqual(activitySearch('architectureReview', undefined, '', 0), {
    task: 'architectureReview',
  });
  assert.deepEqual(activitySearch(undefined, undefined, 'v', -2), { view: 'v' });
});
