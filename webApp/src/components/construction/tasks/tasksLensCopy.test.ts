/**
 * tasksLensCopy — every sentence the TASKS lens says, as pure functions over the
 * owed set (Stage C Task 4). The lens must never say more than it knows: no
 * waiting time it was not given, no policy the project did not record.
 */
/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import type { BuildStatus } from '../../../contracts/constructionAdapters.ts';
import type { RankedOwed } from './owedRanking.ts';
import {
  allClearHeadlineFor,
  askFor,
  ciVerdictFor,
  uncheckedErroredLine,
  uncheckedPendingLine,
  emptyStateCounts,
  emptyStateLine,
  headlineFor,
  policyBannerFor,
  policySummaryFor,
  roundLabel,
  shapeFor,
  slotsLineFor,
} from './tasksLensCopy.ts';

function ranked(
  activityId: string,
  downstreamIds: string[],
  extra: Partial<RankedOwed> = {}
): RankedOwed {
  return {
    key: `${activityId}:gate`,
    reason: 'gate',
    activityId,
    gate: { lifecyclePhase: 'detailed_design' },
    reviewers: [],
    blast: { downstream: downstreamIds.length, downstreamIds },
    why: { rule: 'r', riskFloor: false, tooltip: '' },
    ...extra,
  };
}

// --- the header ----------------------------------------------------------------

void test('the headline counts the UNION of what is waiting, and the critical-path share', () => {
  const items = [
    ranked('A', ['X', 'Y'], {
      blast: { downstream: 2, downstreamIds: ['X', 'Y'], onCriticalPath: true },
    }),
    ranked('B', ['Y', 'Z']),
  ];
  // X, Y, Z — Y is waiting on both and is counted once.
  assert.equal(
    headlineFor(items),
    '2 decisions are blocking 3 downstream activities · 1 on the critical path'
  );
  assert.equal(headlineFor([ranked('A', ['X'])]), '1 decision is blocking 1 downstream activity');
  assert.equal(headlineFor([ranked('A', [])]), '1 decision is blocking 0 downstream activities');
});

void test('the worker-slot line appears only when a gate holds a slot and the cap is known', () => {
  assert.equal(slotsLineFor([ranked('A', [])], 3), '1 of 3 worker slots stopped at a gate');
  assert.equal(slotsLineFor([ranked('A', [])], undefined), undefined);
  const failed = ranked('F', [], { reason: 'failed', key: 'F:failed' });
  assert.equal(slotsLineFor([failed], 3), undefined);
});

// --- the policy ------------------------------------------------------------------

void test('the degraded banner stands while no policy is recorded, and says what the server does', () => {
  const none = policyBannerFor(undefined);
  assert.ok(none !== undefined);
  assert.match(none, /No review policy recorded/);
  assert.match(none, /risk floor/);
  // The spec's old wording described a queue built from head-state status — false here.
  assert.doesNotMatch(none, /derived from activity status/);
  assert.ok(policyBannerFor({ gatedPhasesByType: {} }) !== undefined);
  assert.equal(policyBannerFor({ gatedPhasesByType: {}, preset: 'checkpoints' }), undefined);
  assert.equal(policyBannerFor({ gatedPhasesByType: { service: ['integration'] } }), undefined);
});

void test('the policy summary names what is gated, and always the floor', () => {
  assert.match(policySummaryFor(undefined), /Only the risk floor/);
  const preset = policySummaryFor({ gatedPhasesByType: {}, preset: 'checkpoints' });
  assert.match(preset, /checkpoints/);
  assert.match(preset, /Detailed Design/);
  assert.match(preset, /risk floor/);
  const explicit = policySummaryFor({
    gatedPhasesByType: { service: ['detailed_design', 'integration'], testing: ['test_plan'] },
  });
  assert.match(explicit, /Service › Detailed Design, Integration/);
  assert.match(explicit, /Testing › Test Plan/);
});

// --- what could not be checked (architect Q1) ----------------------------------

