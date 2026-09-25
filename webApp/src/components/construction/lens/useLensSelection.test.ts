/**
 * What survives of the lens module: the route's `validateSearch` rule and the
 * shared toolbar's content signature.
 *
 * ── Task 13 reduced this file; it did not delete it ─────────────────────────
 * The plan's §7.4 teardown table says "delete `useLensSelection.test.ts`",
 * because it was written for the console's URL codec (`parseLensSearch`,
 * `serializeLensSearch`, `artifactKeptFor`) and that codec went with the
 * DetailPane it addressed. But three of its cases covered exports that are
 * KEPT — `validateLensSearch`, `toolbarSignatureOf` and `DEFAULT_TOOLBAR` — and
 * deleting a test of live code is a regression, not a teardown. So the codec
 * cases are gone and those three stay, unchanged. `artifactViewSearch.test.ts`
 * WAS deleted outright: every one of its cases named the deleted codec.
 *
 * The toolbar signature is tested here for the same reason NetworkView's
 * signatureOf is — it must stay STABLE across a fresh-but-identical poll
 * envelope and change only when the dataset genuinely changes.
 */
/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import { validateLensSearch, toolbarSignatureOf, DEFAULT_TOOLBAR } from './useLensSelection.ts';

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
