/// <reference types="node" />
/**
 * Unit tests for the pure Design-Health -> use-case/step join
 * (src/components/flow/useCaseFindings.ts). Fixture sections mirror the
 * designhealth CC-* section grammar exactly as landed in Task 7
 * (server/internal/utility/designhealth/rules_callchain.go):
 *
 *   "useCase <id>"                        - use-case-scoped
 *   "dynamicView <key>"                   - view-scoped (CC-VIEW-USECASE)
 *   "dynamicView <key> step <nodeId>"     - step-scoped
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { findingsForUseCase, findingsForStep } from './useCaseFindings.ts';
import type { Finding, Severity } from '../../contracts/types';

function fnd(ruleId: string, severity: Severity, message: string, section?: string): Finding {
  return {
    ruleId,
    severity,
    message,
    ...(section !== undefined ? { location: { ordinal: 0, section } } : {}),
  };
}

const useCaseFinding = fnd('RuleCovUCDynamic', 'error', 'no dynamic view', 'useCase uc-1');
const viewScopedFinding = fnd(
  'RuleCCViewUseCase',
  'warning',
  'view mismatch',
  'dynamicView dv-order'
);
const stepFinding1 = fnd(
  'RuleCCActorEdge',
  'error',
  'actor edge issue',
  'dynamicView dv-order step n-validate'
);
const stepFinding2 = fnd(
  'RuleChainEntryManager',
  'warning',
  'entry not a manager',
  'dynamicView dv-order step n-charge'
);
const otherUseCaseFinding = fnd('RuleCovUCDynamic', 'error', 'other uc', 'useCase uc-2');
const otherViewFinding = fnd('RuleCCViewUseCase', 'warning', 'other view', 'dynamicView dv-track');
const noLocationFinding = fnd('DH-CARD-MANAGERS', 'warning', 'no location at all');

const ALL: Finding[] = [
  useCaseFinding,
  viewScopedFinding,
  stepFinding1,
  stepFinding2,
  otherUseCaseFinding,
  otherViewFinding,
  noLocationFinding,
];

// ── findingsForUseCase ───────────────────────────────────────────────────────

void test('without a dvLabel, matches only the exact use-case-scoped section', () => {
  assert.deepEqual(findingsForUseCase(ALL, 'uc-1'), [useCaseFinding]);
});

void test('with a dvLabel, also matches the view-scoped section for that view', () => {
  const out = findingsForUseCase(ALL, 'uc-1', 'dv-order');
  assert.ok(out.includes(viewScopedFinding));
});

void test('with a dvLabel, also matches every step-scoped section under that view', () => {
  const out = findingsForUseCase(ALL, 'uc-1', 'dv-order');
  assert.ok(out.includes(stepFinding1));
  assert.ok(out.includes(stepFinding2));
});

void test('with a dvLabel, the full join is exactly useCase + view + its steps', () => {
  const out = findingsForUseCase(ALL, 'uc-1', 'dv-order');
  assert.deepEqual(out, [useCaseFinding, viewScopedFinding, stepFinding1, stepFinding2]);
});

void test('excludes another use case and another view entirely', () => {
  const out = findingsForUseCase(ALL, 'uc-1', 'dv-order');
  assert.ok(!out.includes(otherUseCaseFinding));
  assert.ok(!out.includes(otherViewFinding));
});

void test('a location-less finding never matches', () => {
  assert.ok(!findingsForUseCase(ALL, 'uc-1', 'dv-order').includes(noLocationFinding));
});

void test('an absent dvLabel excludes every dynamicView-sectioned finding', () => {
  const out = findingsForUseCase(ALL, 'uc-1');
  assert.deepEqual(out, [useCaseFinding]);
});

// ── findingsForStep ──────────────────────────────────────────────────────────

void test('matches exactly one step-scoped section', () => {
  assert.deepEqual(findingsForStep(ALL, 'dv-order', 'n-validate'), [stepFinding1]);
  assert.deepEqual(findingsForStep(ALL, 'dv-order', 'n-charge'), [stepFinding2]);
});

void test('does not match the view-scoped or use-case-scoped sections', () => {
  assert.deepEqual(findingsForStep(ALL, 'dv-order', 'nonexistent-node'), []);
});

void test('does not match a step under a different view key', () => {
  assert.deepEqual(findingsForStep(ALL, 'dv-track', 'n-validate'), []);
});
