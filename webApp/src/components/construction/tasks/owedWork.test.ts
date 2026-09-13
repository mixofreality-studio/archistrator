/**
 * owedWork — which decisions the construction pipeline is stopped on, waiting for
 * a human (Stage C Task 1). The owed set is a LIVE fact: the per-activity
 * session's stage, plus a recorded terminal failure. Head-state `in-review` only
 * means "some phases are complete" and never makes a row owed (spec §1 — the old
 * InterventionQueue's defect, pinned first below).
 */
/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import type {
  ConstructionRow,
  ConstructionSessionState,
  ConstructionStage,
  TaskAttemptRow,
} from '../../../contracts/types.ts';
import {
  owedItemsFor,
  owedWorkFor,
  probeCandidatesFor,
  type SessionsByActivity,
} from './owedWork.ts';
import { evidenceViewFor } from '../list/observedOnly.ts';

/** A started, classified, in-review service row; `drop` removes optional fields
 *  outright (exactOptionalPropertyTypes forbids setting them to undefined). */
function row(
  overrides: Partial<ConstructionRow> & { activityId: string },
  ...drop: (keyof ConstructionRow)[]
): ConstructionRow {
  const r: ConstructionRow = {
    kind: 'service',
    classified: true,
    hasBuildEvidence: true,
    recorded: true,
    status: 'in-review',
    currentLifecyclePhase: 'detailed_design',
    startedAt: '2026-09-12T10:00:00Z',
    phases: [],
    attempts: [],
    ...overrides,
  };
  for (const k of drop) Reflect.deleteProperty(r, k);
  return r;
}

function session(
  activityId: string,
  stage: ConstructionStage,
  extra: object = {}
): ConstructionSessionState {
  return {
    projectId: 'p',
    activityId,
    stage,
    view: { projectId: 'p', activityId, stage: 0, ...extra },
  } satisfies ConstructionSessionState;
}

function attempt(task: string, n: number): TaskAttemptRow {
  return {
    attemptId: `C-a:${task}:${String(n)}`,
    task,
    phase: 'detailed_design',
    attempt: n,
    outcome: n === 1 ? 'rejected' : '',
    evidence: { kind: '', ref: '' },
    provenance: { origin: 'observed' },
  };
}

const rowsOf = (...rs: ConstructionRow[]): Record<string, ConstructionRow> =>
  Object.fromEntries(rs.map((r) => [r.activityId, r]));

// --- the §1 defect, pinned ----------------------------------------------------

void test('an in-review row is NOT owed without a session saying a human is awaited', () => {
  const rows = rowsOf(row({ activityId: 'C-a', status: 'in-review' }));
  assert.deepEqual(owedItemsFor({ rows, sessions: {} }), []);
  // …nor while its session says the pipeline is running.
  const running: SessionsByActivity = { 'C-a': session('C-a', 'pipelineRunning') };
  assert.deepEqual(owedItemsFor({ rows, sessions: running }), []);
  // …nor when the probe established that no session exists (dormant pump).
  assert.deepEqual(owedItemsFor({ rows, sessions: { 'C-a': null } }), []);
});

// --- gates ---------------------------------------------------------------------

void test('a session at awaitingApproval is owed, gated on the profile phase gate task', () => {
  const rows = rowsOf(row({ activityId: 'C-a' }));
  const reviewers = [{ role: 'system-architect', perspective: 'architecture', mayAmend: true }];
  const [item] = owedItemsFor({
    rows,
    sessions: { 'C-a': session('C-a', 'awaitingApproval', { reviewSet: { reviewers } }) },
    titleFor: (id) => (id === 'C-a' ? 'Billing Engine' : undefined),
  });
  assert.ok(item);
  assert.equal(item.reason, 'gate');
  assert.equal(item.title, 'Billing Engine');
  const gate = item.gate;
  assert.ok(gate);
  assert.equal(gate.lifecyclePhase, 'detailed_design');
  // The profile's gate task for a service's Detailed Design is Design Review.
  assert.equal(gate.task, 'designReview');
  assert.equal(gate.phaseName, 'Detailed Design');
  assert.ok((gate.exitCriterion ?? '').length > 0);
  assert.deepEqual(item.reviewers, reviewers);
});

void test('a gate on an unclassified row names its phase but never a guessed task', () => {
  // An unclassified row carries no kind, status or current phase on the wire.
  const rows = rowsOf(
    row({ activityId: 'X-1', classified: false }, 'kind', 'status', 'currentLifecyclePhase')
  );
  const [item] = owedItemsFor({ rows, sessions: { 'X-1': session('X-1', 'awaitingApproval') } });
  assert.ok(item);
  assert.equal(item.reason, 'gate');
  assert.equal(item.gate?.task, undefined);
  assert.equal(item.round, undefined);
  assert.equal(item.key, 'X-1:gate');
});

void test('the round is the gate task ledger count, and absent on an empty ledger', () => {
  const retried = rowsOf(
    row({
      activityId: 'C-a',
      attempts: [
        attempt('designReview', 1),
        attempt('detailedDesign', 2),
        attempt('designReview', 2),
      ],
    })
  );
  const [item] = owedItemsFor({
    rows: retried,
    sessions: { 'C-a': session('C-a', 'awaitingApproval') },
  });
  assert.ok(item);
  assert.equal(item.round, 2);
  assert.equal(item.key, 'C-a:designReview:2');

  const [fresh] = owedItemsFor({
    rows: rowsOf(row({ activityId: 'C-a' })),
    sessions: { 'C-a': session('C-a', 'awaitingApproval') },
  });
  assert.ok(fresh);
  assert.equal(fresh.round, undefined);
  assert.equal(fresh.key, 'C-a:gate');
});

