/**
 * The narrow-screen detail drawer's modality (detailDrawer.ts) — designer P2:
 * non-modal in the graph lens, where the canvas must stay reachable.
 */
/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import { detailDrawerModal } from './detailDrawer.ts';

void test('the graph lens gets a NON-modal drawer', () => {
  assert.equal(detailDrawerModal('graph'), false);
});

void test('the list and tasks lenses keep the modal drawer', () => {
  assert.equal(detailDrawerModal('list'), true);
  assert.equal(detailDrawerModal('tasks'), true);
});
