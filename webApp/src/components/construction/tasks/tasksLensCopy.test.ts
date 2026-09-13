/**
 * tasksLensCopy — every sentence the TASKS lens says, as pure functions over the
 * owed set (Stage C Task 4). The lens must never say more than it knows: no
 * waiting time it was not given, no policy the project did not record.
 */
/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import type { BuildStatus } from '../../../contracts/constructionAdapters.ts';
import type { FailureReason } from '../../../contracts/enums.gen';
import type { RankedOwed } from './owedRanking.ts';
import {
  allClearHeadlineFor,
  askFor,
  ciVerdictFor,
  uncheckedErroredLine,
  uncheckedPendingLine,
  emptyStateCounts,
  emptyStateLine,
  filteredLineFor,
  HEADLINE_TOOLTIP,
  headlineFor,
  policyBannerFor,
  policySummaryFor,
  reasonSentenceFor,
  roundLabel,
  SET_POLICY_LABEL,
  shapeFor,
  slotsLineFor,
  whyAffordanceFor,
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

void test('the degraded banner is the designer one-liner while no policy is recorded (§7.7 amended)', () => {
  const none = policyBannerFor(undefined);
  assert.deepEqual(none, {
    title: 'No review policy recorded.',
    body: 'Only the risk floor is gated — changes touching deploy, spend or schema always ask you; everything else proceeds without asking.',
  });
  assert.equal(SET_POLICY_LABEL, 'Set a review policy →');
  // What the PM's sentence also said lives in the headline's tooltip.
  assert.equal(
    HEADLINE_TOOLTIP,
    'Failures and escalations show here under any policy. The rows come from each running activity’s live workflow stage.'
  );
  assert.ok(policyBannerFor({ gatedPhasesByType: {} }) !== undefined);
  assert.equal(policyBannerFor({ gatedPhasesByType: {}, preset: 'checkpoints' }), undefined);
  assert.equal(policyBannerFor({ gatedPhasesByType: { service: ['integration'] } }), undefined);
});

void test('"stop asking" only where a policy rule opened the gate; the risk floor cannot be turned off', () => {
  const policyGate = ranked('A', [], { why: { rule: 'Preset', riskFloor: false, tooltip: '' } });
  const floorGate = ranked('B', [], { why: { rule: 'Risk floor', riskFloor: true, tooltip: '' } });
  assert.equal(whyAffordanceFor(policyGate), 'stopAsking');
  assert.equal(whyAffordanceFor(floorGate), 'cantTurnOff');
  assert.equal(whyAffordanceFor(ranked('S', [], { reason: 'takeover' })), undefined);
  assert.equal(whyAffordanceFor(ranked('F', [], { reason: 'failed' })), undefined);
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
  // The PM's copy (pm-q3-ruling): the reason as a plain sentence, then the cause.
  assert.equal(askFor(takeover), 'Stopped and asking you how to proceed — retry budget exhausted.');
  const failed = ranked('C-f', [], {
    reason: 'failed',
    failure: { reason: 'pipelineTimedOut', detail: 'the build ran past 30m' },
  });
  assert.equal(askFor(failed), 'The agent’s run timed out — the build ran past 30m.');
});

void test('every recorded cause says why in the PM’s words; a gate has no reason sentence', () => {
  const said = (
    reason: Parameters<typeof reasonSentenceFor>[0]['reason'],
    extra = {}
  ): string | undefined => reasonSentenceFor({ reason, ...extra });
  assert.equal(said('takeover'), 'Stopped and asking you how to proceed');
  assert.equal(said('gate'), undefined);
  const cause = (r: FailureReason): string | undefined =>
    reasonSentenceFor({ reason: 'failed', failure: { reason: r } });
  assert.equal(cause('pipelineFailed'), 'The agent’s run failed');
  assert.equal(cause('pipelineCancelled'), 'The run was cancelled');
  assert.equal(cause('varianceExhausted'), 'Gave up after 10 attempts');
  assert.equal(cause('escalationTimedOut'), 'No one answered the escalation in time');
  assert.equal(cause('componentUnresolved'), 'The plan names a component that isn’t in the design');
  assert.equal(cause('activityUnclassifiable'), 'The plan gives this activity no buildable type');
  assert.equal(cause('dependencyUnresolved'), 'The plan depends on an activity that doesn’t exist');
  assert.equal(cause('dependencyCycle'), 'The plan has a dependency loop');
  assert.match(cause('unknown') ?? '', /not recorded/);
});

void test('the headline says when the toolbar hides owed decisions (review I4)', () => {
  assert.equal(filteredLineFor(1, 3), '1 shown · 3 owed');
  assert.equal(filteredLineFor(3, 3), undefined);
  assert.equal(filteredLineFor(0, 0), undefined);
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