// --- the machine stopped -------------------------------------------------------

void test('awaitingTakeover is owed and carries the variance summary', () => {
  const [item] = owedItemsFor({
    rows: rowsOf(row({ activityId: 'C-a', status: 'in-construction' })),
    sessions: {
      'C-a': session('C-a', 'awaitingTakeover', {
        variance: { projectId: 'p', activityId: 'C-a', summary: 'retry budget exhausted' },
      }),
    },
  });
  assert.ok(item);
  assert.equal(item.reason, 'takeover');
  assert.equal(item.variance, 'retry budget exhausted');
  assert.equal(item.gate, undefined);
});

void test('a recorded failure is owed with no session at all', () => {
  const [item] = owedItemsFor({
    rows: rowsOf(
      row({
        activityId: 'C-f',
        status: 'failed',
        failureReason: 'varianceExhausted',
        failureDetail: 'three retries failed the build',
      })
    ),
    sessions: {},
  });
  assert.ok(item);
  assert.equal(item.reason, 'failed');
  assert.deepEqual(item.failure, {
    reason: 'varianceExhausted',
    detail: 'three retries failed the build',
  });
  assert.equal(item.key, 'C-f:failed');
});

void test('owed items come back in activity-id order (ranking is Task 2)', () => {
  const rows = rowsOf(row({ activityId: 'C-b' }), row({ activityId: 'C-a' }));
  const items = owedItemsFor({
    rows,
    sessions: {
      'C-a': session('C-a', 'awaitingApproval'),
      'C-b': session('C-b', 'awaitingApproval'),
    },
  });
  assert.deepEqual(
    items.map((i) => i.activityId),
    ['C-a', 'C-b']
  );
});

// --- a probe that has not answered is not an answer (architect Q1) --------------

void test('an errored probe counts as unchecked, never as nothing owed', () => {
  const work = owedWorkFor({
    rows: rowsOf(row({ activityId: 'C-err' })),
    sessions: {}, // the probe failed: no view, no 404
    erroredProbes: ['C-err'],
  });
  assert.deepEqual(work.items, []);
  assert.deepEqual(work.unchecked, { pending: [], errored: ['C-err'] });
});

void test('a pending probe is unchecked; an established absence is clear; an unasked row never is', () => {
  const rows = rowsOf(
    row({ activityId: 'C-pending' }),
    row({ activityId: 'C-absent' }),
    row({ activityId: 'C-running' }),
    // Backfilled: never started by a pump, so never probed — not "unchecked".
    row({ activityId: 'C-backfilled', status: 'integrated' }, 'startedAt')
  );
  const work = owedWorkFor({
    rows,
    sessions: { 'C-absent': null, 'C-running': session('C-running', 'pipelineRunning') },
  });
  assert.deepEqual(work.items, []);
  assert.deepEqual(work.unchecked, { pending: ['C-pending'], errored: [] });
});

// --- which sessions are worth probing ------------------------------------------

void test('probe only what the pump started and has not finished', () => {
  const rows = rowsOf(
    row({ activityId: 'C-live' }),
    row({ activityId: 'C-done', completedAt: '2026-09-12T11:00:00Z', status: 'integrated' }),
    row({ activityId: 'C-failed', status: 'failed', failureReason: 'pipelineFailed' }),
    // Backfilled: integrated from reconstructed evidence, never started by a pump.
    row({ activityId: 'C-backfilled', status: 'integrated' }, 'startedAt'),
    // Planned-no-record: the committed list names it; nothing is stored.
    row({ activityId: 'N-IT', recorded: false }, 'startedAt', 'status')
  );
  assert.deepEqual(probeCandidatesFor(rows), ['C-live']);
  assert.deepEqual(probeCandidatesFor(undefined), []);
});

// --- "Observed only" never removes an owed decision (plan DC11) ----------------

void test('the evidence view keeps the owed set and the probe set whole', () => {
  const backfilled: TaskAttemptRow = {
    ...attempt('srs', 1),
    outcome: 'passed',
    provenance: { origin: 'backfilled' },
  };
  // A live gate and a recorded failure, each on a row carrying reconstructed
  // attempts — the rows "Observed only" strips hardest.
  const rows = rowsOf(
    row({ activityId: 'C-a', attempts: [backfilled] }),
    row({
      activityId: 'C-f',
      status: 'failed',
      failureReason: 'pipelineFailed',
      attempts: [backfilled],
    })
  );
  const sessions: SessionsByActivity = { 'C-a': session('C-a', 'awaitingApproval') };
  const view = evidenceViewFor(rows, true).rows;
  const whole = owedItemsFor({ rows, sessions }).map((i) => i.key);
  const viewed = owedItemsFor({ rows: view, sessions }).map((i) => i.key);
  assert.deepEqual(viewed, whole);
  assert.deepEqual(probeCandidatesFor(view), probeCandidatesFor(rows));
});
