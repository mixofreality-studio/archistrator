/**
 * decisionRecords — the console's gate decisions read back from the mutation cache
 * (review C1): what a remounted console knows about a decision it did not see sent.
 */
/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import {
  decisionActivityOf,
  decisionEntriesFrom,
  pendingDecisionActivities,
  type DecisionEntry,
  type DecisionMutationState,
} from './decisionRecords.ts';

/** The one entry every case here is about. */
function only(got: Record<string, DecisionEntry>): DecisionEntry {
  const e = got['C-a:gate'];
  assert.ok(e);
  return e;
}

const item = {
  key: 'C-a:gate',
  reason: 'gate',
  activityId: 'C-a',
  reviewers: [],
  blast: { downstream: 0, downstreamIds: [] },
  why: { rule: 'r', riskFloor: true, tooltip: '' },
};

function vars(over: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    activityId: 'C-a',
    phase: 'detailed_design',
    decision: 'approve',
    occurrence: { key: 'C-a:gate', epoch: 1, snapshot: item },
    ...over,
  };
}

function state(over: Partial<DecisionMutationState>): DecisionMutationState {
  return { status: 'pending', variables: vars(), submittedAt: 100, ...over };
}

void test('a pending decision is on the wire, with no answer yet', () => {
  const got = decisionEntriesFrom([state({})]);
  const e = only(got);
  assert.equal(e.pending, true);
  assert.equal(e.record.sentAt, undefined);
  assert.equal(e.record.epoch, 1);
  assert.equal(e.record.gatedPhase, 'detailed_design');
  // When the human decided: the request's own submit time.
  assert.equal(e.record.decidedAt, 100);
  assert.equal(e.item?.key, 'C-a:gate');
});

void test('an answered decision carries when the server answered', () => {
  const ok = decisionEntriesFrom([state({ status: 'success', data: { answeredAt: 250 } })]);
  assert.equal(only(ok).record.sentAt, 250);
  assert.equal(only(ok).pending, false);
  const failed = decisionEntriesFrom([
    state({ status: 'error', error: { message: 'bad gateway', status: 502, answeredAt: 300 } }),
  ]);
  assert.deepEqual(only(failed).record.error, { status: 502, message: 'bad gateway' });
  assert.equal(only(failed).record.sentAt, 300);
  // A network failure has no status: an unknown outcome, not a rejection.
  const network = decisionEntriesFrom([
    state({ status: 'error', error: { message: 'Failed to fetch', answeredAt: 300 } }),
  ]);
  assert.deepEqual(only(network).record.error, { message: 'Failed to fetch' });
});

void test('the latest decision per key wins, whatever order the cache lists them in', () => {
  const older = state({ status: 'success', data: { answeredAt: 150 }, submittedAt: 100 });
  const newer = state({
    status: 'pending',
    submittedAt: 200,
    variables: vars({ decision: 'sendBack', occurrence: { key: 'C-a:gate', epoch: 2 } }),
  });
  for (const order of [
    [older, newer],
    [newer, older],
  ]) {
    const e = only(decisionEntriesFrom(order));
    assert.equal(e.record.decision, 'sendBack');
    assert.equal(e.record.epoch, 2);
  }
});

void test('foreign or malformed cache entries are ignored, never guessed at', () => {
  const got = decisionEntriesFrom([
    state({ variables: { activityId: 'C-a', phase: 'x', decision: 'approve' } }), // no occurrence
    state({ variables: vars({ decision: 'maybe' }) }),
    state({ status: 'idle' }),
  ]);
  assert.deepEqual(got, {});
  assert.equal(decisionActivityOf(vars()), 'C-a');
  assert.equal(decisionActivityOf({ activityId: 'C-a' }), undefined);
  // A snapshot that does not read as an owed item is dropped; the record stays.
  const noItem = decisionEntriesFrom([
    state({ variables: vars({ occurrence: { key: 'C-a:gate', epoch: 1, snapshot: 'x' } }) }),
  ]);
  assert.equal(only(noItem).item, undefined);
});

void test('an activity is held while ANY of its decisions is on the wire, even one no longer latest (round 2 I1)', () => {
  // The pending decision answered epoch 1; a later decision on the same key (a newer
  // occurrence) supersedes it as the entry, yet the first request is still pending.
  const stillSending = state({ status: 'pending', submittedAt: 100 });
  const newer = state({
    status: 'success',
    data: { answeredAt: 260 },
    submittedAt: 200,
    variables: vars({ occurrence: { key: 'C-a:gate', epoch: 2 } }),
  });
  assert.equal(only(decisionEntriesFrom([stillSending, newer])).pending, false);
  assert.deepEqual([...pendingDecisionActivities([stillSending, newer])], ['C-a']);
  // A pending decision on another activity's round key still names its activity.
  const otherKey = state({
    variables: vars({ activityId: 'C-b', occurrence: { key: 'C-b:designReview:2', epoch: 1 } }),
  });
  assert.deepEqual([...pendingDecisionActivities([otherKey])], ['C-b']);
  // Settled decisions hold nothing; neither do entries that do not read as decisions.
  assert.equal(
    pendingDecisionActivities([
      state({ status: 'success', data: { answeredAt: 1 } }),
      state({ status: 'error', error: { message: 'x' } }),
      state({ variables: { phase: 'x' } }),
    ]).size,
    0
  );
});
