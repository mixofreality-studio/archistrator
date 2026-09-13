/**
 * The graph's filter status (graphFilter.ts) — designer P1-4: a filter is never
 * silent, and "nothing matches" reads exactly as it does in the list.
 */
/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import { DEFAULT_TOOLBAR } from '../lens/useLensSelection.ts';
import { filtersActive, graphFilterStatusFor } from './graphFilter.ts';

void test('the default toolbar is no filter', () => {
  assert.equal(filtersActive(DEFAULT_TOOLBAR), false);
  assert.equal(filtersActive({ ...DEFAULT_TOOLBAR, search: '   ' }), false);
});

void test('search, scope, kind and layer each count as a filter', () => {
  assert.equal(filtersActive({ ...DEFAULT_TOOLBAR, search: 'billing' }), true);
  assert.equal(filtersActive({ ...DEFAULT_TOOLBAR, scope: 'critical' }), true);
  assert.equal(filtersActive({ ...DEFAULT_TOOLBAR, kind: 'service' }), true);
  assert.equal(filtersActive({ ...DEFAULT_TOOLBAR, layer: 'manager' }), true);
});

void test('with no filter active there is no status line', () => {
  assert.equal(graphFilterStatusFor(29, 29, '', false), undefined);
});

void test('an active filter says how many match, and always offers the way back', () => {
  assert.deepEqual(graphFilterStatusFor(7, 29, '', true), {
    message: '7 of 29 match',
    separated: true,
    offerClear: true,
    matched: 7,
    total: 29,
  });
  assert.equal(graphFilterStatusFor(29, 29, '', true)?.message, '29 of 29 match');
});

void test("nothing matching reuses the list's own no-match copy", () => {
  assert.equal(graphFilterStatusFor(0, 29, 'zzz', true)?.message, 'No activity matches “zzz”.');
  assert.equal(
    graphFilterStatusFor(0, 29, '', true)?.message,
    'No activity matches the current filters.'
  );
});

void test('a sentence that ends in its own punctuation takes no " · " before "Clear filters"', () => {
  assert.equal(graphFilterStatusFor(0, 29, 'zzz', true)?.separated, false);
  assert.equal(graphFilterStatusFor(0, 29, '', true)?.separated, false);
  assert.equal(graphFilterStatusFor(7, 29, '', true)?.separated, true);
});
