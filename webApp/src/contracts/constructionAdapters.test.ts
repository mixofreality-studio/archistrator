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
import { computeActivityStatuses, buildStatusForConstructionRow } from './constructionAdapters.ts';
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
    // The default fixture is a classified row the server DID resolve completions for;
    // the no-evidence case is opted into per-test by overriding this to false.
    hasBuildEvidence: true,
    recorded: true,
    worstOrigin: 'observed',
    layer: '',
    layerBand: '',
    ...over,
  };
}

// `recorded` is the server's one explicit "a stored row backs this" signal. A
// planned-no-record row arrives recorded:false with worstOrigin OMITTED on the wire;
// the mapper passes the flag through and never invents an origin for it.
void test('mapConstructionRow carries recorded and never invents an origin for an unrecorded row', () => {
  const plannedWire = wireRow({ recorded: false, hasBuildEvidence: false });
  // The server OMITS the key on an unrecorded row (not `undefined`, not '').
  delete plannedWire.worstOrigin;
  const planned = mapConstructionRow(plannedWire);
  assert.equal(planned.recorded, false);
  assert.equal('worstOrigin' in planned, false);

  const stored = mapConstructionRow(
    wireRow({
      recorded: true,
      worstOrigin: 'backfilled',
      attempts: [
        {
          attemptId: 'C-x:codeReview:1',
          task: 'codeReview',
          phase: 'construction',
          attempt: 1,
          actor: 'agent',
          outcome: 'passed',
          evidence: { kind: 'none', ref: '' },
          provenance: { origin: 'backfilled', generator: 'g', generatedAt: null, basis: 'b' },
        },
      ],
    })
  );
  assert.equal(stored.recorded, true);
  assert.equal(stored.worstOrigin, 'backfilled');
});

// Stage C: startedAt/completedAt are the pump's own record that it dispatched the
// activity and finished it — the TASKS lens probes a session only in between. The
// wire sends null (or omits the key) when the pump never wrote one; that is absence.
void test('mapConstructionRow carries the pump start/complete stamps and drops nulls', () => {
  const live = mapConstructionRow(
    wireRow({ startedAt: '2026-09-12T10:00:00Z', completedAt: null })
  );
  assert.equal(live.startedAt, '2026-09-12T10:00:00Z');
  assert.equal('completedAt' in live, false);

  const done = mapConstructionRow(
    wireRow({ startedAt: '2026-09-12T10:00:00Z', completedAt: '2026-09-12T12:00:00Z' })
  );
  assert.equal(done.completedAt, '2026-09-12T12:00:00Z');

  const never = mapConstructionRow(wireRow({}));
  assert.equal('startedAt' in never, false);
  assert.equal('completedAt' in never, false);
});

// `recorded` and `hasBuildEvidence` are DIFFERENT facts (fix-A review M1): a stored
// row with no resolved completions is recorded yet carries no build evidence. A
// mapper that read one off the other would pass every case above.
void test('mapConstructionRow keeps recorded apart from hasBuildEvidence', () => {
  const storedNoEvidence = mapConstructionRow(wireRow({ recorded: true, hasBuildEvidence: false }));
  assert.equal(storedNoEvidence.recorded, true);
  assert.equal(storedNoEvidence.hasBuildEvidence, false);
  const plannedWire = wireRow({ recorded: false, hasBuildEvidence: true });
  delete plannedWire.worstOrigin;
  const planned = mapConstructionRow(plannedWire);
  assert.equal(planned.recorded, false);
  assert.equal(planned.hasBuildEvidence, true);
});

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

// layer/layerBand (stage A task 11, read here in stage B task 2 — this mapper
// predated task 11 so it never picked the fields up). "" is LayerForActivity's own
// real return for a project-wide row, so it must be dropped rather than surfaced as
// a fabricated Layer value — same discipline as kind/status/worstOrigin above.
void test('mapConstructionRow carries the layer projection through to the row', () => {
  const row = mapConstructionRow(wireRow({ layer: 'client', layerBand: 'layered' }));
  assert.equal(row.layer, 'client');
  assert.equal(row.layerBand, 'layered');
});

