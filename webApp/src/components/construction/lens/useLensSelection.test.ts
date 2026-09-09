/**
 * The lens/selection search-param codec — the pure half of useLensSelection.
 *
 * Selection lives in the URL (`?lens=&a=&p=&k=&n=`) rather than in component
 * state so the shared detail pane never owns it, the 1.5s cascade poll's
 * remount cannot wipe it, and a link addresses exactly one task attempt. That
 * only holds if the codec is total: every malformed value has to degrade to a
 * renderable state rather than propagate (`NaN` attempts, unknown lenses).
 *
 * The toolbar signature is tested here for the same reason NetworkView's
 * signatureOf is — it must stay STABLE across a fresh-but-identical poll
 * envelope and change only when the dataset genuinely changes.
 */
/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import {
  parseLensSearch,
  serializeLensSearch,
  validateLensSearch,
  toolbarSignatureOf,
  DEFAULT_TOOLBAR,
} from './useLensSelection.ts';

void test('round-trips a full selection through search params', () => {
  const parsed = parseLensSearch({
    lens: 'list',
    a: 'C-BG',
    p: 'construction',
    k: 'codeReview',
    n: '2',
  });
  assert.equal(parsed.lens, 'list');
  assert.deepEqual(parsed.selection, {
    activityId: 'C-BG',
    lifecyclePhase: 'construction',
    task: 'codeReview',
    attempt: 2,
  });
});

void test('defaults to the list lens and an empty selection', () => {
  const parsed = parseLensSearch({});
  assert.equal(parsed.lens, 'list');
  assert.deepEqual(parsed.selection, {});
});

void test('rejects an unknown lens rather than rendering a blank surface', () => {
  assert.equal(parseLensSearch({ lens: 'bogus' }).lens, 'list');
  assert.equal(parseLensSearch({ lens: 42 }).lens, 'list');
});

void test('drops a non-numeric attempt rather than passing NaN downstream', () => {
  assert.equal(parseLensSearch({ a: 'C-BG', n: 'x' }).selection.attempt, undefined);
  assert.equal(parseLensSearch({ a: 'C-BG', n: '' }).selection.attempt, undefined);
  assert.equal(parseLensSearch({ a: 'C-BG', n: '1.5' }).selection.attempt, undefined);
  assert.equal(parseLensSearch({ a: 'C-BG', n: '0' }).selection.attempt, undefined);
  assert.equal(parseLensSearch({ a: 'C-BG', n: '-3' }).selection.attempt, undefined);
});

void test('accepts the graph and tasks lenses', () => {
  assert.equal(parseLensSearch({ lens: 'graph' }).lens, 'graph');
  assert.equal(parseLensSearch({ lens: 'tasks' }).lens, 'tasks');
});

void test('serialize → parse is a round trip', () => {
  const state = {
    lens: 'tasks' as const,
    selection: {
      activityId: 'C-BG',
      lifecyclePhase: 'construction',
      task: 'codeReview',
      attempt: 3,
    },
  };
  const search = serializeLensSearch(state);
  assert.deepEqual(search, {
    lens: 'tasks',
    a: 'C-BG',
    p: 'construction',
    k: 'codeReview',
    n: 3,
  });
  assert.deepEqual(parseLensSearch({ ...search }), state);
});

void test('serializing an empty selection emits only the lens', () => {
  assert.deepEqual(serializeLensSearch({ lens: 'list', selection: {} }), { lens: 'list' });
});

void test('validateLensSearch keeps a deep link addressable and drops the junk', () => {
  // The route's validateSearch: whatever it returns IS the URL's search, so an
  // explicit ?lens=list&a=C-BG must survive verbatim or the deep link is a lie.
  assert.deepEqual(validateLensSearch({ lens: 'list', a: 'C-BG' }), { lens: 'list', a: 'C-BG' });
  assert.deepEqual(validateLensSearch({ lens: 'bogus', a: 'C-BG', n: 'x', zz: 'drop me' }), {
    lens: 'list',
    a: 'C-BG',
  });
});

void test('the toolbar signature survives a fresh-but-identical poll envelope', () => {
  const a = toolbarSignatureOf('archistrator', ['C-BG', 'C-CW', 'G-SPA']);
  const b = toolbarSignatureOf('archistrator', ['G-SPA', 'C-BG', 'C-CW']);
  assert.equal(a, b, 'a re-fetch of the same activity set must keep the same signature');
});

void test('the toolbar signature changes for a genuinely different dataset', () => {
  const base = toolbarSignatureOf('archistrator', ['C-BG', 'C-CW']);
  assert.notEqual(base, toolbarSignatureOf('gtdapp', ['C-BG', 'C-CW']));
  assert.notEqual(base, toolbarSignatureOf('archistrator', ['C-BG', 'C-CW', 'G-SPA']));
});

void test('the toolbar starts unfiltered', () => {
  assert.deepEqual(DEFAULT_TOOLBAR, {
    search: '',
    scope: 'all',
    kind: 'all',
    layer: 'all',
    sort: 'network',
  });
});
