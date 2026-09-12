/**
 * The LIST lens's tree derivation (activityTree.ts) — a pure function, so it is
 * tested directly with no renderer in the way. The eight cases the Stage-B brief
 * prescribes come first, verbatim in intent; the ones after them pin the rules
 * the brief states but does not itself exercise (profile order, the server as the
 * SOLE authority on phase completion, the arithmetic of a fully-reported row).
 */
/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import type { ConstructionRow, PhaseRow, TaskAttemptRow } from '../../../contracts/types.ts';
import { buildActivityTree } from './activityTree.ts';

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

function row(overrides: Partial<ConstructionRow> = {}): ConstructionRow {
  return {
    activityId: 'C-x',
    classified: true,
    hasBuildEvidence: true,
    recorded: true,
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

/** The single node for a single-row tree — asserted, never `!`-asserted. */
function onlyNode(rows: readonly ConstructionRow[]): ReturnType<typeof buildActivityTree>[number] {
  const nodes = buildActivityTree(rows);
  assert.equal(nodes.length, 1);
  const node = nodes[0];
  assert.ok(node !== undefined);
  return node;
}

// ---------------------------------------------------------------------------
// The brief's eight cases
// ---------------------------------------------------------------------------

void test('derives the row set from the profile, not from stored data', () => {
  const node = onlyNode([row({ activityId: 'C-x', kind: 'service', phases: [], attempts: [] })]);

  assert.deepEqual(
    node.phases.map((p) => p.phase),
    ['requirements', 'detailed_design', 'test_plan', 'construction', 'integration']
  );

  // Every task exists, every one unknown; conditional tasks suppressed.
  const tasks = node.phases.flatMap((p) => p.tasks);
  assert.equal(
    tasks.every((t) => t.state === 'unknown'),
    true
  );
  const taskIds = tasks.map((t) => t.task);
  assert.equal(taskIds.includes('someConstruction'), false);
  assert.equal(taskIds.includes('testClient'), false);
});

void test('emits a conditional task once a real attempt exists for it', () => {
  const node = onlyNode([
    row({
      activityId: 'C-x',
      kind: 'service',
      phases: [],
      attempts: [
        attempt({ task: 'someConstruction', phase: 'detailed_design', outcome: 'passed' }),
      ],
    }),
  ]);

  const dd = node.phases.find((p) => p.phase === 'detailed_design');
  assert.ok(dd !== undefined);
  assert.equal(dd.tasks.map((t) => t.task).includes('someConstruction'), true);
});

void test('gives an unclassified activity no phases and no tasks', () => {
  const node = onlyNode([
    row({ activityId: 'C-artifact-access', classified: false, phases: [], attempts: [] }),
  ]);

  assert.deepEqual(node.phases, []);
  assert.equal(node.unclassified, true);
});

void test('shows a retried task as one row whose state is the LATEST attempt', () => {
  const node = onlyNode([
    row({
      activityId: 'C-x',
      kind: 'service',
      phases: [],
      attempts: [
        attempt({
          task: 'designReview',
          phase: 'detailed_design',
          attempt: 1,
          outcome: 'rejected',
        }),
        attempt({ task: 'designReview', phase: 'detailed_design', attempt: 2, outcome: 'passed' }),
      ],
    }),
  ]);

  const dd = node.phases.find((p) => p.phase === 'detailed_design');
  assert.ok(dd !== undefined);
  const dr = dd.tasks.find((t) => t.task === 'designReview');
  assert.ok(dr !== undefined);

  assert.equal(dr.attempts.length, 2);
  assert.equal(dr.state, 'passed');
  assert.equal(dr.attempts[0]?.superseded, true);
  assert.equal(dr.attempts[1]?.superseded, false);
});

void test('selects the latest attempt by number, not by array position', () => {
  const node = onlyNode([
    row({
      activityId: 'C-x',
      kind: 'service',
      phases: [],
      attempts: [
        attempt({ task: 'codeReview', attempt: 2, outcome: 'passed' }),
        attempt({ task: 'codeReview', attempt: 1, outcome: 'rejected' }),
      ],
    }),
  ]);

  const construction = node.phases.find((p) => p.phase === 'construction');
  assert.ok(construction !== undefined);
  const cr = construction.tasks.find((t) => t.task === 'codeReview');
  assert.ok(cr !== undefined);
  assert.equal(cr.state, 'passed');
  // ...and the ledger itself is presented in attempt order, not arrival order.
  assert.deepEqual(
    cr.attempts.map((a) => a.attempt),
    [1, 2]
  );
});

void test('uses the variant profile for a testing activity', () => {
  const node = onlyNode([
    row({
      activityId: 'N-IT',
      kind: 'testing',
      variant: 'systemTest',
      phases: [],
      attempts: [],
    }),
  ]);

  assert.deepEqual(
    node.phases.map((p) => p.name),
    ['Smoke Pass', 'Use-Case Execution', 'Regression & Sign-off']
  );
});

void test('reports percent complete as undefined while any phase is unknown', () => {
  const node = onlyNode([row({ activityId: 'C-x', kind: 'service', phases: [], attempts: [] })]);
  assert.equal(node.percentComplete, undefined);
});

void test('drops a stored phase the profile does not carry and counts it as a defect', () => {
  const node = onlyNode([
    row({
      activityId: 'U-ui',
      kind: 'uiDesign',
      phases: [
        phaseRow({ phase: 'requirements', weight: 40, completed: false }),
        phaseRow({ phase: 'detailed_design', weight: 60, completed: true }),
        // Not in the uiDesign profile.
        phaseRow({ phase: 'construction', weight: 40, completed: true }),
      ],
      attempts: [],
    }),
  ]);

  assert.equal(node.phases.length, 2);
  assert.equal(node.offProfilePhaseCount, 1);
});

// ---------------------------------------------------------------------------
// The rules the brief states but does not itself exercise
// ---------------------------------------------------------------------------

void test('sums the weights of completed phases once every profile phase is reported', () => {
  const node = onlyNode([
    row({
      activityId: 'C-x',
      kind: 'service',
      phases: [
        phaseRow({ phase: 'requirements', weight: 15, completed: true }),
        phaseRow({ phase: 'detailed_design', weight: 20, completed: true }),
        phaseRow({ phase: 'test_plan', weight: 10, completed: true }),
        phaseRow({ phase: 'construction', weight: 40, completed: false }),
        phaseRow({ phase: 'integration', weight: 15, completed: false }),
      ],
      attempts: [],
    }),
  ]);

  assert.equal(node.percentComplete, 45);
});

void test('weights the percentage from the PROFILE, not from the stored weight', () => {
  // The stored row claims a 99-point requirements phase; Table A-1 says 15.
  const node = onlyNode([
    row({
      activityId: 'C-x',
      kind: 'service',
      phases: [
        phaseRow({ phase: 'requirements', weight: 99, completed: true }),
        phaseRow({ phase: 'detailed_design', weight: 20, completed: false }),
        phaseRow({ phase: 'test_plan', weight: 10, completed: false }),
        phaseRow({ phase: 'construction', weight: 40, completed: false }),
        phaseRow({ phase: 'integration', weight: 15, completed: false }),
      ],
      attempts: [],
    }),
  ]);

  assert.equal(node.percentComplete, 15);
});

void test('never re-derives phase completion from the attempt ledger', () => {
  // A passed GATE attempt for requirements, and no stored phase row saying so.
  // The server is the single authority: this phase stays unknown.
  const node = onlyNode([
    row({
      activityId: 'C-x',
      kind: 'service',
      phases: [],
      attempts: [attempt({ task: 'srsReview', phase: 'requirements', outcome: 'passed' })],
    }),
  ]);

  const requirements = node.phases.find((p) => p.phase === 'requirements');
  assert.ok(requirements !== undefined);
  assert.equal(requirements.completion, 'unknown');
  assert.equal(node.percentComplete, undefined);
});

void test('separates an unreported phase (unknown) from a reported-incomplete one', () => {
  const node = onlyNode([
    row({
      activityId: 'C-x',
      kind: 'service',
      phases: [phaseRow({ phase: 'requirements', weight: 15, completed: false })],
      attempts: [],
    }),
  ]);

  assert.equal(node.phases.find((p) => p.phase === 'requirements')?.completion, 'incomplete');
  assert.equal(node.phases.find((p) => p.phase === 'integration')?.completion, 'unknown');
});

void test('maps a pending outcome to running and a rejected latest to failed', () => {
  const node = onlyNode([
    row({
      activityId: 'C-x',
      kind: 'service',
      phases: [],
      attempts: [
        attempt({ task: 'construction', phase: 'construction', attempt: 1, outcome: '' }),
        attempt({ task: 'codeReview', phase: 'construction', attempt: 1, outcome: 'rejected' }),
      ],
    }),
  ]);

  const construction = node.phases.find((p) => p.phase === 'construction');
  assert.ok(construction !== undefined);
  assert.equal(construction.tasks.find((t) => t.task === 'construction')?.state, 'running');
  assert.equal(construction.tasks.find((t) => t.task === 'codeReview')?.state, 'failed');
});

void test('keeps the profile order of phases and the execution order of tasks', () => {
  const node = onlyNode([
    row({
      activityId: 'C-x',
      kind: 'service',
      // Stored out of Method order on purpose — this must not reorder the tree.
      phases: [
        phaseRow({ phase: 'integration', weight: 15, completed: true }),
        phaseRow({ phase: 'requirements', weight: 15, completed: true }),
      ],
      attempts: [
        attempt({ task: 'someConstruction', phase: 'detailed_design', outcome: 'passed' }),
      ],
    }),
  ]);

  assert.deepEqual(
    node.phases.map((p) => p.phase),
    ['requirements', 'detailed_design', 'test_plan', 'construction', 'integration']
  );
  const dd = node.phases.find((p) => p.phase === 'detailed_design');
  assert.deepEqual(
    dd?.tasks.map((t) => t.task),
    ['someConstruction', 'detailedDesign', 'designReview']
  );
});

void test('counts a retry per task and keeps every node id unique across the tree', () => {
  const nodes = buildActivityTree([
    row({
      activityId: 'C-a',
      kind: 'service',
      attempts: [
        attempt({
          task: 'designReview',
          phase: 'detailed_design',
          attempt: 1,
          outcome: 'rejected',
        }),
        attempt({ task: 'designReview', phase: 'detailed_design', attempt: 2, outcome: 'passed' }),
      ],
    }),
    row({ activityId: 'C-b', kind: 'service' }),
  ]);

  assert.deepEqual(
    nodes.map((n) => n.activityId),
    ['C-a', 'C-b']
  );
  assert.equal(nodes[0]?.retryCount, 1);
  assert.equal(nodes[1]?.retryCount, 0);

  const ids: string[] = [];
  for (const n of nodes) {
    ids.push(n.nodeId);
    for (const p of n.phases) {
      ids.push(p.nodeId);
      for (const t of p.tasks) ids.push(t.nodeId);
    }
  }
  assert.equal(new Set(ids).size, ids.length);
});

void test('joins per-activity network facts by activity id and leaves them absent otherwise', () => {
  const nodes = buildActivityTree(
    [row({ activityId: 'C-a', kind: 'service' }), row({ activityId: 'C-b', kind: 'service' })],
    {
      meta: { 'C-a': { label: 'Billing Manager', effortDays: 15, float: 0, onCriticalPath: true } },
    }
  );
  const withMeta = nodes[0];
  const without = nodes[1];
  assert.ok(withMeta !== undefined);
  assert.ok(without !== undefined);

  assert.equal(withMeta.label, 'Billing Manager');
  assert.equal(withMeta.effortDays, 15);
  assert.equal(withMeta.onCriticalPath, true);
  // No meta is ABSENT, never a fabricated zero.
  assert.equal('effortDays' in without, false);
  assert.equal('float' in without, false);
  assert.equal('onCriticalPath' in without, false);
  // The label falls back to the activity's own id — never to another row's.
  assert.equal(without.label, 'C-b');
});

void test('treats an empty current phase as an absent one', () => {
  // The wire mapper gates currentLifecyclePhase on `classified` only, so the
  // server's zero value reaches here as '' on every row nothing has started on.
  const present = onlyNode([
    row({ activityId: 'C-x', kind: 'service', currentLifecyclePhase: 'construction' }),
  ]);
  const blank = onlyNode([row({ activityId: 'C-y', kind: 'service', currentLifecyclePhase: '' })]);

  assert.equal(present.currentLifecyclePhase, 'construction');
  assert.equal('currentLifecyclePhase' in blank, false);
});

void test('makes no claim at all about an unclassified row', () => {
  const node = onlyNode([
    row({
      activityId: 'C-artifact-access',
      classified: false,
      hasBuildEvidence: false,
      phases: [phaseRow({ phase: 'construction', weight: 40, completed: true })],
      attempts: [attempt({ task: 'codeReview', outcome: 'passed' })],
    }),
  ]);

  assert.deepEqual(node.phases, []);
  assert.equal(node.percentComplete, undefined);
  assert.equal(node.taskCount, 0);
  // The defect is the missing classification, already reported by `unclassified` —
  // stored phases are not additionally counted as off-profile.
  assert.equal(node.offProfilePhaseCount, 0);
});

void test('falls back to the generic testing profile when the variant is missing', () => {
  // No `variant` key at all — `exactOptionalPropertyTypes` makes that the only
  // way to express "the server reported none", which is the real wire case.
  const node = onlyNode([row({ activityId: 'N-STP', kind: 'testing' })]);
  assert.deepEqual(
    node.phases.map((p) => p.phase),
    ['requirements', 'construction', 'integration']
  );
});

// ---------------------------------------------------------------------------
// Per-profile vocabulary (fix round B, designer P1-7)
// ---------------------------------------------------------------------------

void test('a profile names its own tasks and exits: N-STP is not closed by a Code Review', () => {
  const stp = onlyNode([row({ activityId: 'N-STP', kind: 'testing', variant: 'plan' })]);
  const svc = onlyNode([row({ activityId: 'C-x', kind: 'service' })]);
  const stpBuild = stp.phases.find((p) => p.phase === 'construction');
  const svcBuild = svc.phases.find((p) => p.phase === 'construction');
  assert.ok(stpBuild !== undefined && svcBuild !== undefined);

  const gate = stpBuild.tasks.find((tk) => tk.gate);
  assert.ok(gate !== undefined);
  // The KEY is the book's and never varies — it is the ledger's join key …
  assert.equal(gate.task, 'codeReview');
  assert.equal(gate.bookLabel, 'Code Review');
  // … but the label and the exit are the profile's own.
  assert.notEqual(gate.label, 'Code Review');
  assert.notEqual(stpBuild.exitCriterion, svcBuild.exitCriterion);
  assert.ok(stpBuild.exitCriterion.length > 0);
  // A Service row reads the book.
  const svcGate = svcBuild.tasks.find((tk) => tk.gate);
  assert.ok(svcGate !== undefined);
  assert.equal(svcGate.label, svcGate.bookLabel);
});
