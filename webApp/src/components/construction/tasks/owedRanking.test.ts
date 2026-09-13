/**
 * owedRanking — what each owed decision costs to leave, which rule opened it, and
 * the order the TASKS lens lists them in (Stage C Task 2).
 */
/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import type { NetworkModel, ReviewPolicyView } from '../../../contracts/types.ts';
import type { OwedItem } from './owedWork.ts';
import { downstreamOf, rankOwed, whyFor } from './owedRanking.ts';

function net(
  deps: [string, string[]][],
  milestones: { id: string; dependsOn: string[] }[] = [],
  computed: NetworkModel['computed'] = {}
): NetworkModel {
  return {
    criticalPath: null,
    dependencies: deps.map(([activity, dependsOn]) => ({ activity, dependsOn })),
    milestones: milestones.map((m) => ({ ...m, name: m.id, public: false })),
    computed,
  };
}

function gateItem(
  activityId: string,
  lifecyclePhase: string,
  kind: NonNullable<OwedItem['kind']> = 'service'
): OwedItem {
  return {
    key: `${activityId}:gate`,
    reason: 'gate',
    activityId,
    kind,
    gate: { lifecyclePhase },
    reviewers: [],
  };
}

// --- blast radius ----------------------------------------------------------------

void test('blast radius is every activity transitively waiting, milestones traversed not counted', () => {
  // A → M1 → B → C, and A → D directly.
  const network = net(
    [
      ['B', ['M1']],
      ['C', ['B']],
      ['D', ['A']],
      ['A', []],
    ],
    [{ id: 'M1', dependsOn: ['A'] }]
  );
  assert.deepEqual(downstreamOf(network, 'A', new Set()), ['B', 'C', 'D']);
});

void test('a diamond counts each downstream activity once', () => {
  const network = net([
    ['B', ['A']],
    ['C', ['A']],
    ['D', ['B', 'C']],
  ]);
  assert.deepEqual(downstreamOf(network, 'A', new Set()), ['B', 'C', 'D']);
});

void test('an activity already done is not waiting, and does not carry the wait past it', () => {
  // A → B → C with B integrated: C's dependency on B is satisfied, so C is not
  // blocked by A; B is not waiting either.
  const network = net([
    ['B', ['A']],
    ['C', ['B']],
  ]);
  assert.deepEqual(downstreamOf(network, 'A', new Set(['B'])), []);
});

void test('an activity absent from the network has no blast radius, not an error', () => {
  assert.deepEqual(downstreamOf(net([]), 'A', new Set()), []);
});

// --- WHY: which rule opened the gate ---------------------------------------------

const empty: ReviewPolicyView = { gatedPhasesByType: {} };

void test('with no policy recorded, an open gate can only be the risk floor', () => {
  const why = whyFor(gateItem('C-a', 'construction'), undefined);
  assert.equal(why.riskFloor, true);
  assert.match(why.rule, /risk floor/i);
  assert.equal(whyFor(gateItem('C-a', 'construction'), empty).riskFloor, true);
});

void test('an explicit map entry for (kind, phase) attributes the gate to the policy', () => {
  const policy: ReviewPolicyView = { gatedPhasesByType: { service: ['detailed_design'] } };
  const why = whyFor(gateItem('C-a', 'detailed_design'), policy);
  assert.equal(why.riskFloor, false);
  assert.match(why.rule, /Service/);
  assert.match(why.rule, /Detailed Design/);
  // The same map says nothing about a frontend at that phase.
  assert.equal(whyFor(gateItem('U-x', 'detailed_design', 'frontend'), policy).riskFloor, true);
});

void test('presets attribute per the server EffectiveGate switch', () => {
  const checkpoints: ReviewPolicyView = { gatedPhasesByType: {}, preset: 'checkpoints' };
  assert.equal(whyFor(gateItem('C-a', 'detailed_design'), checkpoints).riskFloor, false);
  assert.match(whyFor(gateItem('C-a', 'detailed_design'), checkpoints).rule, /checkpoints/);
  // checkpoints does not gate the test plan — an open test-plan gate is the floor.
  assert.equal(whyFor(gateItem('C-a', 'test_plan'), checkpoints).riskFloor, true);
  const full: ReviewPolicyView = { gatedPhasesByType: {}, preset: 'full' };
  assert.equal(whyFor(gateItem('C-a', 'test_plan'), full).riskFloor, false);
  // vibes gates nothing; the floor is all that is left.
  const vibes: ReviewPolicyView = {
    gatedPhasesByType: { service: ['test_plan'] },
    preset: 'vibes',
  };
  assert.equal(whyFor(gateItem('C-a', 'test_plan'), vibes).riskFloor, true);
});

void test('takeover and failed rows name the machine, never the floor', () => {
  const takeover: OwedItem = {
    key: 'C-a:takeover',
    reason: 'takeover',
    activityId: 'C-a',
    reviewers: [],
  };
  const failed: OwedItem = {
    key: 'C-f:failed',
    reason: 'failed',
    activityId: 'C-f',
    reviewers: [],
    failure: { reason: 'pipelineTimedOut' },
  };
  assert.equal(whyFor(takeover, undefined).riskFloor, false);
  assert.match(whyFor(takeover, undefined).rule, /variance/i);
  assert.equal(whyFor(failed, undefined).riskFloor, false);
  assert.match(whyFor(failed, undefined).rule, /timed out/i);
});

// --- the default order -------------------------------------------------------------

void test('risk floor first, then blast radius descending, then activity id', () => {
  const network = net(
    [
      ['X1', ['BIG']],
      ['X2', ['BIG']],
      ['X3', ['BIG']],
      ['Y1', ['MID']],
      ['Y2', ['MID']],
    ],
    [],
    {
      BIG: {
        band: 'critical',
        column: 0,
        earliestFinish: 5,
        earliestStart: 0,
        freeFloat: 0,
        latestFinish: 5,
        latestStart: 0,
        nearCritical: false,
        onCriticalPath: true,
        totalFloat: 0,
      },
    }
  );
  const policy: ReviewPolicyView = { gatedPhasesByType: { service: ['detailed_design'] } };
  const items = [
    gateItem('BIG', 'detailed_design'), // policy gate, blast 3
    gateItem('MID', 'detailed_design'), // policy gate, blast 2
    gateItem('ZZZ', 'construction'), // floor gate, blast 0
    gateItem('AAA', 'detailed_design'), // policy gate, blast 0
  ];
  const ranked = rankOwed(items, { network, done: new Set(), policy });
  assert.deepEqual(
    ranked.map((r) => r.activityId),
    ['ZZZ', 'BIG', 'MID', 'AAA']
  );
  const big = ranked.find((r) => r.activityId === 'BIG');
  assert.ok(big);
  assert.equal(big.blast.downstream, 3);
  assert.equal(big.blast.onCriticalPath, true);
  assert.equal(big.blast.float, 0);
  // No CPM entry → no float and no critical-path claim, never a fabricated 0.
  const aaa = ranked.find((r) => r.activityId === 'AAA');
  assert.ok(aaa);
  assert.equal('float' in aaa.blast, false);
  assert.equal('onCriticalPath' in aaa.blast, false);
});
