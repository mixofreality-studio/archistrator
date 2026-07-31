/// <reference types="node" />
/**
 * Unit tests for the pure per-use-case realization join (src/contracts/realization.ts):
 *
 *  - realizationByNode indexes one use case's realized DynamicView steps by
 *    activityNodeId (no graph walk — that DFS linearization is adapters.ts'
 *    toDynamicView, tested separately).
 *  - personParticipants filters a use case's actors down to the ones that show
 *    up as a call endpoint in its realized view, in ACTOR-list order.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import type { System, UseCase } from './types';
import { realizationByNode, personParticipants } from './realization.ts';

const SYSTEM: System = {
  components: null,
  relationships: null,
  dynamicViews: [
    {
      key: 'dv-order',
      title: 'Place Order',
      useCaseId: 'uc-1',
      steps: [
        {
          activityNodeId: 'n-validate',
          calls: [{ from: 'webClient', to: 'orderManager', mode: 'sync', label: 'submit order' }],
        },
        {
          activityNodeId: 'n-charge',
          calls: [
            { from: 'orderManager', to: 'billingEngine', mode: 'sync', label: 'charge card' },
            {
              from: 'billingEngine',
              to: 'customer',
              mode: 'eventPubSub',
              label: 'notify customer',
            },
          ],
        },
        { activityNodeId: 'n-empty', calls: [] },
      ],
    },
    // A view with a blank key and null steps — realizationByNode must tolerate
    // null steps, and useCaseId 'uc-2' has no calls to derive persons from.
    { key: '', title: 'Broken', useCaseId: 'uc-2', steps: null },
  ],
};

function useCase(id: string, actors: { id: string; role: string }[]): UseCase {
  return {
    id,
    name: 'Place Order',
    actors,
    activity: null,
    classification: 'core',
    trigger: 'clientAction',
    variationOf: null,
  };
}

// ── realizationByNode ────────────────────────────────────────────────────────

void test('indexes each step by its activityNodeId, mapping Relationship -> RealizedCall', () => {
  const map = realizationByNode(SYSTEM, 'uc-1');
  assert.deepEqual([...map.keys()], ['n-validate', 'n-charge', 'n-empty']);
  assert.deepEqual(map.get('n-validate')?.calls, [
    { from: 'webClient', to: 'orderManager', mode: 'sync', label: 'submit order' },
  ]);
  assert.equal(map.get('n-charge')?.calls.length, 2);
  assert.equal(map.get('n-charge')?.nodeId, 'n-charge');
  assert.deepEqual(map.get('n-empty')?.calls, []);
});

void test('a use case with a null-steps view yields an empty map', () => {
  assert.equal(realizationByNode(SYSTEM, 'uc-2').size, 0);
});

void test('an unlinked use case id yields an empty map', () => {
  assert.equal(realizationByNode(SYSTEM, 'uc-none').size, 0);
});

void test('an absent system or a blank use case id yields an empty map', () => {
  assert.equal(realizationByNode(undefined, 'uc-1').size, 0);
  assert.equal(realizationByNode(SYSTEM, '  ').size, 0);
});

// ── personParticipants ───────────────────────────────────────────────────────

void test('keeps only actors that appear as a call endpoint, in actor-list order', () => {
  const uc = useCase('uc-1', [
    { id: 'auditor', role: 'Auditor' },
    { id: 'customer', role: 'Customer' },
  ]);
  assert.deepEqual(personParticipants(SYSTEM, uc), [{ id: 'customer', role: 'Customer' }]);
});

void test('an actor appearing as a `from` endpoint also counts', () => {
  const uc = useCase('uc-1', [{ id: 'webClient', role: 'Shopper' }]);
  assert.deepEqual(personParticipants(SYSTEM, uc), [{ id: 'webClient', role: 'Shopper' }]);
});

void test('no actors reach an endpoint -> empty', () => {
  const uc = useCase('uc-1', [{ id: 'auditor', role: 'Auditor' }]);
  assert.deepEqual(personParticipants(SYSTEM, uc), []);
});

void test('a use case with no linked dynamic view -> empty', () => {
  const uc = useCase('uc-none', [{ id: 'webClient', role: 'Shopper' }]);
  assert.deepEqual(personParticipants(SYSTEM, uc), []);
});

void test('an absent system or use case -> empty', () => {
  assert.deepEqual(personParticipants(undefined, useCase('uc-1', [])), []);
  assert.deepEqual(personParticipants(SYSTEM, undefined), []);
});