void test('mapConstructionRow omits layer/layerBand when the server drew no layer for the row', () => {
  const row = mapConstructionRow(wireRow({ layer: '', layerBand: 'projectWide' }));
  assert.equal(row.layer, undefined);
  assert.equal(row.layerBand, 'projectWide');
  assert.equal(
    Object.prototype.hasOwnProperty.call(row, 'layer'),
    false,
    'the key must be OMITTED, not set to undefined (exactOptionalPropertyTypes)'
  );
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

// worstOrigin is an aggregate over the ledger. The server's roll-up seeds an EMPTY
// ledger to 'observed' — correct as an aggregate (nothing was derived from anything
// unknown) and a trap at row level, where it reads as "recorded" for a row about which
// nothing is known. Every row nothing has been attempted on has an empty ledger, so the
// stamp is dropped at this boundary, exactly as kind/status/currentLifecyclePhase are
// dropped on an unclassified row. The server-side seed is deliberately NOT changed to
// 'synthesized': that would tar those rows as fabricated.
void test('mapConstructionRow omits worstOrigin when the ledger is empty', () => {
  const row = mapConstructionRow(wireRow({ attempts: [], worstOrigin: 'observed' }));
  assert.equal(row.attempts.length, 0);
  assert.equal(row.worstOrigin, undefined);
  assert.equal(
    Object.prototype.hasOwnProperty.call(row, 'worstOrigin'),
    false,
    'the key must be OMITTED, not set to undefined (exactOptionalPropertyTypes)'
  );
});

void test('mapConstructionRow keeps worstOrigin when the ledger is non-empty', () => {
  const row = mapConstructionRow(
    wireRow({
      attempts: [
        {
          attemptId: 'C-x:srs:1',
          task: 'srs',
          phase: 'requirements',
          attempt: 1,
          outcome: 'passed',
          evidence: { kind: '', ref: '' },
          provenance: { origin: 'observed' },
        },
      ],
      worstOrigin: 'observed',
    })
  );
  assert.equal(row.worstOrigin, 'observed');
});

// Stage B task 1: twenty CLASSIFIED rows have neither stored phases nor an attempt
// ledger. They resolve to nil completions, so the server's CoarseBuildStatusFor returns
// its zero value BuildInConstruction and the row arrived asserting "In construction"
// about work that has not begun. The flag — not `classified`, which is TRUE on these
// rows — is what the gate reads.
void test('does not surface a status for a classified row with no evidence', () => {
  const row = mapConstructionRow(wireRow({ classified: true, hasBuildEvidence: false }));
  assert.equal(row.classified, true);
  assert.equal(row.hasBuildEvidence, false);
  assert.equal(Object.prototype.hasOwnProperty.call(row, 'status'), false);
});

void test('still surfaces a status for a classified row WITH evidence', () => {
  const row = mapConstructionRow(
    wireRow({
      classified: true,
      hasBuildEvidence: true,
      BuildStatus: 1,
      Phases: [
        {
          Phase: 'construction',
          Weight: 40,
          Label: 'Construction',
          Completed: true,
          ArtifactRef: '',
        },
      ],
    })
  );
  assert.notEqual(row.status, undefined);
});

// A dropped flag must decode as NO evidence — the safe direction, the same rule
// `classified` follows. The field is `required` in the schema so this should not
// happen; the point is that if it ever does, the failure lands on the safe side.
void test('an absent hasBuildEvidence flag reads as no evidence', () => {
  const row = mapConstructionRow(
    wireRow({ classified: true, hasBuildEvidence: undefined as unknown as boolean })
  );
  assert.ok(!row.hasBuildEvidence);
  assert.equal(row.status, undefined);
});

void test('mapConstructionRow treats an absent classified flag as unclassified', () => {
  const row = mapConstructionRow(wireRow({ classified: false }));
  assert.equal(row.classified, false);
});

// Item 2: the server leaves Type/Kind/Variant at their zero value on an
// unclassified row (Type: 0 decodes to 'service' when NOT gated on classified —
// this pins that gate). Rendering that zero as a real kind would fabricate a
// lifecycle for a row the server could not type.
void test('an unclassified row does not surface a plausible kind even though Type sits at its zero value', () => {
  const row = mapConstructionRow(wireRow({ classified: false, Type: 0, Kind: 0 }));
  assert.equal(row.classified, false);
  assert.equal(row.kind, undefined);
});

// Task 13 item 2: ActivityBuildStatus(0) is the real, named state
// BuildInConstruction and is NOT serialized with omitempty, so an unclassified
// row's wire BuildStatus is indistinguishable from a genuinely-started build
// unless status is gated on classified the same way kind is. Pins that gate.
void test('an unclassified row does not surface a plausible status even though BuildStatus sits at its zero value', () => {
  const row = mapConstructionRow(wireRow({ classified: false, BuildStatus: 0 }));
  assert.equal(row.classified, false);
  assert.equal(row.status, undefined);
});

// Same failure shape for currentLifecyclePhase — the wireRow fixture's default
// CurrentPhase is the real string 'construction', so this pins that an
// unclassified row does not surface it even though the wire value is present.
void test('an unclassified row does not surface a plausible current lifecycle phase', () => {
  const row = mapConstructionRow(wireRow({ classified: false }));
  assert.equal(row.classified, false);
  assert.equal(row.currentLifecyclePhase, undefined);
});

// A classified row is the mirror case: status/currentLifecyclePhase MUST
// survive the boundary — the gate must not swallow real data either.
void test('a classified row still surfaces its real status and current lifecycle phase', () => {
  const row = mapConstructionRow(
    wireRow({ classified: true, BuildStatus: 1, CurrentPhase: 'test_plan' })
  );
  assert.equal(row.status, 'in-review');
  assert.equal(row.currentLifecyclePhase, 'test_plan');
});

// Stage B task 6: `CurrentPhase` is a plain string with no omitempty, so a
// CLASSIFIED row the server has not started reporting a phase for arrives as
// '' on every row nothing has started on. Gating on `classified` alone let that
// through as a present field naming a phase called nothing; the field now drops
// at its zero value exactly like kind/status/worstOrigin/layer.
void test('a classified row with no current phase yet drops the field rather than surfacing an empty one', () => {
  const row = mapConstructionRow(wireRow({ classified: true, CurrentPhase: '' }));
  assert.equal(row.classified, true);
  assert.equal(row.currentLifecyclePhase, undefined);
});

// buildStatusForConstructionRow must not fold "we don't know" (status
// undefined) into "we know it hasn't started" (not-started) — the two are
// different claims, and the honest-fallback member for the first is
// 'unclassified', which is also what feeds the tracker's head-state rollup
// and node coloring (computeActivityStatuses below).
void test('buildStatusForConstructionRow reports unclassified, never not-started, for a row with no status', () => {
  const row = mapConstructionRow(wireRow({ classified: false }));
  assert.equal(buildStatusForConstructionRow(row), 'unclassified');
});

// A classified row with no build evidence asserts nothing, so it must NOT
// short-circuit the network-derived readiness pass. Before this gate its wire
// zero decoded as 'in-construction' and twenty activities never reached the
// eligible/blocked branch below — the console showed builds in progress for work
// that had not begun and hid the work that was actually ready to start.
void test('a classified row with no evidence falls through to network-derived readiness', () => {
  const network: NetworkModel = {
    criticalPath: [],
    dependencies: [
      { activity: 'A-01', dependsOn: null },
      { activity: 'B-01', dependsOn: ['A-01'] },
      { activity: 'C-01', dependsOn: ['B-01'] },
    ],
    milestones: [],
  };
  const rows: Record<string, ReturnType<typeof mapConstructionRow>> = {
    'B-01': mapConstructionRow(wireRow({ ActivityID: 'B-01', hasBuildEvidence: false })),
    'C-01': mapConstructionRow(wireRow({ ActivityID: 'C-01', hasBuildEvidence: false })),
  };

  const statuses = computeActivityStatuses(
    network,
    (id) => ({ merged: id === 'A-01' }),
    undefined,
    'not-started',
    (id) => rows[id]
  );

  // A-01 is merged ⇒ integrated; B-01's only predecessor is done ⇒ eligible;
  // C-01 waits on the un-done B-01 ⇒ blocked. None of the three reads
  // 'in-construction'.
  assert.equal(statuses.get('B-01'), 'eligible');
  assert.equal(statuses.get('C-01'), 'blocked');
});

// The mirror case: an UNCLASSIFIED row still short-circuits on 'unclassified' —
// there the row IS the answer, and falling through would claim a readiness the
// server has no basis to compute (it does not know what the activity is).
void test('an unclassified row still short-circuits network-derived readiness', () => {
  const network: NetworkModel = {
    criticalPath: [],
    dependencies: [{ activity: 'B-01', dependsOn: null }],
    milestones: [],
  };
  const rows: Record<string, ReturnType<typeof mapConstructionRow>> = {
    'B-01': mapConstructionRow(wireRow({ ActivityID: 'B-01', classified: false })),
  };

  const statuses = computeActivityStatuses(
    network,
    () => ({ merged: false }),
    undefined,
    'not-started',
    (id) => rows[id]
  );

  assert.equal(statuses.get('B-01'), 'unclassified');
});

// And a classified row WITH evidence still wins over the network, as it always did.
void test('a classified row with evidence still overrides network-derived readiness', () => {
  const network: NetworkModel = {
    criticalPath: [],
    dependencies: [{ activity: 'B-01', dependsOn: null }],
    milestones: [],
  };
  const rows: Record<string, ReturnType<typeof mapConstructionRow>> = {
    'B-01': mapConstructionRow(
      wireRow({ ActivityID: 'B-01', hasBuildEvidence: true, BuildStatus: 1 })
    ),
  };

  const statuses = computeActivityStatuses(
    network,
    () => ({ merged: false }),
    undefined,
    'not-started',
    (id) => rows[id]
  );

  assert.equal(statuses.get('B-01'), 'in-review');
});
