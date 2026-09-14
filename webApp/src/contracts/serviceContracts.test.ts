/**
 * The activity → contract join (architect ruling §3.1): componentId → the
 * component's EXPLICIT contractKey → serviceContracts. Pinned per kind against
 * the shapes the dogfood project actually commits (slot 9 + slot 5 +
 * ServiceContracts at 662328f4): service rows resolve, the SPA resolves to its
 * client contract, design-health-engine is a real gap, the R-* rows are none by
 * design, and N-* rows build no component.
 */
/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import type { C4Component } from './adapters.ts';
import type { ServiceContract } from './types.ts';
import {
  activityForComponent,
  contractJoinFor,
  type ContractJoinInput,
} from './serviceContracts.ts';

function comp(id: string, kind: C4Component['kind'], contractKey = ''): C4Component {
  return {
    id,
    name: id,
    kind,
    layer: kind,
    encapsulates: '',
    encapsulatesVolatilities: [],
    contractKey,
  };
}

function contract(component: string, layer: string): ServiceContract {
  return { component, layer, ops: [{ signature: 'Op()', stereotype: 'query' }] };
}

const input: ContractJoinInput = {
  activities: [
    { name: 'C-construction-manager', componentId: 'construction-manager' },
    { name: 'C-review-engine', componentId: 'review-engine' },
    { name: 'C-design-health-engine', componentId: 'design-health-engine' },
    { name: 'C-broken-key', componentId: 'broken-key-access' },
    { name: 'R-github', componentId: 'github' },
    { name: 'R-logging', componentId: 'logging' },
    { name: 'U-SPA-web-client', componentId: 'web-client' },
    { name: 'N-STP' },
    { name: 'N-IT', componentId: '' },
    { name: 'C-ghost', componentId: 'no-such-component' },
  ],
  components: [
    comp('construction-manager', 'manager', 'constructionManager'),
    comp('review-engine', 'engine', 'reviewEngine'),
    // The one engine with no contractKey and no contract (T7).
    comp('design-health-engine', 'engine'),
    comp('broken-key-access', 'resourceAccess', 'brokenKeyAccess'),
    comp('github', 'resource'),
    comp('logging', 'utility'),
    comp('web-client', 'client', 'webClient'),
  ],
  contracts: {
    constructionManager: contract('constructionManager', 'Manager'),
    reviewEngine: contract('reviewEngine', 'Engine'),
    webClient: contract('webClient', 'Client'),
  },
};

void test('a service row resolves through its component contractKey', () => {
  const join = contractJoinFor(input, 'C-construction-manager');
  assert.equal(join.kind, 'contract');
  assert.equal(join.contractKey, 'constructionManager');
  assert.equal(join.componentId, 'construction-manager');
  assert.equal(join.contract.component, 'constructionManager');
  assert.equal(contractJoinFor(input, 'C-review-engine').kind, 'contract');
});

void test('the SPA resolves to its client contract', () => {
  const join = contractJoinFor(input, 'U-SPA-web-client');
  assert.equal(join.kind, 'contract');
  assert.equal(join.contractKey, 'webClient');
});

void test('an engine with no contractKey is a real gap, not a design choice', () => {
  const join = contractJoinFor(input, 'C-design-health-engine');
  assert.deepEqual(
    { kind: join.kind, key: join.kind === 'missing' ? join.contractKey : 'x' },
    { kind: 'missing', key: undefined }
  );
});

void test('a contractKey naming no contract is also a real gap, and names the key', () => {
  const join = contractJoinFor(input, 'C-broken-key');
  assert.equal(join.kind, 'missing');
  assert.equal(join.contractKey, 'brokenKeyAccess');
});

void test('resources and utilities carry no contract by design', () => {
  assert.equal(contractJoinFor(input, 'R-github').kind, 'byDesign');
  assert.equal(contractJoinFor(input, 'R-logging').kind, 'byDesign');
});

void test('N-* rows build no component', () => {
  assert.equal(contractJoinFor(input, 'N-STP').kind, 'noComponent');
  assert.equal(contractJoinFor(input, 'N-IT').kind, 'noComponent');
});

void test('the join never falls back to a name heuristic', () => {
  // A component whose kebab id converts to an existing contract key, but which
  // names no contractKey itself, must NOT pick that contract up.
  const trap: ContractJoinInput = {
    ...input,
    activities: [{ name: 'C-review-engine', componentId: 'review-engine' }],
    components: [comp('review-engine', 'engine')],
  };
  assert.equal(contractJoinFor(trap, 'C-review-engine').kind, 'missing');
});

void test('unloaded or unplaced data is unresolved, never a claim about the contract', () => {
  assert.deepEqual(contractJoinFor({ ...input, activities: undefined }, 'C-construction-manager'), {
    kind: 'unresolved',
    reason: 'noActivityList',
  });
  assert.deepEqual(contractJoinFor(input, 'C-unknown'), {
    kind: 'unresolved',
    reason: 'notInActivityList',
  });
  assert.deepEqual(contractJoinFor(input, 'C-ghost'), {
    kind: 'unresolved',
    reason: 'noSuchComponent',
  });
});

void test('activityForComponent is the first hop inverted', () => {
  assert.equal(activityForComponent(input.activities, 'review-engine'), 'C-review-engine');
  assert.equal(activityForComponent(input.activities, 'nobody'), undefined);
  assert.equal(activityForComponent(undefined, 'review-engine'), undefined);
});
