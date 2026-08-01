/// <reference types="node" />
/**
 * Unit tests for viewVerdict: the Architecture dynamic lens' per-view CC
 * verdict roll-up (call-chain rollout Task 6). Fixture sections mirror the
 * designhealth CC-* section grammar exactly as callStatus.test.ts uses it:
 *
 *   "dynamicView <key>"                   - view-scoped (CC-VIEW-USECASE)
 *   "dynamicView <key> step <nodeId>"     - step-scoped
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

void test('zero realized -> pending, regardless of eligible count', () => {
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

// ── the pending state outranks findings (module doc's core design decision) ─

void test('zero realized still reads pending even when the view already carries findings', () => {
  const findings = [fnd('RuleCCCoverage', 'error', 'dynamicView dv-order step n-validate')];
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

// ── zero-eligible edge case (nothing was ever required) ─────────────────────

void test('zero eligible nodes, zero realized, no findings -> ok "0/0", not pending', () => {
  // Nothing was ever required (e.g. an all-control-flow diagram) — vacuously
  // satisfied, not "pending realization".
  const out = viewVerdict([], 'dv-order', 0, 0);
  assert.deepEqual(out, { label: '0/0 realized · CC clean', tone: 'ok' });
});
