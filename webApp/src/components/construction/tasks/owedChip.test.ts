/**
 * owedChip — one owed vocabulary across the lenses and the pane (Q4 / review I4 /
 * designer P0-2), and the review-only rule for steer-needed and failed rows (the
 * PM's must-hold).
 */
/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import {
  OWED_CHIP,
  REVIEW_ONLY_NOTE,
  owedMarksFor,
  owedStateFor,
  reviewOnlyFor,
} from './owedChip.ts';

void test('one word per reason, everywhere — a failure is "Failed"', () => {
  assert.deepEqual(OWED_CHIP.gate, { label: 'Awaiting you', state: 'awaitingHuman' });
  assert.deepEqual(OWED_CHIP.takeover, { label: 'Steer needed', state: 'awaitingHuman' });
  assert.deepEqual(OWED_CHIP.failed, { label: 'Failed', state: 'failed' });
});

void test('the marks carry the gate a decision is on', () => {
  const marks = owedMarksFor([
    {
      activityId: 'C-g',
      reason: 'gate',
      gate: { lifecyclePhase: 'detailed_design', task: 'designReview' },
    },
    { activityId: 'C-s', reason: 'takeover' },
  ]);
  assert.deepEqual(marks.get('C-g'), {
    reason: 'gate',
    gateTask: 'designReview',
    lifecyclePhase: 'detailed_design',
  });
  assert.deepEqual(marks.get('C-s'), { reason: 'takeover' });
});

void test('the pane chip comes from the owed set: the gated activity, phase or task only', () => {
  const gate = {
    reason: 'gate' as const,
    gateTask: 'designReview',
    lifecyclePhase: 'detailed_design',
  };
  assert.equal(owedStateFor(gate, {})?.label, 'Awaiting you');
  assert.equal(owedStateFor(gate, { lifecyclePhase: 'detailed_design' })?.label, 'Awaiting you');
  assert.equal(
    owedStateFor(gate, { lifecyclePhase: 'detailed_design', task: 'designReview' })?.label,
    'Awaiting you'
  );
  assert.equal(owedStateFor(gate, { lifecyclePhase: 'detailed_design', task: 'srs' }), undefined);
  assert.equal(owedStateFor(gate, { lifecyclePhase: 'construction' }), undefined);
  assert.equal(owedStateFor(undefined, {}), undefined);
  // A steer or a failure: the activity itself, not a passed task inside it.
  assert.equal(owedStateFor({ reason: 'takeover' }, {})?.label, 'Steer needed');
  assert.equal(owedStateFor({ reason: 'failed' }, {})?.state, 'failed');
  assert.equal(owedStateFor({ reason: 'failed' }, { lifecyclePhase: 'requirements' }), undefined);
});

void test('steer-needed and failed activities are review-only; a gate is not', () => {
  assert.equal(reviewOnlyFor({ reason: 'takeover' }, {}), true);
  assert.equal(reviewOnlyFor({ reason: 'failed' }, {}), true);
  assert.equal(reviewOnlyFor({ reason: 'failed' }, { task: 'srs' }), false);
  assert.equal(reviewOnlyFor({ reason: 'gate' }, {}), false);
  assert.equal(reviewOnlyFor(undefined, {}), false);
  assert.equal(
    REVIEW_ONLY_NOTE,
    'Retry and re-queue unlock once a verification run shows your note reaching the agent. Until then, steer from GitHub or the MCP override_activity tool.'
  );
});
