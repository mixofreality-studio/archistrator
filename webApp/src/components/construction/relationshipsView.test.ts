/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import type { C4Component, C4View } from '../../contracts/adapters.ts';
import {
  callsOf,
  EDGE_LABEL_MAX,
  edgeCallLabel,
  neighbourRowsFor,
  shortCallLabel,
  utilityNeighbours,
  withoutUtilities,
} from './relationshipsView.ts';

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

void test('the pane lists callers and callees as rows, with their operations, and no utility', () => {
  const view = {
    components: [
      comp('client', 'WebClient', 'client'),
      ...VIEW.components,
      comp('state-access', 'StateAccess', 'resourceaccess'),
    ],
    relationships: [
      { from: 'client', to: 'construction-manager', label: 'Supervise', mode: 'sync' },
      { from: 'client', to: 'construction-manager', label: 'Begin', mode: 'sync' },
      ...VIEW.relationships,
      { from: 'construction-manager', to: 'review-engine', label: 'Route', mode: 'sync' },
      { from: 'construction-manager', to: 'state-access', label: '', mode: 'sync' },
    ],
  } as unknown as C4View;
  const { callers, callees } = neighbourRowsFor(view, 'construction-manager');
  assert.deepEqual(callers, [
    { id: 'client', name: 'WebClient', layer: 'client', operations: ['Supervise', 'Begin'] },
  ]);
  assert.deepEqual(
    callees.map((r) => [r.id, r.operations]),
    [
      ['review-engine', ['Propose', 'Route']],
      ['state-access', []],
    ]
  );
  // An engine's only caller is its manager; its utility (Security) is no row.
  const engine = neighbourRowsFor(view, 'review-engine');
  assert.deepEqual(
    engine.callers.map((r) => r.id),
    ['construction-manager']
  );
  assert.deepEqual(engine.callees, []);
});

void test('a relationship label reads as its calls: the op name, then (…)', () => {
  assert.equal(
    shortCallLabel('EvaluateDesignHealth(project, systemModel) → findings'),
    'EvaluateDesignHealth(…)'
  );
  assert.equal(
    shortCallLabel('pauseProject | overrideActivity'),
    'pauseProject · overrideActivity'
  );
  assert.equal(
    shortCallLabel(
      'getInstallationToken(repo) → RepoCredential | openBranch(repo, sessionBranch, cred) → BranchRef'
    ),
    'getInstallationToken(…) · openBranch(…)'
  );
  // Alternatives split at the top level only: the slash inside the parens stays.
  assert.equal(
    shortCallLabel(
      'ComputeCharge(cycleUsage, terms) → {chargeAmount} / RecomputeCharge (dispute/refund correction)'
    ),
    'ComputeCharge(…) · RecomputeCharge(…)'
  );
  // Prose is kept as written; repeats are said once.
  assert.deepEqual(callsOf('readProject / stage·commit typed model / readProject'), [
    'readProject',
    'stage·commit typed model',
  ]);
  assert.equal(shortCallLabel(''), '');
});

void test('an edge label is cut at the limit with an ellipsis; the rows say all of it', () => {
  const long = 'readProject / stage·commit·reject·withdraw typed artifact model / advancePhase';
  const edge = edgeCallLabel(long);
  assert.equal(edge.length, EDGE_LABEL_MAX);
  assert.ok(edge.endsWith('…'));
  assert.equal(
    edgeCallLabel('EvaluateDesignHealth(project) → findings'),
    'EvaluateDesignHealth(…)'
  );
});

void test('a focal utility keeps itself', () => {
  const v = withoutUtilities(VIEW, 'logging');
  assert.ok(v.components.some((c) => c.id === 'logging'));
  assert.ok(!v.components.some((c) => c.id === 'diagnostics'));
});
