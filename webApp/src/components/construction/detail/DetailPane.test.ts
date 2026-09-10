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
  resolvePhaseTask,
  taskDetailStateFor,
} from './detailPaneState.ts';

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

// ---------------------------------------------------------------------------
// The two invariants (brief steps, verbatim)
// ---------------------------------------------------------------------------

void test('enables the retry action in every task state', () => {
  for (const state of [
    'unknown',
    'notStarted',
    'running',
    'awaitingHuman',
    'passed',
    'failed',
  ] as const) {
    const actions = detailActionsFor(state);
    const retry = actions.find((a) => a.id === 'run');
    assert.ok(retry !== undefined, `no retry action for ${state}`);
    assert.equal(retry.disabled, false, `retry disabled for ${state}`);
  }
});

void test('offers approve and send-back only where a human decision is owed', () => {
  assert.ok(
    detailActionsFor('awaitingHuman')
      .map((a) => a.id)
      .includes('approve')
  );
  assert.equal(
    detailActionsFor('passed')
      .map((a) => a.id)
      .includes('approve'),
    false
  );
});

// ---------------------------------------------------------------------------
// taskDetailStateFor
// ---------------------------------------------------------------------------

void test('a selected task with no attempt at all is unknown, not notStarted', () => {
  const r = row({ status: 'in-construction', attempts: [] });
  const selection: LensSelection = { activityId: 'C-x', task: 'codeReview' };
  assert.equal(taskDetailStateFor(r, selection), 'unknown');
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
