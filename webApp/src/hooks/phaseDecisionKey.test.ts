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

import { phaseDecisionMutationKey } from './phaseDecisionKey.ts';

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
