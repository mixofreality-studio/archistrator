/**
 * The lane spine (laneSpine.ts) — an activity's lifecycle drawn as five
 * canonical slots: a profile phase is as wide as its Table A-1 weight and
 * filled by its state, a phase the profile does not carry is a narrow gap, and
 * an unclassified activity has no spine at all (spec §9 AC4).
 *
 * Fixtures go through the list lens's own buildActivityTree, so the spine reads
 * exactly the tree the list renders — never a second derivation.
 */
/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import type { ConstructionRow, PhaseRow, TaskAttemptRow } from '../../../contracts/types.ts';
import { buildActivityTree, type ActivityNode } from '../list/activityTree.ts';
import {
  ABSENT_GAP_FRACTION,
  CANONICAL_LIFECYCLE,
  laneSpineFor,
  type SpineSegment,
} from './laneSpine.ts';

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

function row(overrides: Partial<ConstructionRow> = {}): ConstructionRow {
  return {
    activityId: 'C-x',
    classified: true,
    hasBuildEvidence: true,
    recorded: true,
    kind: 'service',
    phases: [],
    attempts: [],
    ...overrides,
  };
}

function attempt(overrides: Partial<TaskAttemptRow> = {}): TaskAttemptRow {
  return {
    attemptId: 'a1',
    task: 'codeReview',
    phase: 'construction',
    attempt: 1,
    outcome: 'passed',
    evidence: { kind: '', ref: '' },
    provenance: { origin: 'observed' },
    ...overrides,
  };
}

function phaseRow(overrides: Partial<PhaseRow> = {}): PhaseRow {
  return {
    phase: 'requirements',
    weight: 15,
    label: 'Requirements',
    completed: false,
    ...overrides,
  };
}

function nodeOf(r: ConstructionRow): ActivityNode {
  const [node] = buildActivityTree([r]);
  assert.ok(node !== undefined);
  return node;
}

function segment(segments: readonly SpineSegment[], phase: string): SpineSegment {
  const s = segments.find((x) => x.phase === phase);
  assert.ok(s !== undefined, `no ${phase} segment`);
  return s;
}

function near(a: number, b: number): boolean {
  return Math.abs(a - b) < 1e-9;
}

// ---------------------------------------------------------------------------
// Geometry
// ---------------------------------------------------------------------------

void test('the canonical order is the Service profile — Figure A-1 verbatim, all five phases', () => {
  assert.deepEqual(
    [...CANONICAL_LIFECYCLE],
    ['requirements', 'detailed_design', 'test_plan', 'construction', 'integration']
  );
});

void test('a service lane has five segments, each as wide as its Table A-1 weight', () => {
  const spine = laneSpineFor(nodeOf(row({ hasBuildEvidence: false })));
  assert.equal(spine.unclassified, false);
  assert.deepEqual(
    spine.segments.map((s) => s.phase),
    [...CANONICAL_LIFECYCLE]
  );
  const total = spine.segments.reduce((a, s) => a + s.fraction, 0);
  assert.ok(near(total, 1), `fractions sum to ${String(total)}`);
  for (const s of spine.segments) {
    assert.ok(s.weight !== undefined);
    assert.ok(near(s.fraction, s.weight / 100), `${s.phase}: ${String(s.fraction)}`);
  }
  assert.ok(near(segment(spine.segments, 'construction').fraction, 0.4));
});

void test('a phase the profile does not carry is a narrow ABSENT gap at its canonical place', () => {
  // Deployment carries Detailed Design, Construction and Integration only.
  const spine = laneSpineFor(nodeOf(row({ kind: 'deployment', hasBuildEvidence: false })));
  assert.deepEqual(
    spine.segments.map((s) => s.state === 'absent'),
    [true, false, true, false, false]
  );
  for (const phase of ['requirements', 'test_plan']) {
    const gap = segment(spine.segments, phase);
    assert.equal(gap.fraction, ABSENT_GAP_FRACTION);
    assert.deepEqual(gap.ticks, []);
    assert.equal(gap.weight, undefined);
  }
  const total = spine.segments.reduce((a, s) => a + s.fraction, 0);
  assert.ok(near(total, 1), `fractions sum to ${String(total)}`);
});

