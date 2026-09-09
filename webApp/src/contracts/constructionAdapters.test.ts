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
import { mapConstructionRow } from './wire.ts';
import type { components } from './schema.ts';

type WireConstructionStatus = components['schemas']['SystemDesignActivityConstructionStatus'];

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

// ---------------------------------------------------------------------------
// mapConstructionRow — reading the Phases array + attempt ledger the server
// already emits (Task 9). Field casing on WireConstructionStatus is a mix of
// PascalCase (older fields, default Go json encoding) and lowerCamelCase
// (attempts/classified/worstOrigin/layer/layerBand/completedAt — explicit json
// tags added later): see server/internal/manager/systemdesign/contract.gen.go.
// ---------------------------------------------------------------------------

function wireRow(over: Partial<WireConstructionStatus>): WireConstructionStatus {
  return {
    ActivityID: 'C-x',
    BuildStatus: 0,
    CurrentPhase: 'construction',
    FailureDetail: '',
    FailureReason: 0,
    Kind: 0,
    Phase: 0,
    Phases: null,
    Produced: null,
    Type: 0,
    Variant: 0,
    classified: true,
    worstOrigin: 'observed',
    layer: '',
    layerBand: '',
    ...over,
  };
}

void test('mapConstructionRow reads the Phases array the server already emits', () => {
  const row = mapConstructionRow(
    wireRow({
      Phases: [
        {
          Phase: 'requirements',
          Weight: 15,
          Label: 'UX Requirements',
          Completed: true,
          ArtifactRef: '',
        },
      ],
    })
  );
  assert.equal(row.phases.length, 1);
  const phase = row.phases[0];
  assert.ok(phase !== undefined, 'expected one mapped phase');
  assert.equal(phase.label, 'UX Requirements');
  assert.equal(phase.completed, true);
});

void test('mapConstructionRow reads the attempt ledger with its join key', () => {
  const row = mapConstructionRow(
    wireRow({
      attempts: [
        {
          attemptId: 'C-x:designReview:2',
          task: 'designReview',
          phase: 'detailed_design',
          attempt: 2,
          outcome: 'rejected',
          evidence: { kind: 'contract', ref: 'billingStateAccess' },
          provenance: { origin: 'observed' },
        },
      ],
    })
  );
  assert.equal(row.attempts.length, 1);
  const attempt = row.attempts[0];
  assert.ok(attempt !== undefined, 'expected one mapped attempt');
  assert.equal(attempt.attemptId, 'C-x:designReview:2');
  assert.equal(attempt.evidence.kind, 'contract');
});

// The zero value must survive the boundary as "synthesized", never as observed —
// that is the load-bearing invariant of this whole stage.
void test('mapConstructionRow treats an absent provenance origin as synthesized', () => {
  const row = mapConstructionRow(
    wireRow({
      attempts: [
        {
          attemptId: 'C-x:srs:1',
          task: 'srs',
          phase: 'requirements',
          attempt: 1,
          outcome: '',
          evidence: { kind: '', ref: '' },
          provenance: { origin: '' },
        },
      ],
      worstOrigin: '',
    })
  );
  assert.equal(row.attempts[0]?.provenance.origin, 'synthesized');
  assert.equal(row.worstOrigin, 'synthesized');
});

// The invariant is not just "absent maps to the safe member" — it is "anything
// UNRECOGNIZED does". mapOrigin, mapOutcome, and mapEvidenceKind are all total
// functions over the input space; an unknown non-empty string on any of the
// three must degrade the same way a dropped/absent value does, never surface
// as a plausible real value (an unknown outcome is not 'passed'; an unknown
// evidence kind must not claim to point at an episode or a contract).
void test('mapConstructionRow degrades an unrecognized origin, outcome, and evidence kind to their safe member', () => {
  const row = mapConstructionRow(
    wireRow({
      attempts: [
        {
          attemptId: 'C-x:srs:1',
          task: 'srs',
          phase: 'requirements',
          attempt: 1,
          outcome: 'bogus',
          evidence: { kind: 'bogus', ref: '' },
          provenance: { origin: 'bogus' },
        },
      ],
      worstOrigin: 'bogus',
    })
  );
  const attempt = row.attempts[0];
  assert.ok(attempt !== undefined, 'expected one mapped attempt');
  assert.equal(attempt.provenance.origin, 'synthesized');
  assert.equal(attempt.outcome, '');
  assert.equal(attempt.evidence.kind, '');
  assert.equal(row.worstOrigin, 'synthesized');
});

void test('mapConstructionRow treats an absent classified flag as unclassified', () => {
  const row = mapConstructionRow(wireRow({ classified: false }));
  assert.equal(row.classified, false);
});

// Item 2: the server leaves Type/Kind/Variant at their zero value on an
// unclassified row (Type: 0 decodes to 'service' when NOT gated on classified —
// this pins that gate). Rendering that zero as a real kind would fabricate a
// lifecycle for ~60 of 69 committed activities.
void test('an unclassified row does not surface a plausible kind even though Type sits at its zero value', () => {
  const row = mapConstructionRow(wireRow({ classified: false, Type: 0, Kind: 0 }));
  assert.equal(row.classified, false);
  assert.equal(row.kind, undefined);
});
