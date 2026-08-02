/// <reference types="node" />
/**
 * Unit tests for viewVerdict: the Architecture dynamic lens' per-view CC
 * verdict roll-up (call-chain rollout Task 6). Fixture sections mirror the
 * designhealth CC-* section grammar exactly as callStatus.test.ts uses it:
 *
 *   "dynamicView <key>"                   - view-scoped (CC-VIEW-USECASE)
 *   "dynamicView <key> step <nodeId>"     - step-scoped (e.g. CC-ACTOR-EDGE)
 *   "useCase <useCaseId>"                 - use-case-scoped (e.g. CC-COVERAGE
 *                                            — see server/internal/utility/
 *                                            designhealth/rules_callchain.go).
 *                                            NEVER dvKey-prefixed, so
 *                                            findingsForView structurally
 *                                            cannot see it.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { viewVerdict } from './viewVerdict.ts';
import type { Finding, Severity } from '../../contracts/types';

function fnd(ruleId: string, severity: Severity, section: string): Finding {
  return { ruleId, severity, message: `${ruleId} fired`, location: { ordinal: 0, section } };
}

// ── the four verdict shapes (brief's exact examples) ────────────────────────

void test('all eligible nodes realized, no findings -> ok, "N/N realized · CC clean"', () => {
  const out = viewVerdict([], 'dv-order', 15, 15);
  assert.deepEqual(out, { label: '15/15 realized · CC clean', tone: 'ok' });
});

void test('(a) zero realized, no view-scoped findings -> pending, regardless of eligible count', () => {
  const out = viewVerdict([], 'dv-order', 0, 7);
  assert.deepEqual(out, { label: '0/7 realized · pending', tone: 'pending' });
});

void test('any finding scoped to this view -> error, "N CC findings" (no ratio)', () => {
  const findings = [
    fnd('RuleCCActorEdge', 'error', 'dynamicView dv-order step n-validate'),
    fnd('RuleCCViewUseCase', 'warning', 'dynamicView dv-order'),
  ];
  const out = viewVerdict(findings, 'dv-order', 5, 7);
  assert.deepEqual(out, { label: '2 CC findings', tone: 'error' });
});

void test('partial realization, no findings -> warn, "N/M realized" (no gloss suffix)', () => {
  const out = viewVerdict([], 'dv-order', 3, 7);
  assert.deepEqual(out, { label: '3/7 realized', tone: 'warn' });
});

// ── singular/plural finding-count grammar ───────────────────────────────────

void test('exactly one finding reads "1 CC finding", not "1 CC findings"', () => {
  const out = viewVerdict([fnd('RuleX', 'error', 'dynamicView dv-order')], 'dv-order', 4, 7);
  assert.deepEqual(out, { label: '1 CC finding', tone: 'error' });
});

// ── precedence: findings ALWAYS win, even at zero-realized (fix round 1) ────
//
// FINDING 1 (task reviewer): the earlier version of this module checked
// zero-realized BEFORE findings, on the false premise that a wholly-
// unrealized view's CC-COVERAGE findings would need suppressing. CC-COVERAGE
// is use-case-scoped, never dvKey-scoped, so that premise was wrong — and the
// old precedence hid a REAL reachable defect: a realized DECISION step
// (ineligible for the realized/eligible count) can still carry a genuine
// dvKey-scoped finding while eligible-realized sits at 0.

void test('(b) zero-eligible-realized + a genuine dvKey-scoped finding -> error, not pending', () => {
  // CC-ACTOR-EDGE-style: step-scoped under a DECISION node, which never counts
  // toward eligibleNodeCount (only action/timeEvent/acceptEvent do) — so
  // realizedStepCount is 0 out of 7 eligible, yet a real defect exists.
  const findings = [fnd('RuleCCActorEdge', 'error', 'dynamicView dv-order step n-decide')];
  const out = viewVerdict(findings, 'dv-order', 0, 7);
  assert.deepEqual(out, { label: '1 CC finding', tone: 'error' });
});

void test("(c) a use-case-scoped finding (CC-COVERAGE's real shape) never flips the view verdict", () => {
  // "useCase <id>" carries no "dynamicView <dvKey>" prefix at all, so
  // findingsForView's join can't see it — the verdict falls through to the
  // (correct) pending shape, proving CC-COVERAGE's absence here is the
  // section grammar at work, not a precedence carve-out.
  const findings = [fnd('RuleCCCoverage', 'error', 'useCase uc-order')];
  const out = viewVerdict(findings, 'dv-order', 0, 7);
  assert.deepEqual(out, { label: '0/7 realized · pending', tone: 'pending' });
});

// ── the view-scoped join (dvKey prefix) ──────────────────────────────────────

void test('a finding under ANOTHER view never counts toward this one', () => {
  const findings = [fnd('RuleCCActorEdge', 'error', 'dynamicView dv-track step n-charge')];
  const out = viewVerdict(findings, 'dv-order', 5, 7);
  assert.deepEqual(out, { label: '5/7 realized', tone: 'warn' });
});

void test('a finding with no location never counts', () => {
  const findings: Finding[] = [{ ruleId: 'RuleX', severity: 'error', message: 'no location' }];
  const out = viewVerdict(findings, 'dv-order', 5, 7);
  assert.deepEqual(out, { label: '5/7 realized', tone: 'warn' });
});

void test('a blank dvKey joins no findings at all', () => {
  const findings = [fnd('RuleX', 'error', 'dynamicView dv-order')];
  const out = viewVerdict(findings, '', 5, 7);
  assert.deepEqual(out, { label: '5/7 realized', tone: 'warn' });
});

void test('a view key that is a STRING PREFIX of the requested one does not collide', () => {
  // "dv-order" is itself a prefix of "dv-order2" — a bare startsWith would let
  // dv-order2's view-scoped and step-scoped findings flip dv-order's verdict.
  const findings = [
    fnd('RuleCCViewUseCase', 'warning', 'dynamicView dv-order2'),
    fnd('RuleCCActorEdge', 'error', 'dynamicView dv-order2 step n-validate'),
  ];
  const out = viewVerdict(findings, 'dv-order', 5, 7);
  assert.deepEqual(out, { label: '5/7 realized', tone: 'warn' });
});

// ── zero-eligible edge case (nothing was ever required) ─────────────────────

void test('zero eligible nodes, zero realized, no findings -> ok "0/0", not pending', () => {
  // Nothing was ever required (e.g. an all-control-flow diagram) — vacuously
  // satisfied, not "pending realization".
  const out = viewVerdict([], 'dv-order', 0, 0);
  assert.deepEqual(out, { label: '0/0 realized · CC clean', tone: 'ok' });
});