void test('AC4: an unclassified activity has no spine at all', () => {
  // The wire drops `kind` when the server could not classify the activity.
  const unclassified = row({ classified: false });
  delete unclassified.kind;
  const spine = laneSpineFor(nodeOf(unclassified));
  assert.equal(spine.unclassified, true);
  assert.deepEqual(spine.segments, []);
});

// ---------------------------------------------------------------------------
// Segment state
// ---------------------------------------------------------------------------

void test('a phase the SERVER reports complete is complete, whatever its ticks say', () => {
  const spine = laneSpineFor(
    nodeOf(
      row({
        phases: [phaseRow({ phase: 'requirements', completed: true })],
        attempts: [attempt({ task: 'srsReview', phase: 'requirements', outcome: 'rejected' })],
      })
    )
  );
  assert.equal(segment(spine.segments, 'requirements').state, 'complete');
});

void test('a phase whose latest attempt failed reads failed', () => {
  const spine = laneSpineFor(
    nodeOf(row({ attempts: [attempt({ task: 'codeReview', outcome: 'rejected' })] }))
  );
  assert.equal(segment(spine.segments, 'construction').state, 'failed');
});

void test('a pending gate on an in-review activity reads awaitingHuman — the loudest state', () => {
  const spine = laneSpineFor(
    nodeOf(
      row({
        status: 'in-review',
        attempts: [
          attempt({ task: 'construction', outcome: 'rejected' }),
          attempt({ task: 'codeReview', outcome: '' }),
        ],
      })
    )
  );
  assert.equal(segment(spine.segments, 'construction').state, 'awaitingHuman');
});

void test('a pending non-gate task reads running', () => {
  const spine = laneSpineFor(
    nodeOf(
      row({
        status: 'in-construction',
        attempts: [attempt({ task: 'construction', outcome: '' })],
      })
    )
  );
  assert.equal(segment(spine.segments, 'construction').state, 'running');
});

void test('a phase the server reports incomplete, with nothing pending, reads incomplete', () => {
  const spine = laneSpineFor(
    nodeOf(
      row({
        phases: [phaseRow({ phase: 'requirements', completed: false })],
        attempts: [attempt({ task: 'srs', phase: 'requirements', outcome: 'passed' })],
      })
    )
  );
  assert.equal(segment(spine.segments, 'requirements').state, 'incomplete');
});

void test('a classified activity with no evidence reads notStarted in every segment', () => {
  const spine = laneSpineFor(nodeOf(row({ hasBuildEvidence: false })));
  assert.deepEqual(new Set(spine.segments.map((s) => s.state)), new Set(['notStarted']));
});

void test('evidence with no per-task history reads unknown, never notStarted', () => {
  // The list's own no-attempt rule (noAttemptStateFor): recorded work that
  // predates per-task capture is UNKNOWN, not "has not run".
  const spine = laneSpineFor(nodeOf(row({ hasBuildEvidence: true, attempts: [] })));
  assert.deepEqual(new Set(spine.segments.map((s) => s.state)), new Set(['unknown']));
});

// ---------------------------------------------------------------------------
// Ticks
// ---------------------------------------------------------------------------

void test('a task retried twice carries retries: 2', () => {
  const spine = laneSpineFor(
    nodeOf(
      row({
        attempts: [
          attempt({ task: 'codeReview', attempt: 1, outcome: 'rejected' }),
          attempt({ task: 'codeReview', attempt: 2, outcome: 'rejected' }),
          attempt({ task: 'codeReview', attempt: 3, outcome: 'passed' }),
        ],
      })
    )
  );
  const tick = segment(spine.segments, 'construction').ticks.find((t) => t.task === 'codeReview');
  assert.ok(tick !== undefined);
  assert.equal(tick.retries, 2);
  assert.equal(tick.state, 'passed');
  assert.equal(tick.gate, true);
});

void test('a conditional task is a tick only once it was actually attempted', () => {
  const quiet = laneSpineFor(nodeOf(row({ hasBuildEvidence: false })));
  assert.equal(
    segment(quiet.segments, 'detailed_design').ticks.some((t) => t.task === 'someConstruction'),
    false
  );
  const attempted = laneSpineFor(
    nodeOf(
      row({
        attempts: [
          attempt({ task: 'someConstruction', phase: 'detailed_design', outcome: 'passed' }),
        ],
      })
    )
  );
  assert.equal(
    segment(attempted.segments, 'detailed_design').ticks.some((t) => t.task === 'someConstruction'),
    true
  );
});
