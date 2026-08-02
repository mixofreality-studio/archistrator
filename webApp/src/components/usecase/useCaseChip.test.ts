/// <reference types="node" />
/**
 * Unit tests for useCaseChip: the carousel's per-use-case realization roll-up
 * ("N/M steps realized"), the walkthrough's per-node badge state, the shared
 * eligibility predicate that decides which activity-node kinds a dynamic view
 * is REQUIRED to realize (action/timeEvent/acceptEvent — mirrors the server's
 * designhealth ccMustHaveStep set / CC-COVERAGE), and the shared tone→color
 * mapping both surfaces render with. Decision/switch nodes MAY carry a step
 * but are never "missing" one, so callers exclude them from eligibleNodeIds
 * entirely rather than this module special-casing them — and stepBadgeState
 * gates on eligibility FIRST, so a decision node carrying a step is still not
 * badge-worthy (fix-round-1: this was the reviewer-found defect).
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  realizationChip,
  isEligibleForRealization,
  stepBadgeState,
  toneColor,
} from './useCaseChip.ts';
import type { RealizedStep } from '../../contracts/realization';
import type { Finding } from '../../contracts/types';
import type { Tokens } from '../../utilities/theme/themes';

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

// ── stepBadgeState ────────────────────────────────────────────────────────

void test('a decision node carrying a realized step is still not badge-worthy (eligibility gates first)', () => {
  assert.equal(stepBadgeState('decision', step('n-decision'), []), undefined);
});

void test('a decision node with no step and no findings is not badge-worthy either', () => {
  assert.equal(stepBadgeState('decision', undefined, []), undefined);
});

void test('an eligible node with a realized step and no findings is realized/ok', () => {
  assert.deepEqual(stepBadgeState('action', step('n1'), []), { label: '✓ realized', tone: 'ok' });
});

void test('an eligible node with a realized step and findings is the FIRST ruleId/error', () => {
  const out = stepBadgeState('action', step('n1'), [fnd('RuleCCActorEdge'), fnd('RuleCCStepNode')]);
  assert.deepEqual(out, { label: '✗ RuleCCActorEdge', tone: 'error' });
});

void test('an eligible node with no step is — no realization/warn', () => {
  assert.deepEqual(stepBadgeState('action', undefined, []), {
    label: '— no realization',
    tone: 'warn',
  });
});

void test('timeEvent and acceptEvent share the SAME eligibility as action', () => {
  assert.deepEqual(stepBadgeState('timeEvent', undefined, []), {
    label: '— no realization',
    tone: 'warn',
  });
  assert.deepEqual(stepBadgeState('acceptEvent', step('n1'), []), {
    label: '✓ realized',
    tone: 'ok',
  });
});

// ── toneColor ─────────────────────────────────────────────────────────────

// Only the 3 fields toneColor reads are meaningful here — a full Tokens fixture
// would be ~20 unrelated fields of noise for a 3-branch pure mapping, so this
// leaf test asserts just the shape toneColor actually touches (cast through
// unknown, the standard partial-fixture idiom for a narrow pure function).
const FAKE_TOKENS = {
  committedDot: '#0a0',
  dangerFg: '#a00',
  awaitingFg: '#aa0',
} as unknown as Tokens;

void test('toneColor maps ok/error/warn to the committed/danger/awaiting tokens', () => {
  assert.equal(toneColor('ok', FAKE_TOKENS), '#0a0');
  assert.equal(toneColor('error', FAKE_TOKENS), '#a00');
  assert.equal(toneColor('warn', FAKE_TOKENS), '#aa0');
});

void test('toneColor maps the widened "pending" tone to the same awaiting token as warn (Task 6)', () => {
  assert.equal(toneColor('pending', FAKE_TOKENS), '#aa0');
});
