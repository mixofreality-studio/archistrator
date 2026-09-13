/**
 * Approve / Send back say why they are off (final review minor): an owed gate with no
 * lifecycle phase — the route hands the pane no decision for it, because none can be
 * addressed — disabled both with no reason at all.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  DECISION_BUSY_REASON,
  DECISION_CLOSED_REASON,
  DECISION_ELSEWHERE_REASON,
  DECISION_NO_PHASE_REASON,
  decisionActionState,
} from './detailPaneState.ts';

void test('a decision the pane can make is enabled; every other case is off with its reason', () => {
  const live = { open: true, busy: false };
  assert.deepEqual(decisionActionState(live, true), { disabled: false });
  assert.deepEqual(decisionActionState(undefined, false), {
    disabled: true,
    reason: DECISION_NO_PHASE_REASON,
  });
  assert.deepEqual(decisionActionState(live, false), {
    disabled: true,
    reason: DECISION_ELSEWHERE_REASON,
  });
  assert.deepEqual(decisionActionState({ open: false, busy: false }, true), {
    disabled: true,
    reason: DECISION_CLOSED_REASON,
  });
  assert.deepEqual(decisionActionState({ open: true, busy: true }, true), {
    disabled: true,
    reason: DECISION_BUSY_REASON,
  });
});

void test('the no-phase reason says why no decision can be sent', () => {
  assert.match(DECISION_NO_PHASE_REASON, /no current phase/);
  for (const r of [
    DECISION_NO_PHASE_REASON,
    DECISION_ELSEWHERE_REASON,
    DECISION_CLOSED_REASON,
    DECISION_BUSY_REASON,
  ]) {
    assert.ok(r.length > 0 && r.endsWith('.'), r);
  }
});
