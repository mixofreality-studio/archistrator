/**
 * phaseDecisionKey — the phase-decision mutation key is PER PROJECT (tasks round 2
 * minor). The mutation cache is shared by every project the app has open; a real
 * QueryClient holds project A's decision on the wire while project B's console asks
 * what is in flight for it, the way the route does (useMutationState and the
 * one-click guard's isMutating, both filtered by this key).
 */
/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { QueryClient } from '@tanstack/react-query';

import { phaseDecisionFilters, phaseDecisionMutationKey } from './phaseDecisionKey.ts';

void test("project A's decision on the wire never reads as in flight for project B", async () => {
  const client = new QueryClient();
  const mutation = client.getMutationCache().build(client, {
    mutationKey: phaseDecisionMutationKey('A'),
    // Held on the wire for the whole test.
    mutationFn: () => new Promise<never>(() => undefined),
    gcTime: Infinity,
  });
  // The same activity id in both projects: only the project tells them apart.
  void mutation.execute({ activityId: 'N-STP', phase: 'detailed_design', decision: 'approve' });
  await new Promise((r) => setTimeout(r, 0));

  const inFlight = (projectId: string): number =>
    client.isMutating({ mutationKey: phaseDecisionMutationKey(projectId) });
  assert.equal(inFlight('A'), 1);
  assert.equal(inFlight('B'), 0);
  // A key prefix is not a match either.
  assert.equal(inFlight('A-2'), 0);
  const seenByB = client
    .getMutationCache()
    .findAll({ mutationKey: phaseDecisionMutationKey('B'), status: 'pending' });
  assert.equal(seenByB.length, 0);
  client.clear();
});

// Tasks round-2 review, minor: the route's cache read and its one-click guard each
// built the filter themselves. Both now pass phaseDecisionFilters; pinned here the
// way each uses it — findAll (what useMutationState runs) and isMutating with the
// per-activity predicate (the guard).
void test('phaseDecisionFilters: the cache read and the guard see only their own project', async () => {
  const client = new QueryClient();
  const held = (projectId: string, activityId: string): void => {
    const mutation = client.getMutationCache().build(client, {
      mutationKey: phaseDecisionMutationKey(projectId),
      mutationFn: () => new Promise<never>(() => undefined),
      gcTime: Infinity,
    });
    void mutation.execute({ activityId, phase: 'detailed_design', decision: 'approve' });
  };
  held('A', 'N-STP');
  held('A-2', 'N-STP');
  await new Promise((r) => setTimeout(r, 0));

  assert.deepEqual(phaseDecisionFilters('B'), {
    mutationKey: ['submitPhaseDecision', 'B'],
    exact: true,
  });
  // The cache read (useMutationState = findAll(filters)).
  assert.equal(client.getMutationCache().findAll(phaseDecisionFilters('A')).length, 1);
  assert.equal(client.getMutationCache().findAll(phaseDecisionFilters('B')).length, 0);
  // The one-click guard (isMutating with the activity predicate).
  const onTheWire = (projectId: string, activityId: string): number =>
    client.isMutating({
      ...phaseDecisionFilters(projectId),
      predicate: (m) => (m.state.variables as { activityId?: string }).activityId === activityId,
    });
  assert.equal(onTheWire('A', 'N-STP'), 1);
  assert.equal(onTheWire('B', 'N-STP'), 0);
  assert.equal(onTheWire('A', 'N-IT'), 0);
  // The project-less key matches every project — what the filter exists to prevent.
  assert.equal(client.isMutating({ mutationKey: ['submitPhaseDecision'] }), 2);
  client.clear();
});
