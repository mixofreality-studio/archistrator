/// <reference types="node" />
/**
 * Unit tests for the pure CC-finding -> per-call status join
 * (src/components/flow/callStatus.ts), which tints the Architecture dynamic
 * lens: every call of a step designhealth flagged goes RED, every call of a
 * clean (realized) step goes GREEN.
 *
 * Fixture sections mirror the designhealth CC-* section grammar exactly as
 * landed in Task 7 (server/internal/utility/designhealth/rules_callchain.go):
 *
 *   "dynamicView <key>"                   - view-scoped (CC-VIEW-USECASE)
 *   "dynamicView <key> step <nodeId>"     - step-scoped
 *
 * The label is always the view KEY, never its display title.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { statusBySeqFromFindings } from './callStatus.ts';
import type { DynamicViewModel, SequencedCall } from '../../contracts/adapters';
import type { Finding, Severity } from '../../contracts/types';

function fnd(ruleId: string, severity: Severity, section: string): Finding {
  return { ruleId, severity, message: `${ruleId} fired`, location: { ordinal: 0, section } };
}

/** One call of `stepNodeId`, at global position `seq`. */
function call(seq: number, stepNodeId: string, callInStep = 1, callsInStep = 1): SequencedCall {
  return {
    from: 'a',
    to: 'b',
    mode: 'sync',
    label: `call ${String(seq)}`,
    seq,
    stepNodeId,
    stepLabel: stepNodeId,
    callInStep,
    callsInStep,
  };
}

function dv(edges: SequencedCall[]): DynamicViewModel {
  return { title: 'Order', participants: [], persons: [], edges, unresolved: [] };
}

// n-validate authors two calls, n-charge one, n-notify one.
const VIEW = dv([call(1, 'n-validate', 1, 2), call(2, 'n-validate', 2, 2), call(3, 'n-charge')]);

const chargeFinding = fnd('RuleChainEntryManager', 'warning', 'dynamicView dv-order step n-charge');
const validateFinding = fnd('RuleCCActorEdge', 'error', 'dynamicView dv-order step n-validate');
const viewScopedFinding = fnd('RuleCCViewUseCase', 'warning', 'dynamicView dv-order');
const otherViewFinding = fnd('RuleCCActorEdge', 'error', 'dynamicView dv-track step n-charge');

void test('a clean step tints every one of its calls green', () => {
  const out = statusBySeqFromFindings(VIEW, [chargeFinding], 'dv-order');
  assert.equal(out.get(1), 'green');
  assert.equal(out.get(2), 'green');
});

void test('a flagged step tints EVERY call of that step red', () => {
  const out = statusBySeqFromFindings(VIEW, [validateFinding], 'dv-order');
  assert.equal(out.get(1), 'red');
  assert.equal(out.get(2), 'red');
  assert.equal(out.get(3), 'green');
});

void test('status is keyed by the call GLOBAL seq, one entry per call', () => {
  const out = statusBySeqFromFindings(VIEW, [chargeFinding], 'dv-order');
  assert.deepEqual([...out.keys()], [1, 2, 3]);
  assert.equal(out.get(3), 'red');
});

void test('with no findings at all every realized call is green', () => {
  const out = statusBySeqFromFindings(VIEW, [], 'dv-order');
  assert.deepEqual([...out.values()], ['green', 'green', 'green']);
});

void test('a view-scoped finding tints no individual step red', () => {
  const out = statusBySeqFromFindings(VIEW, [viewScopedFinding], 'dv-order');
  assert.deepEqual([...out.values()], ['green', 'green', 'green']);
});

void test('a finding on the same step id under ANOTHER view never tints', () => {
  const out = statusBySeqFromFindings(VIEW, [otherViewFinding], 'dv-order');
  assert.equal(out.get(3), 'green');
});

void test('a call with no owning step is left untinted (absent from the map)', () => {
  const out = statusBySeqFromFindings(dv([call(1, ''), call(2, 'n-charge')]), [], 'dv-order');
  assert.equal(out.has(1), false);
  assert.equal(out.get(2), 'green');
});

void test('a blank view label tints nothing (no section grammar to match on)', () => {
  assert.equal(statusBySeqFromFindings(VIEW, [validateFinding], '').size, 0);
});

void test('an empty view yields an empty map', () => {
  assert.equal(statusBySeqFromFindings(dv([]), [validateFinding], 'dv-order').size, 0);
});
