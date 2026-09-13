/**
 * The gate ribbon (gateRibbon.ts) — the network's milestones, lifted off the
 * canvas into a strip across the top (spec §7.6).
 *
 * The one rule with teeth is §9.2's: a completion count over reconstructed or
 * unrecorded evidence is a LAUNDERED AGGREGATE, so `complete` exists only when
 * every feeder's evidence was observed — never as a badged number.
 */
/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import type {
  ConstructionRow,
  PhaseRow,
  RecordOriginRow,
  TaskAttemptRow,
} from '../../../contracts/types.ts';
import { buildActivityTree, type ActivityNode } from '../list/activityTree.ts';
import { gateRibbonFor, type RibbonMilestone } from './gateRibbon.ts';

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

const SERVICE_PHASES = [
  'requirements',
  'detailed_design',
  'test_plan',
  'construction',
  'integration',
] as const;

function attempt(origin: RecordOriginRow): TaskAttemptRow {
  return {
    attemptId: 'a1',
    task: 'codeReview',
    phase: 'construction',
    attempt: 1,
    outcome: 'passed',
    evidence: { kind: '', ref: '' },
    provenance: { origin },
  };
}

/** A service activity with every phase reported, done or not, from one origin. */
function recorded(activityId: string, done: boolean, origin: RecordOriginRow): ConstructionRow {
  const phases: PhaseRow[] = SERVICE_PHASES.map((phase) => ({
    phase,
    weight: 0,
    label: phase,
    completed: done,
  }));
  return {
    activityId,
    classified: true,
    hasBuildEvidence: true,
    recorded: true,
    kind: 'service',
    phases,
    attempts: [attempt(origin)],
    worstOrigin: origin,
  };
}

/** A planned activity nothing has been recorded against. */
function unrecorded(activityId: string): ConstructionRow {
  return {
    activityId,
    classified: true,
    hasBuildEvidence: false,
    recorded: false,
    kind: 'service',
    phases: [],
    attempts: [],
  };
}

function nodes(rows: ConstructionRow[]): ActivityNode[] {
  return buildActivityTree(rows);
}

function only(ribbon: RibbonMilestone[], id: string): RibbonMilestone {
  const m = ribbon.find((x) => x.id === id);
  assert.ok(m !== undefined, `no milestone ${id}`);
  return m;
}

const MILESTONES = [
  { id: 'M0', name: 'SDP Review Approved' },
  { id: 'M1', name: 'Infrastructure Provisioned', dependsOn: ['R-a', 'R-b'] },
];

const DEPENDENCIES = [
  { activity: 'R-b', dependsOn: ['M0'] },
  { activity: 'R-a', dependsOn: ['M0'] },
  { activity: 'C-x', dependsOn: ['R-a'] },
];

// ---------------------------------------------------------------------------
// Shape
// ---------------------------------------------------------------------------

void test('milestones keep the network order', () => {
  const ribbon = gateRibbonFor(MILESTONES, DEPENDENCIES, []);
  assert.deepEqual(
    ribbon.map((m) => m.id),
    ['M0', 'M1']
  );
});

void test("a milestone's feeders are its dependsOn", () => {
  assert.deepEqual(only(gateRibbonFor(MILESTONES, DEPENDENCIES, []), 'M1').feeders, ['R-a', 'R-b']);
});

void test('M0 has no feeders; what it GATES is every activity that lists it, by id', () => {
  const m0 = only(gateRibbonFor(MILESTONES, DEPENDENCIES, []), 'M0');
  assert.deepEqual(m0.feeders, []);
  assert.deepEqual(m0.gates, ['R-a', 'R-b']);
  assert.equal(m0.complete, undefined);
});

// ---------------------------------------------------------------------------
// §9.2 — no laundered aggregate
// ---------------------------------------------------------------------------

void test('with every feeder OBSERVED, complete counts the feeders at 100%', () => {
  const m1 = only(
    gateRibbonFor(
      MILESTONES,
      DEPENDENCIES,
      nodes([recorded('R-a', true, 'observed'), recorded('R-b', false, 'observed')])
    ),
    'M1'
  );
  assert.equal(m1.complete, 1);
  assert.equal(m1.provenance, 'observed');
});

void test('§9.2: a RECONSTRUCTED feeder makes the count absent, not a badged number', () => {
  const m1 = only(
    gateRibbonFor(
      MILESTONES,
      DEPENDENCIES,
      nodes([recorded('R-a', true, 'observed'), recorded('R-b', true, 'backfilled')])
    ),
    'M1'
  );
  assert.equal(m1.complete, undefined);
  assert.equal(m1.provenance, 'backfilled');
});

void test('§9.2: an UNRECORDED feeder makes the count absent too', () => {
  const m1 = only(
    gateRibbonFor(
      MILESTONES,
      DEPENDENCIES,
      nodes([recorded('R-a', true, 'observed'), unrecorded('R-b')])
    ),
    'M1'
  );
  assert.equal(m1.complete, undefined);
  assert.equal(m1.provenance, 'unknown');
});

void test('reconstructed outranks unrecorded, and synthesized outranks backfilled', () => {
  const mixed = only(
    gateRibbonFor(
      MILESTONES,
      DEPENDENCIES,
      nodes([recorded('R-a', true, 'backfilled'), unrecorded('R-b')])
    ),
    'M1'
  );
  assert.equal(mixed.provenance, 'backfilled');
  const worse = only(
    gateRibbonFor(
      MILESTONES,
      DEPENDENCIES,
      nodes([recorded('R-a', true, 'backfilled'), recorded('R-b', true, 'synthesized')])
    ),
    'M1'
  );
  assert.equal(worse.provenance, 'synthesized');
});

void test('a feeder missing from the tree is neither complete nor observed, and never crashes', () => {
  const m1 = only(
    gateRibbonFor(MILESTONES, DEPENDENCIES, nodes([recorded('R-a', true, 'observed')])),
    'M1'
  );
  assert.equal(m1.complete, undefined);
  assert.equal(m1.provenance, 'unknown');
});

void test('an OBSERVED feeder with an unreported phase makes the count absent — never a false 0/n', () => {
  // R-b is observed but reports only four of its five phases, so its
  // percentage is unknown; counting it as "not complete" would read 1/2.
  const partial = recorded('R-b', true, 'observed');
  partial.phases = partial.phases.slice(0, 4);
  const tree = nodes([recorded('R-a', true, 'observed'), partial]);
  assert.equal(
    tree.find((n) => n.activityId === 'R-b')?.percentComplete,
    undefined,
    'fixture: R-b has an unknown percentage'
  );
  const m1 = only(gateRibbonFor(MILESTONES, DEPENDENCIES, tree), 'M1');
  assert.equal(m1.provenance, 'observed', 'fixture: every feeder is observed');
  assert.equal(m1.complete, undefined);
  assert.equal(m1.unreported, 1);
  assert.equal(m1.unobserved, 0);
});

void test('the ribbon counts the feeders with no observed record', () => {
  const m1 = only(
    gateRibbonFor(
      MILESTONES,
      DEPENDENCIES,
      nodes([recorded('R-a', true, 'backfilled'), unrecorded('R-b')])
    ),
    'M1'
  );
  assert.equal(m1.unobserved, 2);
});

void test('a milestone with no feeders is unknown, never observed', () => {
  assert.equal(only(gateRibbonFor(MILESTONES, DEPENDENCIES, []), 'M0').provenance, 'unknown');
});
