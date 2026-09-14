/**
 * steerActions — the PM's Q3 rows (pm-q3-ruling.md), shipped locked behind ONE flag.
 */
/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import type { FailureReason } from '../../../contracts/enums.gen';
import {
  isPlanDefect,
  MAX_VARIANCE_ATTEMPTS,
  PLAN_DEFECT_WARNING,
  REQUEUE_COPY,
  REQUEUE_NOT_BUILT_REASON,
  requeueMetaLine,
  RETRY_COPY,
  retryMetaLine,
  SKIP_COPY,
  STEER_ACTIONS_UNLOCKED,
  STEER_LOCKED_REASON,
  steerBarFor,
} from './steerActions.ts';

void test('the steer actions ship LOCKED: the flag is down', () => {
  assert.equal(STEER_ACTIONS_UNLOCKED, false);
});

void test('locked, a steer row offers Retry… and ⋯ Skip…, both disabled with the locked reason, and Review stays primary', () => {
  const bar = steerBarFor({ reason: 'takeover' });
  assert.ok(bar !== undefined);
  assert.deepEqual(
    bar.actions.map((a) => [a.id, a.label, a.overflow]),
    [
      ['retry', 'Retry…', false],
      ['skip', 'Skip…', true],
    ]
  );
  for (const a of bar.actions) {
    assert.equal(a.disabled, true, a.id);
    assert.equal(a.reason, STEER_LOCKED_REASON, a.id);
  }
  assert.equal(bar.reviewPrimary, true, 'a disabled Retry is never the primary');
  assert.equal(bar.warning, undefined);
});

void test('Takeover and Reassign are cut: they are never offered', () => {
  for (const unlocked of [false, true]) {
    for (const reason of ['takeover', 'failed'] as const) {
      const labels = steerBarFor({ reason }, unlocked)?.actions.map((a) => a.label) ?? [];
      for (const l of labels) assert.doesNotMatch(l, /Takeover|Reassign/);
    }
  }
});

void test('unlocked, Retry is the primary and Skip is enabled; neither carries a reason', () => {
  const bar = steerBarFor({ reason: 'takeover' }, true);
  assert.ok(bar !== undefined);
  for (const a of bar.actions) {
    assert.equal(a.disabled, false, a.id);
    assert.equal(a.reason, undefined, a.id);
  }
  assert.equal(bar.reviewPrimary, false);
});

void test('a failed row offers Re-queue… — locked, and still off once unlocked because no server verb exists', () => {
  const locked = steerBarFor({ reason: 'failed', failure: { reason: 'pipelineFailed' } });
  assert.deepEqual(
    locked?.actions.map((a) => [a.id, a.label, a.disabled, a.reason]),
    [['requeue', 'Re-queue…', true, STEER_LOCKED_REASON]]
  );
  const unlocked = steerBarFor({ reason: 'failed', failure: { reason: 'pipelineFailed' } }, true);
  assert.ok(unlocked !== undefined);
  assert.deepEqual(
    unlocked.actions.map((a) => [a.id, a.disabled, a.reason]),
    [['requeue', true, REQUEUE_NOT_BUILT_REASON]]
  );
  assert.equal(unlocked.reviewPrimary, true);
});

void test('plan-defect failures make Review primary and open Re-queue with the warning', () => {
  const planDefects: FailureReason[] = [
    'componentUnresolved',
    'activityUnclassifiable',
    'dependencyUnresolved',
    'dependencyCycle',
  ];
  for (const reason of planDefects) {
    assert.equal(isPlanDefect(reason), true, reason);
    const bar = steerBarFor({ reason: 'failed', failure: { reason } }, true);
    assert.ok(bar !== undefined, reason);
    assert.equal(bar.reviewPrimary, true, reason);
    assert.equal(bar.warning, PLAN_DEFECT_WARNING, reason);
  }
  for (const reason of [
    'pipelineFailed',
    'pipelineTimedOut',
    'pipelineCancelled',
    'varianceExhausted',
    'escalationTimedOut',
    'unknown',
  ] as const) {
    assert.equal(isPlanDefect(reason), false, reason);
    assert.equal(steerBarFor({ reason: 'failed', failure: { reason } }, true)?.warning, undefined);
  }
  assert.equal(isPlanDefect(undefined), false);
});

void test('a gate has no steer actions', () => {
  assert.equal(steerBarFor({ reason: 'gate' }), undefined);
  assert.equal(steerBarFor({ reason: 'gate' }, true), undefined);
});

void test('the words are the PM’s, verbatim', () => {
  assert.equal(
    PLAN_DEFECT_WARNING,
    'This failed because of the plan, not the agent. Re-queuing before you amend the plan will fail the same way.'
  );
  assert.equal(RETRY_COPY.title('C-x'), 'Retry C-x');
  assert.equal(RETRY_COPY.noteLabel, 'What should the agent do differently?');
  assert.equal(RETRY_COPY.notePlaceholder, 'Required. The agent reads this on its next attempt.');
  assert.equal(RETRY_COPY.button, 'Retry now');
  assert.equal(RETRY_COPY.dispatched(3), 'Retrying — attempt 3 dispatched');
  assert.equal(RETRY_COPY.didNotLand, 'Retry did not land');
  assert.equal(REQUEUE_COPY.title('R-y'), 'Re-queue R-y');
  assert.equal(REQUEUE_COPY.noteLabel, 'What changed since it failed?');
  assert.equal(REQUEUE_COPY.backInLine, 'Back in line — waits for a free worker slot');
  assert.equal(REQUEUE_COPY.started('Construction'), 'Started — now in Construction');
  assert.equal(SKIP_COPY.title('C-x'), 'Skip C-x and mark it done?');
  assert.equal(
    SKIP_COPY.body(4, 1),
    'Nothing will be built for this activity. 4 downstream activities (1 on the critical path) will start as if it were finished.'
  );
  assert.equal(SKIP_COPY.noteLabel, 'Why is it safe to skip?');
  assert.equal(SKIP_COPY.confirm(4), 'Skip and unblock 4');
  assert.equal(SKIP_COPY.cancel, 'Cancel');
});

void test('the meta lines say what is known and "—" for the rest', () => {
  assert.equal(MAX_VARIANCE_ATTEMPTS, 10);
  assert.equal(retryMetaLine(3, 10, 1.5), 'Attempt 3 of 10 · $1.50 spent on this activity so far');
  assert.equal(
    retryMetaLine(undefined, undefined, undefined),
    'Attempt — of 10 · $— spent on this activity so far'
  );
  assert.equal(
    requeueMetaLine('The agent’s run timed out', 2, 0.25),
    'Failed: The agent’s run timed out. 2 attempts · $0.25 spent.'
  );
  assert.equal(requeueMetaLine('x', undefined, undefined), 'Failed: x. — attempts · $— spent.');
});

void test('the locked reasons carry no ticket names', () => {
  for (const words of [STEER_LOCKED_REASON, REQUEUE_NOT_BUILT_REASON]) {
    assert.doesNotMatch(words, /\b[BQ]\d\b|\bP\d-\d\b|\bDC\d+\b/, words);
  }
});
