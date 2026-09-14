/**
 * The artifact view's deep links (designer §3): `av` names the contract's tab and
 * `focus=1` opens the focus view. They live in the URL beside the selection, so
 * the 1.5s poll's remount can neither reset the tab nor close the focus view —
 * and the codec stays total: junk is dropped, never rendered.
 */
/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import {
  artifactKeptFor,
  parseLensSearch,
  serializeLensSearch,
  validateLensSearch,
} from './useLensSelection.ts';

void test('av and focus round-trip with a selected activity', () => {
  const parsed = parseLensSearch({
    lens: 'list',
    a: 'C-x',
    p: 'detailed_design',
    av: 'component',
    focus: '1',
  });
  assert.deepEqual(parsed.artifact, { view: 'component', focus: true });
  assert.deepEqual(serializeLensSearch(parsed), {
    lens: 'list',
    a: 'C-x',
    p: 'detailed_design',
    av: 'component',
    focus: 1,
  });
  assert.deepEqual(parseLensSearch({ ...serializeLensSearch(parsed) }), parsed);
});

void test('junk views and focus values are dropped, and neither survives without an activity', () => {
  assert.equal(
    parseLensSearch({ lens: 'list', a: 'C-x', av: 'nope', focus: 'yes' }).artifact,
    undefined
  );
  assert.equal(parseLensSearch({ lens: 'list', av: 'code', focus: '1' }).artifact, undefined);
  assert.deepEqual(validateLensSearch({ lens: 'list', av: 'code', focus: 1 }), { lens: 'list' });
  assert.deepEqual(validateLensSearch({ lens: 'list', a: 'C-x', focus: 1 }), {
    lens: 'list',
    a: 'C-x',
    focus: 1,
  });
});

void test('the artifact view is kept for the same activity and dropped for another', () => {
  const prev = parseLensSearch({ lens: 'list', a: 'C-x', av: 'facets', focus: '1' });
  assert.deepEqual(artifactKeptFor(prev, { activityId: 'C-x', task: 'designReview', attempt: 2 }), {
    view: 'facets',
    focus: true,
  });
  assert.equal(artifactKeptFor(prev, { activityId: 'C-y' }), undefined);
  // A neighbour hop passes its own view explicitly, which wins.
  assert.deepEqual(artifactKeptFor(prev, { activityId: 'C-y' }, { view: 'component' }), {
    view: 'component',
  });
});