void test('the all-clear headline is suppressed while any probe is unchecked', () => {
  assert.equal(allClearHeadlineFor({ pending: 0, errored: 0 }), 'Nothing needs you.');
  assert.equal(allClearHeadlineFor({ pending: 0, errored: 0 }, true), 'Nothing else needs you.');
  assert.equal(allClearHeadlineFor({ pending: 1, errored: 0 }), undefined);
  assert.equal(allClearHeadlineFor({ pending: 0, errored: 2 }), undefined);
  assert.equal(allClearHeadlineFor({ pending: 0, errored: 1 }, true), undefined);
});

void test('the unchecked lines say how many, and nothing when none', () => {
  assert.equal(uncheckedPendingLine(1), 'Checking 1 in-flight activity…');
  assert.equal(uncheckedPendingLine(3), 'Checking 3 in-flight activities…');
  assert.equal(uncheckedPendingLine(0), undefined);
  assert.equal(uncheckedErroredLine(2), "Couldn't check 2 in-flight activities");
  assert.equal(uncheckedErroredLine(0), undefined);
});

// --- the empty state -------------------------------------------------------------

void test('the empty state counts come from the statuses it is handed', () => {
  const statuses = new Map<string, BuildStatus>([
    ['A', 'eligible'],
    ['B', 'eligible'],
    ['C', 'blocked'],
    ['D', 'integrated'],
  ]);
  const counts = emptyStateCounts(statuses, 1);
  assert.deepEqual(counts, { eligible: 2, inFlight: 1, blocked: 1 });
  assert.equal(emptyStateLine(counts), '2 eligible · 1 in flight · 1 blocked');
  // A different split moves the numbers — nothing is hardcoded.
  assert.deepEqual(emptyStateCounts(new Map([['A', 'blocked']]), 0), {
    eligible: 0,
    inFlight: 0,
    blocked: 1,
  });
});

// --- the row's triage fields -----------------------------------------------------

void test('the ask is a sentence about the artifact, from the profile exit criterion', () => {
  const gate = ranked('C-a', [], {
    gate: {
      lifecyclePhase: 'detailed_design',
      phaseName: 'Detailed Design',
      task: 'designReview',
      label: 'Design Review',
      exitCriterion: 'The service contract is designed and passes design review',
    },
  });
  assert.equal(
    askFor(gate),
    'Decide whether the service contract is designed and passes design review.'
  );
  assert.match(
    askFor(ranked('C-a', [], { gate: { lifecyclePhase: 'integration' } })),
    /integration/i
  );
  assert.match(askFor(ranked('C-a', [], { gate: {} })), /reports no current phase/);
  const takeover = ranked('C-t', [], {
    reason: 'takeover',
    title: 'Billing Engine',
    variance: 'retry budget exhausted',
  });
  assert.equal(askFor(takeover), 'Steer Billing Engine: retry budget exhausted.');
  const failed = ranked('C-f', [], {
    reason: 'failed',
    failure: { reason: 'pipelineTimedOut', detail: 'the build ran past 30m' },
  });
  assert.match(askFor(failed), /C-f stopped: the build ran past 30m/);
});

void test('round and CI never guess', () => {
  assert.equal(roundLabel(2), 'round 2');
  assert.equal(roundLabel(undefined), 'round —');
  assert.deepEqual(ciVerdictFor('failed'), {
    label: "CI red — don't read the diff yet",
    tone: 'danger',
  });
  assert.equal(ciVerdictFor(undefined).label, 'no CI record');
  assert.equal(ciVerdictFor(undefined).tone, 'muted');
});

void test('the shape of what you are about to read, or nothing', () => {
  assert.equal(shapeFor('service', { contractOps: 12 }), 'Contract · 12 ops');
  assert.equal(shapeFor('service', { contractOps: 1 }), 'Contract · 1 op');
  assert.equal(shapeFor('testing', { scenarios: 5 }), 'Test plan · 5 scenarios');
  assert.equal(shapeFor('service', {}), '—');
  assert.equal(shapeFor(undefined, { contractOps: 3 }), '—');
});
