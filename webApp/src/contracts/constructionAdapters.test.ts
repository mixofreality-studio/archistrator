/**
 * computeActivityStatuses' milestone dependency resolution — mirrors the server's
 * resolveDependencySatisfied (constructionmanager.go:982-1016).
 *
 * A milestone id can appear inside an activity's dependsOn (network.milestones[]
 * carries its own dependsOn, recursively resolved). Before this fix the universe
 * builder only looked at dependencies rows, so a milestone id became a phantom
 * "activity" that was never in the done set — its dependents read BLOCKED forever
 * even when every real predecessor had landed. These pin the fix: a milestone
 * satisfied by its own (Done) dependsOn makes its dependent 'eligible', and an
 * authored milestone cycle resolves to "not satisfied" (dependent stays 'blocked')
 * without looping forever.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { computeActivityStatuses } from './constructionAdapters.ts';
import type { NetworkModel } from './types.ts';

void test("an activity gated on a milestone is eligible once the milestone's own dependsOn are all done", () => {
  const network: NetworkModel = {
    criticalPath: [],
    dependencies: [
      { activity: 'A-01', dependsOn: null },
      { activity: 'A-02', dependsOn: null },
      { activity: 'X-01', dependsOn: ['M3'] },
    ],
    milestones: [{ id: 'M3', name: 'Gate 3', public: true, dependsOn: ['A-01', 'A-02'] }],
  };

  const statuses = computeActivityStatuses(
    network,
    (id) => ({ merged: id === 'A-01' || id === 'A-02' }),
    undefined,
    'not-started'
  );

  assert.equal(statuses.get('X-01'), 'eligible');
  // The milestone id itself is not a constructable activity — it must not show up
  // as a phantom entry in the status map.
  assert.equal(statuses.has('M3'), false);
});

void test("an activity gated on a milestone stays blocked while any of the milestone's own dependsOn are undone", () => {
  const network: NetworkModel = {
    criticalPath: [],
    dependencies: [
      { activity: 'A-01', dependsOn: null },
      { activity: 'A-02', dependsOn: null },
      { activity: 'X-01', dependsOn: ['M3'] },
    ],
    milestones: [{ id: 'M3', name: 'Gate 3', public: true, dependsOn: ['A-01', 'A-02'] }],
  };

  const statuses = computeActivityStatuses(
    network,
    (id) => ({ merged: id === 'A-01' }),
    undefined,
    'not-started'
  );

  assert.equal(statuses.get('X-01'), 'blocked');
});

void test('a milestone dependency cycle resolves to not-satisfied without infinite recursion, dependent stays blocked', () => {
  const network: NetworkModel = {
    criticalPath: [],
    dependencies: [{ activity: 'X-01', dependsOn: ['M1'] }],
    // M1 depends on M2 and M2 depends on M1 — an authored cycle in the network.
    milestones: [
      { id: 'M1', name: 'Gate 1', public: true, dependsOn: ['M2'] },
      { id: 'M2', name: 'Gate 2', public: true, dependsOn: ['M1'] },
    ],
  };

  const statuses = computeActivityStatuses(
    network,
    () => ({ merged: false }),
    undefined,
    'not-started'
  );

  assert.equal(statuses.get('X-01'), 'blocked');
});

void test('a milestone with no dependsOn (the project-start gate) is satisfied', () => {
  const network: NetworkModel = {
    criticalPath: [],
    dependencies: [{ activity: 'X-01', dependsOn: ['M0'] }],
    milestones: [{ id: 'M0', name: 'Start', public: true, dependsOn: null }],
  };

  const statuses = computeActivityStatuses(
    network,
    () => ({ merged: false }),
    undefined,
    'not-started'
  );

  assert.equal(statuses.get('X-01'), 'eligible');
});
