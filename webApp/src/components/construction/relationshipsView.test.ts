/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import type { C4Component, C4View } from '../../contracts/adapters.ts';
import { utilityNeighbours, withoutUtilities } from './relationshipsView.ts';

function comp(id: string, name: string, layer: string): C4Component {
  return {
    id,
    name,
    kind: layer as C4Component['kind'],
    layer: layer as C4Component['layer'],
    encapsulates: '',
    encapsulatesVolatilities: [],
    contractKey: '',
  };
}

const VIEW = {
  components: [
    comp('construction-manager', 'ConstructionManager', 'manager'),
    comp('review-engine', 'ReviewEngine', 'engine'),
    comp('logging', 'Logging', 'utility'),
    comp('diagnostics', 'Diagnostics', 'utility'),
    comp('security', 'Security', 'utility'),
  ],
  relationships: [
    { from: 'construction-manager', to: 'review-engine', label: 'Propose', mode: 'sync' },
    { from: 'construction-manager', to: 'logging', label: 'Log', mode: 'sync' },
    { from: 'construction-manager', to: 'diagnostics', label: 'Trace', mode: 'sync' },
    { from: 'review-engine', to: 'security', label: 'Check', mode: 'sync' },
  ],
} as unknown as C4View;

void test('the Component view draws no utility and no edge to one', () => {
  const v = withoutUtilities(VIEW, 'construction-manager');
  assert.deepEqual(
    v.components.map((c) => c.id),
    ['construction-manager', 'review-engine']
  );
  assert.deepEqual(
    v.relationships.map((r) => `${r.from}>${r.to}`),
    ['construction-manager>review-engine']
  );
});

void test('the utilities line names only the ones this component reaches', () => {
  assert.deepEqual(utilityNeighbours(VIEW, 'construction-manager'), ['Logging', 'Diagnostics']);
  assert.deepEqual(utilityNeighbours(VIEW, 'review-engine'), ['Security']);
});

void test('a focal utility keeps itself', () => {
  const v = withoutUtilities(VIEW, 'logging');
  assert.ok(v.components.some((c) => c.id === 'logging'));
  assert.ok(!v.components.some((c) => c.id === 'diagnostics'));
});
