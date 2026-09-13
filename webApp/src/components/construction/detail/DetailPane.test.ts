/**
 * The shared detail pane's PURE logic (detailPaneState.ts) — the header and
 * action-bar invariants have to hold without rendering anything, so this
 * tests that module directly. See detailPaneState.ts's own header comment for
 * why DetailPane.tsx itself (JSX) cannot be imported by node:test at all.
 */
/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import type { ConstructionRow, TaskAttemptRow } from '../../../contracts/types.ts';
import type { LensSelection } from '../lens/useLensSelection.ts';
import {
  attemptProvenance,
  attemptsForTask,
  breadcrumbFor,
  detailActionsFor,
  observedOnlyChipLabel,
  resolvePhaseTask,
  runActionFor,
  taskDetailStateFor,
  WIDE_PANE_SX,
} from './detailPaneState.ts';

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

// ---------------------------------------------------------------------------
// The two invariants (brief steps, verbatim)
// ---------------------------------------------------------------------------

const RUN = runActionFor(undefined, {});

void test('enables the retry action in every task state', () => {
  for (const state of [
    'unknown',
    'notStarted',
    'running',
    'awaitingHuman',
    'passed',
    'failed',
  ] as const) {
    // Even a run action handed in disabled comes out enabled: nothing may gate it.
    const actions = detailActionsFor(state, { ...RUN, disabled: true });
    const retry = actions.find((a) => a.id === 'run');
    assert.ok(retry !== undefined, `no retry action for ${state}`);
    assert.equal(retry.disabled, false, `retry disabled for ${state}`);
  }
});

void test('offers approve and send-back only where a human decision is owed', () => {
  assert.ok(
    detailActionsFor('awaitingHuman', RUN)
      .map((a) => a.id)
      .includes('approve')
  );
  assert.equal(
    detailActionsFor('passed', RUN)
      .map((a) => a.id)
      .includes('approve'),
    false
  );
});

// Designer re-check B2: the run action names what is selected, and reads ↻ (run
// AGAIN) only where the selection holds an attempt — ▶ for a first run.
void test('the run action names its selection, and marks a re-run only where an attempt exists', () => {
  const r = row({
    attempts: [attempt({ task: 'codeReview', phase: 'construction' })],
  });
  assert.equal(runActionFor(r, { activityId: 'C-x' }).label, '↻ Run this activity');
  assert.equal(
    runActionFor(r, { activityId: 'C-x', lifecyclePhase: 'construction' }).label,
    '↻ Run this phase'
  );
  assert.equal(
    runActionFor(r, { activityId: 'C-x', lifecyclePhase: 'requirements' }).label,
    '▶ Run this phase'
  );
  assert.equal(
    runActionFor(r, { activityId: 'C-x', lifecyclePhase: 'construction', task: 'codeReview' })
      .label,
    '↻ Run this task'
  );
  assert.equal(
    runActionFor(r, { activityId: 'C-x', lifecyclePhase: 'construction', task: 'construction' })
      .label,
    '▶ Run this task'
  );
  assert.equal(runActionFor(row(), { activityId: 'C-x' }).label, '▶ Run this activity');
  assert.equal(runActionFor(undefined, {}).label, '▶ Run this activity');
});

void test('the Observed-only chip names the toggle and the count, never UNRECORDED', () => {
  assert.equal(observedOnlyChipLabel(10), 'OBSERVED ONLY · 10 reconstructed hidden');
  assert.doesNotMatch(observedOnlyChipLabel(1), /UNRECORDED/);
});

// ---------------------------------------------------------------------------
// taskDetailStateFor
// ---------------------------------------------------------------------------

void test('a selected task with no attempt at all is unknown, not notStarted', () => {
  const r = row({ status: 'in-construction', attempts: [] });
  const selection: LensSelection = { activityId: 'C-x', task: 'codeReview' };
  assert.equal(taskDetailStateFor(r, selection), 'unknown');
});

// Designer re-check N3: the pane agrees with the not-started activity above it.
void test('a task under a classified, not-started activity reads notStarted, not unknown', () => {
  const r = row({ hasBuildEvidence: false, recorded: false, attempts: [] });
  const selection: LensSelection = { activityId: 'C-x', task: 'srs' };
  assert.equal(taskDetailStateFor(r, selection), 'notStarted');
  // Unclassified stays unknown whatever the evidence says.
  assert.equal(taskDetailStateFor(row({ classified: false, hasBuildEvidence: false }), selection), 'unknown');
});

void test('picks the latest attempt by NUMBER, not array position', () => {
  const r = row({
    attempts: [
      attempt({ attempt: 2, outcome: 'passed' }),
      attempt({ attempt: 1, outcome: 'rejected' }),
    ],
  });
  const selection: LensSelection = { activityId: 'C-x', task: 'codeReview' };
  assert.equal(taskDetailStateFor(r, selection), 'passed');
});

void test('a pending gate task on an in-review row is awaitingHuman, not running', () => {
  const r = row({
    status: 'in-review',
    attempts: [attempt({ outcome: '' })],
  });
  const selection: LensSelection = { activityId: 'C-x', task: 'codeReview' };
  assert.equal(taskDetailStateFor(r, selection), 'awaitingHuman');
});

void test('a pending task on an in-construction row is running', () => {
  const r = row({
    status: 'in-construction',
    attempts: [attempt({ outcome: '' })],
  });
  const selection: LensSelection = { activityId: 'C-x', task: 'codeReview' };
  assert.equal(taskDetailStateFor(r, selection), 'running');
});

