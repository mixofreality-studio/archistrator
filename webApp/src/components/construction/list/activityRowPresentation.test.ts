/**
 * The LIST lens's presentation rules (activityRowPresentation.ts). The brief's
 * four cases come first, verbatim in intent; the rest pin the rules the brief
 * states as prose but does not itself exercise — the ones a later edit could
 * quietly undo (a chip creeping onto `unknown`, a fabricated zero float, the
 * `skipped` outcome borrowing the success mark).
 *
 * The renderer is deliberately not in the way: ActivityTreeView.tsx is a `.tsx`
 * module and Node's native test runner cannot load one at all.
 */
/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import type { ConstructionRow, TaskAttemptRow } from '../../../contracts/types.ts';
import { buildActivityTree, type ActivityNode, type TaskNode } from './activityTree.ts';
import {
  activityRowState,
  attemptRowState,
  chipFor,
  criticalBorderPx,
  currentStageMarker,
  effortBarFraction,
  emphasisRank,
  floatPresentation,
  inlineActionsFor,
  isCurrentStage,
  percentLabel,
  retryCounterLabel,
  ROW_STATE_LABEL,
  stageRule,
  taskRowState,
  type RowState,
} from './activityRowPresentation.ts';

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

function row(overrides: Partial<ConstructionRow> = {}): ConstructionRow {
  return {
    activityId: 'C-x',
    classified: true,
    hasBuildEvidence: true,
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

function onlyNode(r: ConstructionRow): ActivityNode {
  const nodes = buildActivityTree([r]);
  const node = nodes[0];
  assert.ok(node !== undefined);
  return node;
}

function taskNamed(node: ActivityNode, task: string): TaskNode {
  const found = node.phases.flatMap((p) => p.tasks).find((t) => t.task === task);
  assert.ok(found !== undefined, `no task ${task} in the profile`);
  return found;
}

const EVERY_STATE: RowState[] = [
  'unknown',
  'notStarted',
  'running',
  'awaitingHuman',
  'passed',
  'failed',
  'absent',
  'skipped',
  'superseded',
];

// ---------------------------------------------------------------------------
// The brief's four cases
// ---------------------------------------------------------------------------

void test('never renders a chip for the unknown state', () => {
  assert.equal(chipFor('unknown'), undefined);
});

void test('marks awaitingHuman as the loudest state', () => {
  assert.ok(emphasisRank('awaitingHuman') > emphasisRank('running'));
  assert.ok(emphasisRank('awaitingHuman') > emphasisRank('failed'));
});

void test('always offers an inline retry on a failed task', () => {
  assert.ok(inlineActionsFor('failed').includes('retry'));
});

void test('renders float as a numeral, not only a colour band', () => {
  assert.equal(floatPresentation(6).numeral, '6');
});

// ---------------------------------------------------------------------------
// Chips: at most one per row, and only where nothing else already says it
// ---------------------------------------------------------------------------

void test('only the four states that assert something happened carry a chip', () => {
  const chipped = EVERY_STATE.filter((s) => chipFor(s) !== undefined);
  assert.deepEqual(chipped, ['running', 'awaitingHuman', 'passed', 'failed']);
});

void test('the states that already have a geometry carry no chip', () => {
  // Each of these has its own geometric channel — a dashed hairline, a hollow
  // circle, a struck label, reduced opacity, a `↻n` index. A chip on top would
  // be the double-encoding two rejected rounds died of.
  for (const state of ['unknown', 'notStarted', 'absent', 'skipped', 'superseded'] as const) {
    assert.equal(chipFor(state), undefined, `${state} must not carry a chip`);
  }
});

void test('the two states that owe a human something get the larger chip', () => {
  assert.equal(chipFor('awaitingHuman')?.size, 'sm');
  assert.equal(chipFor('failed')?.size, 'sm');
  assert.equal(chipFor('running')?.size, 'xs');
  assert.equal(chipFor('passed')?.size, 'xs');
});

void test('every state has a label, including the ones that never render a chip', () => {
  for (const state of EVERY_STATE) {
    assert.ok(ROW_STATE_LABEL[state].length > 0, `${state} has no label`);
  }
});

void test('emphasis is a total order with no ties between the four chip states', () => {
  const ranks = (['awaitingHuman', 'failed', 'running', 'passed'] as const).map(emphasisRank);
  assert.deepEqual(
    [...ranks].sort((a, b) => b - a),
    ranks
  );
  assert.equal(new Set(ranks).size, ranks.length);
});

void test('unknown and absent are the quietest states on the surface', () => {
  for (const state of EVERY_STATE) {
    assert.ok(emphasisRank(state) >= emphasisRank('unknown'));
    assert.ok(emphasisRank(state) >= emphasisRank('absent'));
  }
});

void test('no state but failed offers an inline action', () => {
  for (const state of EVERY_STATE.filter((s) => s !== 'failed')) {
    assert.deepEqual(inlineActionsFor(state), []);
  }
});

// ---------------------------------------------------------------------------
// Magnitudes: geometry, and never a fabricated zero
// ---------------------------------------------------------------------------

void test('an unknown float renders as unknown, never as zero', () => {
  const p = floatPresentation(undefined);
  assert.equal(p.numeral, '—');
  assert.equal(p.known, false);
  assert.equal(p.band, undefined);
});

void test('a real zero float is a real zero, not an absence', () => {
  const p = floatPresentation(0, 'critical');
  assert.equal(p.numeral, '0');
  assert.equal(p.known, true);
  assert.equal(p.band, 'critical');
});

void test('the band is passed through, never re-derived from the number', () => {
  // 6 days would be `yellow` under the server's policy; the caller says `green`,
  // and the view renders `green`. Re-deriving here is the hand-mirror this
  // codebase has already paid for twice.
  assert.equal(floatPresentation(6, 'green').band, 'green');
});

void test('an activity with no effort estimate draws no bar at all', () => {
  assert.equal(effortBarFraction(undefined, 35), undefined);
  assert.equal(effortBarFraction(35, 0), undefined);
});

void test('the effort bar is a fraction of the widest activity on screen', () => {
  assert.equal(effortBarFraction(15, 30), 0.5);
  assert.equal(effortBarFraction(60, 30), 1);
});

void test('an unknowable percentage renders as unknown, never as 0%', () => {
  assert.equal(percentLabel(undefined), '—');
  assert.equal(percentLabel(0), '0%');
  assert.equal(percentLabel(60), '60%');
});

void test('one attempt is not history; two or more is', () => {
  assert.equal(retryCounterLabel(0), undefined);
  assert.equal(retryCounterLabel(1), undefined);
  assert.equal(retryCounterLabel(3), '↻3');
});

void test('criticality is asserted at 3px and never assumed', () => {
  assert.equal(criticalBorderPx(true), 3);
  assert.equal(criticalBorderPx(false), 2);
  assert.equal(criticalBorderPx(undefined), 2);
});

// ---------------------------------------------------------------------------
// Row state derivation
// ---------------------------------------------------------------------------

void test('an unclassified activity row is unknown, and therefore chip-less', () => {
  const state = activityRowState(row({ classified: false, hasBuildEvidence: false }));
  assert.equal(state, 'unknown');
  assert.equal(chipFor(state), undefined);
});

void test('a classified activity with no evidence is notStarted, and also chip-less', () => {
  const state = activityRowState(row({ kind: 'service', hasBuildEvidence: false }));
  assert.equal(state, 'notStarted');
  assert.equal(chipFor(state), undefined);
});

void test('an in-review activity is the loudest row on the screen', () => {
  const state = activityRowState(row({ kind: 'service', status: 'in-review' }));
  assert.equal(state, 'awaitingHuman');
  assert.equal(emphasisRank(state), emphasisRank('awaitingHuman'));
});

void test('a task with no attempt is unknown — the majority case, chip-less', () => {
  const node = onlyNode(row({ kind: 'service' }));
  const task = taskNamed(node, 'srs');
  assert.equal(taskRowState(task, undefined), 'unknown');
  assert.equal(chipFor(taskRowState(task, undefined)), undefined);
});

void test('a pending gate task on an in-review activity is awaiting the human', () => {
  const node = onlyNode(
    row({
      kind: 'service',
      status: 'in-review',
      attempts: [attempt({ task: 'codeReview', outcome: '' })],
    })
  );
  const gate = taskNamed(node, 'codeReview');
  assert.equal(gate.gate, true);
  assert.equal(taskRowState(gate, 'in-review'), 'awaitingHuman');
});

void test('the same pending outcome elsewhere is merely running', () => {
  const node = onlyNode(
    row({ kind: 'service', attempts: [attempt({ task: 'construction', outcome: '' })] })
  );
  const notAGate = taskNamed(node, 'construction');
  assert.equal(notAGate.gate, false);
  assert.equal(taskRowState(notAGate, 'in-construction'), 'running');
});

void test('a skipped task does not borrow the success mark', () => {
  // The tree's four-member union has to fold `skipped` onto `passed`; this
  // surface has a channel for it and must use it. A skipped task did not run,
  // and the phase's exit is the GATE task's verdict, not this one's.
  const node = onlyNode(
    row({ kind: 'service', attempts: [attempt({ task: 'srs', outcome: 'skipped' })] })
  );
  const task = taskNamed(node, 'srs');
  assert.equal(task.state, 'passed', 'the tree still reports its coarse answer');
  assert.equal(taskRowState(task, undefined), 'skipped');
  assert.equal(chipFor('skipped'), undefined);
});

void test('a superseded attempt is quiet regardless of what it once was', () => {
  assert.equal(attemptRowState(true, 'failed'), 'superseded');
  assert.equal(attemptRowState(false, 'failed'), 'failed');
});

// ---------------------------------------------------------------------------
// The current phase that the profile does not carry (the real G-SPA case)
// ---------------------------------------------------------------------------

void test('a current phase the profile does not carry is reported as absent, not silently dropped', () => {
  // G-SPA verbatim: a two-phase uiDesign profile, CurrentPhase = 'integration'.
  const node = onlyNode(row({ kind: 'uiDesign', currentLifecyclePhase: 'integration' }));
  const marker = currentStageMarker(node);
  assert.deepEqual(marker, { named: 'integration', inProfile: false });
  assert.equal(chipFor('absent'), undefined);
  // …and nothing in the profile falsely claims to be current.
  assert.deepEqual(
    node.phases.filter((p) => isCurrentStage(node, p)),
    []
  );
});

void test('a current phase the profile does carry highlights exactly one node', () => {
  const node = onlyNode(row({ kind: 'service', currentLifecyclePhase: 'test_plan' }));
  assert.deepEqual(currentStageMarker(node), { named: 'test_plan', inProfile: true });
  assert.deepEqual(
    node.phases.filter((p) => isCurrentStage(node, p)).map((p) => p.phase),
    ['test_plan']
  );
});

void test('no reported current phase marks nothing', () => {
  const node = onlyNode(row({ kind: 'service' }));
  assert.equal(currentStageMarker(node), undefined);
  assert.deepEqual(
    node.phases.filter((p) => isCurrentStage(node, p)),
    []
  );
});

// ---------------------------------------------------------------------------
// The tier-2 group rule
// ---------------------------------------------------------------------------

void test('the group rule carries the weight as BOTH a numeral and a width', () => {
  const node = onlyNode(row({ kind: 'service' }));
  const heaviest = Math.max(...node.phases.map((p) => p.weight));
  const construction = node.phases.find((p) => p.phase === 'construction');
  assert.ok(construction !== undefined);
  const rule = stageRule(construction, heaviest);
  assert.equal(rule.weightLabel, 'wt 40');
  assert.equal(rule.weightFraction, 1);
  assert.ok(rule.exitCriterion.length > 0);
});

void test('an unreported phase is marked unreported rather than incomplete', () => {
  const node = onlyNode(row({ kind: 'service' }));
  const first = node.phases[0];
  assert.ok(first !== undefined);
  const rule = stageRule(first, 40);
  assert.equal(rule.unreported, true);
  assert.equal(rule.filled, false);
});
