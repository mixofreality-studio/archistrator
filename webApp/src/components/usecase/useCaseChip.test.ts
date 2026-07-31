/// <reference types="node" />
/**
 * Unit tests for useCaseChip: the carousel's per-use-case realization roll-up
 * ("N/M steps realized") and the shared eligibility predicate that decides
 * which activity-node kinds a dynamic view is REQUIRED to realize
 * (action/timeEvent/acceptEvent — mirrors the server's designhealth
 * ccMustHaveStep set / CC-COVERAGE). Decision/switch nodes MAY carry a step
 * but are never "missing" one, so callers exclude them from eligibleNodeIds
 * entirely rather than this module special-casing them.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { realizationChip, isEligibleForRealization } from './useCaseChip.ts';
import type { RealizedStep } from '../../contracts/realization';
import type { Finding } from '../../contracts/types';

function step(nodeId: string): RealizedStep {
  return { nodeId, calls: [{ from: 'a', to: 'b', mode: 'sync', label: 'call' }] };
}

function fnd(ruleId: string): Finding {
  return { ruleId, severity: 'error', message: `${ruleId} fired` };
}

// ── isEligibleForRealization ─────────────────────────────────────────────────

void test('action, timeEvent and acceptEvent are eligible', () => {
  assert.equal(isEligibleForRealization('action'), true);
  assert.equal(isEligibleForRealization('timeEvent'), true);
  assert.equal(isEligibleForRealization('acceptEvent'), true);
});

void test('decision, switch, and every other control-flow kind are not eligible', () => {
  assert.equal(isEligibleForRealization('decision'), false);
  assert.equal(isEligibleForRealization('switch'), false);
  assert.equal(isEligibleForRealization('start'), false);
  assert.equal(isEligibleForRealization('end'), false);
  assert.equal(isEligibleForRealization('merge'), false);
  assert.equal(isEligibleForRealization('fork'), false);
  assert.equal(isEligibleForRealization('join'), false);
});

// ── realizationChip ──────────────────────────────────────────────────────────

void test('every eligible node realized, no findings -> ok, N===M', () => {
  const realization = new Map([
    ['n1', step('n1')],
    ['n2', step('n2')],
  ]);
  const out = realizationChip(realization, ['n1', 'n2'], []);
  assert.deepEqual(out, { label: '2/2 steps realized', tone: 'ok' });
});

void test('a missing step for an eligible node -> warn, N < M', () => {
  const realization = new Map([['n1', step('n1')]]);
  const out = realizationChip(realization, ['n1', 'n2'], []);
  assert.deepEqual(out, { label: '1/2 steps realized', tone: 'warn' });
});

void test('any finding forces error tone even when every node is realized', () => {
  const realization = new Map([
    ['n1', step('n1')],
    ['n2', step('n2')],
  ]);
  const out = realizationChip(realization, ['n1', 'n2'], [fnd('RuleCCActorEdge')]);
  assert.deepEqual(out, { label: '2/2 steps realized', tone: 'error' });
});

void test('a finding takes priority over warn when steps are also missing', () => {
  const realization = new Map([['n1', step('n1')]]);
  const out = realizationChip(realization, ['n1', 'n2'], [fnd('RuleCCStepNode')]);
  assert.deepEqual(out, { label: '1/2 steps realized', tone: 'error' });
});

void test('a decision/switch node kept out of eligibleNodeIds never affects the count', () => {
  const realization = new Map([
    ['n1', step('n1')],
    ['n-decision', step('n-decision')],
  ]);
  const out = realizationChip(realization, ['n1'], []);
  assert.deepEqual(out, { label: '1/1 steps realized', tone: 'ok' });
});

void test('no eligible nodes at all is vacuously ok, 0/0', () => {
  const out = realizationChip(new Map(), [], []);
  assert.deepEqual(out, { label: '0/0 steps realized', tone: 'ok' });
});

void test('an eligible node absent from the realization map counts as missing', () => {
  const out = realizationChip(new Map(), ['n1'], []);
  assert.deepEqual(out, { label: '0/1 steps realized', tone: 'warn' });
});