void test('an unclassified row is unknown regardless of evidence', () => {
  const r = row({ classified: false, hasBuildEvidence: true, status: 'in-construction' });
  assert.equal(taskDetailStateFor(r, {}), 'unknown');
});

void test('a classified row with no evidence and nothing selected is notStarted', () => {
  const r = row({ classified: true, hasBuildEvidence: false });
  assert.equal(taskDetailStateFor(r, {}), 'notStarted');
});

void test('an absent row is unknown', () => {
  assert.equal(taskDetailStateFor(undefined, {}), 'unknown');
});

// ---------------------------------------------------------------------------
// attemptsForTask / attemptProvenance
// ---------------------------------------------------------------------------

void test('attemptsForTask sorts ascending by attempt number, filters by task', () => {
  const attempts = [
    attempt({ task: 'codeReview', attempt: 3 }),
    attempt({ task: 'designReview', attempt: 1 }),
    attempt({ task: 'codeReview', attempt: 1 }),
  ];
  const got = attemptsForTask(attempts, 'codeReview');
  assert.deepEqual(
    got.map((a) => a.attempt),
    [1, 3]
  );
});

void test('attemptProvenance reads the selected attempt, falling back to the row worst-origin', () => {
  const r = row({
    worstOrigin: 'backfilled',
    attempts: [attempt({ task: 'codeReview', attempt: 1, provenance: { origin: 'synthesized' } })],
  });
  assert.equal(attemptProvenance(r, { task: 'codeReview' }), 'synthesized');
  assert.equal(attemptProvenance(r, {}), 'backfilled');
});

// ---------------------------------------------------------------------------
// resolvePhaseTask / breadcrumbFor
// ---------------------------------------------------------------------------

void test('resolvePhaseTask reads weight and exit criterion from the generated profile', () => {
  const r = row({ kind: 'service', currentLifecyclePhase: 'detailed_design' });
  const resolved = resolvePhaseTask(r, { task: 'designReview' });
  assert.equal(resolved.phaseName, 'Detailed Design');
  assert.equal(resolved.phaseWeight, 20);
  assert.equal(resolved.taskLabel, 'Design Review');
  assert.ok(resolved.exitCriterion !== undefined && resolved.exitCriterion.length > 0);
});

void test('resolvePhaseTask uses the testing VARIANT profile, not the generic representative', () => {
  const r = row({ kind: 'testing', variant: 'systemTest', currentLifecyclePhase: 'construction' });
  const resolved = resolvePhaseTask(r, {});
  assert.equal(resolved.phaseName, 'Use-Case Execution');
  assert.equal(resolved.phaseWeight, 45);
});

void test('resolvePhaseTask is honestly empty when nothing resolves', () => {
  assert.deepEqual(resolvePhaseTask(undefined, {}), {});
  assert.deepEqual(resolvePhaseTask(row(), {}), {});
});

void test('breadcrumbFor degrades gracefully as parts go missing', () => {
  assert.equal(
    breadcrumbFor('Build Billing Gateway', 'Construction', 'Code Review', 2),
    'Build Billing Gateway › Construction › Code Review · attempt 2'
  );
  assert.equal(
    breadcrumbFor('Build Billing Gateway', undefined, undefined, undefined),
    'Build Billing Gateway'
  );
});

// ---------------------------------------------------------------------------
// The beside-content layout contract (review round 1).
//
// Whether the action bar sits below the fold is invisible to every other gate
// in this repo — typecheck, eslint and 468 tests were all green while the pane
// stretched to 1989px. These assertions pin the three properties that were
// MEASURED to produce the fix, so the mechanism is defended by something
// runnable rather than by a comment.
// ---------------------------------------------------------------------------

void test('sizes the beside-content pane by its own content, never by the column beside it', () => {
  // 1. No explicit height: the 1989px-tall `construction-lens-detail` wrapper
  //    must not propagate into this block child.
  assert.equal('height' in WIDE_PANE_SX, false);
  assert.equal('minHeight' in WIDE_PANE_SX, false);
  // 2. Capped at what fits below the lens toolbar AS MEASURED — the scroller's
  //    own visible height less the toolbar's — so a long body scrolls inside the
  //    pane and the action bar stays on screen at every width. (It was
  //    `calc(100vh - 76px - 16px)`: 100vh ignored the chrome above the scroller.)
  assert.equal(
    WIDE_PANE_SX.maxHeight,
    'calc(var(--lens-scroll-h, 100vh) - max(var(--lens-row-top, 0px), var(--lens-toolbar-h, 0px) + 8px) - 16px)'
  );
  assert.equal(WIDE_PANE_SX.overflow, 'hidden');
  // 3. Pinned in the viewport across the scroll, just below the MEASURED toolbar
  //    — never at a constant offset (a fixed 76px sat under the toolbar once it
  //    wrapped to ~86px at 1280/1366).
  assert.equal(WIDE_PANE_SX.position, 'sticky');
  assert.equal(WIDE_PANE_SX.top, 'calc(var(--lens-toolbar-h, 0px) + 8px)');
});

void test('carries no alignSelf, which would be inert on a non-flex parent', () => {
  // ConstructionShell's `construction-lens-detail` box is a plain block
  // container (`sx={{flexShrink:0}}`), so this pane is not a flex item and
  // `align-self` has no layout effect on it. It stood here for one round
  // claiming to "stop the stretch"; its absence is now asserted so it cannot
  // return as an explanation nobody re-measured.
  assert.equal(Object.keys(WIDE_PANE_SX).includes('alignSelf'), false);
});
