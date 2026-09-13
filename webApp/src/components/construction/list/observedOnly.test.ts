/**
 * "Observed only" (observedOnly.ts): every activity stays; reconstructed evidence
 * is stripped, so a backfilled activity reads NOT STARTED (designer P1-11, spec R6).
 */
/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import type { ConstructionRow, TaskAttemptRow } from '../../../contracts/types.ts';
import { buildActivityTree } from './activityTree.ts';
import { activityRowState } from './activityRowPresentation.ts';
import { worstOriginOf } from '../provenanceAxis.ts';
import {
  evidenceViewFor,
  hiddenInScope,
  observedOnlyRow,
  rowsForEvidenceView,
} from './observedOnly.ts';

function attempt(
  origin: TaskAttemptRow['provenance']['origin'],
  task = 'codeReview'
): TaskAttemptRow {
  return {
    attemptId: `C-x:${task}:1`,
    task,
    phase: 'construction',
    attempt: 1,
    outcome: 'passed',
    evidence: { kind: '', ref: '' },
    provenance: { origin, basis: 'b' },
  };
}

function row(overrides: Partial<ConstructionRow> = {}): ConstructionRow {
  return {
    activityId: 'C-x',
    kind: 'service',
    classified: true,
    hasBuildEvidence: true,
    recorded: true,
    status: 'integrated',
    currentLifecyclePhase: 'integration',
    worstOrigin: 'backfilled',
    phases: [{ phase: 'requirements', weight: 15, label: 'Requirements', completed: true }],
    attempts: [attempt('backfilled', 'srsReview'), attempt('backfilled')],
    ...overrides,
  };
}

void test('a backfilled activity keeps its row and reads NOT STARTED, with no provenance mark', () => {
  const stripped = observedOnlyRow(row());
  assert.deepEqual(stripped.attempts, []);
  assert.deepEqual(stripped.phases, []);
  assert.equal(stripped.hasBuildEvidence, false);
  assert.equal('status' in stripped, false);
  assert.equal('currentLifecyclePhase' in stripped, false);
  assert.equal('worstOrigin' in stripped, false);
  // A stored row still exists; it carries no trusted evidence.
  assert.equal(stripped.recorded, true);

  const [node] = buildActivityTree([stripped]);
  assert.ok(node !== undefined);
  assert.equal(activityRowState(node.row), 'notStarted');
  assert.equal(worstOriginOf(node), 'unknown');
  assert.ok(node.phases.length > 0, 'the profile skeleton still draws');
});

void test('a synthesized attempt is stripped exactly like a backfilled one', () => {
  const stripped = observedOnlyRow(row({ attempts: [attempt('synthesized')] }));
  assert.deepEqual(stripped.attempts, []);
  assert.equal(stripped.hasBuildEvidence, false);
});

void test('a row with nothing to strip is returned as-is', () => {
  const observed = row({ attempts: [attempt('observed')], worstOrigin: 'observed' });
  assert.equal(observedOnlyRow(observed), observed);
  const planned = row({ recorded: false, hasBuildEvidence: false, attempts: [], phases: [] });
  assert.equal(observedOnlyRow(planned), planned);
});

void test('a mixed row keeps its observed attempts and drops what the server derived from the rest', () => {
  const mixed = observedOnlyRow(
    row({ attempts: [attempt('backfilled', 'srsReview'), attempt('observed')] })
  );
  assert.deepEqual(
    mixed.attempts.map((a) => a.provenance.origin),
    ['observed']
  );
  assert.deepEqual(mixed.phases, []);
  assert.equal('status' in mixed, false);
  assert.equal(mixed.worstOrigin, 'observed');
});

// Designer re-check B1: a stripped row is NOT an unrecorded one. The view carries
// what it hid, so the pane can say so.
void test('the evidence view carries the attempts it hid, per activity, and nothing when off', () => {
  const rows = {
    'C-x': row(),
    'C-o': row({ activityId: 'C-o', attempts: [attempt('observed')], worstOrigin: 'observed' }),
    'C-y': row({ activityId: 'C-y', recorded: false, hasBuildEvidence: false, attempts: [] }),
  };
  const view = evidenceViewFor(rows, true);
  assert.deepEqual(Object.keys(view.hidden), ['C-x']);
  assert.equal(view.hidden['C-x']?.length, 2);
  assert.deepEqual(view.rows?.['C-x']?.attempts, []);
  const off = evidenceViewFor(rows, false);
  assert.equal(off.rows, rows);
  assert.deepEqual(off.hidden, {});
});

void test('hiddenInScope counts the selected task, else the phase, else the activity', () => {
  const hidden = [
    attempt('backfilled', 'srsReview'),
    { ...attempt('backfilled', 'srs'), phase: 'requirements' },
    attempt('backfilled', 'codeReview'),
  ];
  assert.equal(hiddenInScope(hidden, {}), 3);
  assert.equal(hiddenInScope(hidden, { lifecyclePhase: 'requirements' }), 1);
  assert.equal(hiddenInScope(hidden, { lifecyclePhase: 'construction' }), 2);
  assert.equal(hiddenInScope(hidden, { lifecyclePhase: 'construction', task: 'codeReview' }), 1);
  assert.equal(hiddenInScope(hidden, { task: 'stp' }), 0);
  assert.equal(hiddenInScope(undefined, {}), 0);
});

void test('the evidence view never changes the row SET — off, it is the identity', () => {
  const rows = { 'C-x': row(), 'C-y': row({ activityId: 'C-y', recorded: false, attempts: [] }) };
  const view = rowsForEvidenceView(rows, true);
  assert.deepEqual(Object.keys(view ?? {}), ['C-x', 'C-y']);
  assert.equal(rowsForEvidenceView(rows, false), rows);
  assert.equal(rowsForEvidenceView(undefined, true), undefined);
});
