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
    a: 'C-billing-engine',
    p: 'construction',
    k: 'codeReview',
    n: '2',
  });
  assert.equal(parsed.lens, 'list');
  assert.deepEqual(parsed.selection, {
    activityId: 'C-billing-engine',
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
  assert.equal(parseLensSearch({ a: 'C-billing-engine', n: 'x' }).selection.attempt, undefined);
  assert.equal(parseLensSearch({ a: 'C-billing-engine', n: '' }).selection.attempt, undefined);
  assert.equal(parseLensSearch({ a: 'C-billing-engine', n: '1.5' }).selection.attempt, undefined);
  assert.equal(parseLensSearch({ a: 'C-billing-engine', n: '0' }).selection.attempt, undefined);
  assert.equal(parseLensSearch({ a: 'C-billing-engine', n: '-3' }).selection.attempt, undefined);
});

void test('accepts the graph and tasks lenses', () => {
  assert.equal(parseLensSearch({ lens: 'graph' }).lens, 'graph');
  assert.equal(parseLensSearch({ lens: 'tasks' }).lens, 'tasks');
});

void test('serialize → parse is a round trip', () => {
  const state = {
    lens: 'tasks' as const,
    selection: {
      activityId: 'C-billing-engine',
      lifecyclePhase: 'construction',
      task: 'codeReview',
      attempt: 3,
    },
  };
  const search = serializeLensSearch(state);
  assert.deepEqual(search, {
    lens: 'tasks',
    a: 'C-billing-engine',
    p: 'construction',
    k: 'codeReview',
    n: 3,
  });
  assert.deepEqual(parseLensSearch({ ...search }), state);
});

void test('serializing an empty selection emits only the lens', () => {
  assert.deepEqual(serializeLensSearch({ lens: 'list', selection: {} }), { lens: 'list' });
});

void test('validateLensSearch keeps the lens and drops every pane param (stage 5, R8)', () => {
  // The route's validateSearch: whatever it returns IS the URL's search. Since
  // stage 5 that is the LENS and nothing else — `a`/`p`/`k`/`n` addressed a task
  // attempt inside the DetailPane and `av`/`focus`/`sc` a view of its artifact,
  // and that pane is gone, so carrying them would put junk in the address bar.
  assert.deepEqual(validateLensSearch({ lens: 'list', a: 'C-billing-engine' }), { lens: 'list' });
  assert.deepEqual(validateLensSearch({ lens: 'tasks' }), { lens: 'tasks' });
  // The default is still EMITTED, and an unknown lens falls back to it rather
  // than throwing — a blank surface is worse than the default one.
  assert.deepEqual(
    validateLensSearch({ lens: 'bogus', a: 'C-billing-engine', n: 'x', zz: 'drop me' }),
    { lens: 'list' }
  );
  assert.deepEqual(validateLensSearch({}), { lens: 'list' });
});

void test('the toolbar signature survives a fresh-but-identical poll envelope', () => {
  const a = toolbarSignatureOf('archistrator', [
    'C-billing-engine',
    'C-construction-manager',
    'U-SPA-web-client',
  ]);
  const b = toolbarSignatureOf('archistrator', [
    'U-SPA-web-client',
    'C-billing-engine',
    'C-construction-manager',
  ]);
  assert.equal(a, b, 'a re-fetch of the same activity set must keep the same signature');
});

void test('the toolbar signature changes for a genuinely different dataset', () => {
  const base = toolbarSignatureOf('archistrator', ['C-billing-engine', 'C-construction-manager']);
  assert.notEqual(
    base,
    toolbarSignatureOf('gtdapp', ['C-billing-engine', 'C-construction-manager'])
  );
  assert.notEqual(
    base,
    toolbarSignatureOf('archistrator', [
      'C-billing-engine',
      'C-construction-manager',
      'U-SPA-web-client',
    ])
  );
});

void test('the toolbar starts unfiltered', () => {
  assert.deepEqual(DEFAULT_TOOLBAR, {
    search: '',
    scope: 'all',
    kind: 'all',
    layer: 'all',
    sort: 'network',
    observedOnly: false,
  });
});
