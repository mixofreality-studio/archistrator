import { test } from 'node:test';
import assert from 'node:assert/strict';
import type { ConstructionRow, TaskAttemptRow } from '../../../contracts/types.ts';
import { selectionSummaryFor } from './detailPaneState.ts';

function attempt(task: string, phase: string): TaskAttemptRow {
  return {
    attemptId: `C-x:${task}:1`,
    task,
    phase,
    attempt: 1,
    outcome: 'passed',
    evidence: { kind: '', ref: '' },
    provenance: { origin: 'backfilled', generator: 'g', basis: 'b' },
  };
}

function serviceRow(attempts: TaskAttemptRow[]): ConstructionRow {
  return {
    activityId: 'C-x',
    kind: 'service',
    classified: true,
    hasBuildEvidence: attempts.length > 0,
    recorded: attempts.length > 0,
    phases: [],
    attempts,
  };
}

void test('an activity selection reads "N attempts · M phases", never "NO ATTEMPTS" beside a PASSED chip', () => {
  const row = serviceRow([
    attempt('srs', 'requirements'),
    attempt('srsReview', 'requirements'),
    attempt('construction', 'construction'),
  ]);
  assert.equal(selectionSummaryFor(row, { activityId: 'C-x' }), '3 attempts · 5 phases');
});

void test('a phase selection counts that phase’s attempts and the tasks it renders', () => {
  const row = serviceRow([attempt('srs', 'requirements'), attempt('construction', 'construction')]);
  assert.equal(
    selectionSummaryFor(row, { activityId: 'C-x', lifecyclePhase: 'requirements' }),
    '1 attempt · 2 tasks'
  );
});

void test('a conditional task counts only once an attempt makes it render', () => {
  const plain = serviceRow([]);
  const withTestClient = serviceRow([attempt('testClient', 'construction')]);
  const sel = { activityId: 'C-x', lifecyclePhase: 'construction' };
  const without = selectionSummaryFor(plain, sel);
  const withIt = selectionSummaryFor(withTestClient, sel);
  assert.ok(without !== undefined && withIt !== undefined);
  const tasksOf = (s: string): number => Number(/· (\d+) tasks?/.exec(s)?.[1]);
  assert.equal(tasksOf(withIt), tasksOf(without) + 1);
});

void test('a task selection has no summary — its attempt selector speaks for it', () => {
  const row = serviceRow([attempt('srs', 'requirements')]);
  assert.equal(
    selectionSummaryFor(row, { activityId: 'C-x', lifecyclePhase: 'requirements', task: 'srs' }),
    undefined
  );
  assert.equal(selectionSummaryFor(undefined, { activityId: 'C-x' }), undefined);
});
