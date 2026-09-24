/// <reference types="node" />
/**
 * The old rails' enforcement point (activityRedirect.ts) — the actual thing
 * that decides where an old `/construction` or `/design/*` bookmark lands.
 * Written directly after operationsGuard.test.ts: `isRedirect(err)` plus the
 * thrown Redirect's `options`, so the test exercises the redirect itself and
 * not a description of it.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { isRedirect } from '@tanstack/react-router';
import {
  designRedirectSearch,
  planSearchFromLegacy,
  PLAN_PATH,
  redirectToPlan,
} from './activityRedirect.ts';

void test('an old construction link keeps its lens and drops the pane selection', () => {
  assert.deepEqual(
    planSearchFromLegacy({
      lens: 'tasks',
      a: 'C-x',
      p: 'construction',
      k: 'codeReview',
      n: 2,
      av: 'code',
      focus: 1,
      sc: 'S1',
    }),
    { lens: 'tasks' }
  );
});

void test('an unknown or absent lens falls back to list rather than throwing', () => {
  assert.deepEqual(planSearchFromLegacy({ lens: 'nonsense' }), { lens: 'list' });
  assert.deepEqual(planSearchFromLegacy({}), { lens: 'list' });
});

void test('the design rails land on the plan list', () => {
  assert.deepEqual(designRedirectSearch(), { lens: 'list' });
});

void test('the redirect names the plan route, the project it came from, and its lens', () => {
  assert.throws(
    () => redirectToPlan('archistrator', { lens: 'graph' }),
    (err: unknown): boolean => {
      assert.ok(isRedirect(err), 'expected a Redirect to be thrown, not an arbitrary error');
      const options = (
        err as {
          options: { to?: string; params?: { projectId?: string }; search?: { lens?: string } };
        }
      ).options;
      assert.equal(options.to, PLAN_PATH);
      assert.equal(options.params?.projectId, 'archistrator');
      assert.equal(options.search?.lens, 'graph');
      return true;
    }
  );
});
