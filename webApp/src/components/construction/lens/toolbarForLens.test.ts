/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import { LIST_LENS_ONLY, RANKED_LABEL, toolbarForLens } from './toolbarForLens.ts';

void test('the TASKS lens names its own order and disables the list-only controls (designer P1-1)', () => {
  assert.deepEqual(toolbarForLens('tasks'), { sort: 'ranked', listControls: false });
  assert.equal(RANKED_LABEL, 'Ranked: risk floor · blast radius · id');
  assert.equal(LIST_LENS_ONLY, 'List lens only');
});

void test('the list keeps its Sort menu and its tree controls', () => {
  assert.deepEqual(toolbarForLens('list'), { sort: 'menu', listControls: true });
  assert.deepEqual(toolbarForLens('graph'), { sort: 'menu', listControls: true });
});
