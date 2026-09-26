/**
 * mapConstructionSession carries the session view's gate-occurrence fields (B1.2) to
 * the ops-client session hooks: which gate the activity waits at, when this occurrence
 * began, when an escalation gives up, whether the send-back budget is spent, and the
 * attempt of its budget. A view from an older server has none of those keys and maps to
 * a view that simply lacks them.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mapConstructionSession } from './wire.ts';
import type { components } from './schema.ts';

type WireSession = components['schemas']['DeliveryConstructionSessionView'];

void test('the gate, its occurrence, the redraft budget and the attempt reach the mapped view', () => {
  const wire: WireSession = {
    projectId: 'archistrator',
    activityId: 'C-billing-manager',
    stage: 7,
    awaitingGate: 'detailed_design',
    awaitingSince: '2026-09-13T12:00:00Z',
    redraftExhausted: true,
    attempt: 2,
    attemptBudget: 10,
  };
  const { view } = mapConstructionSession(wire);
  assert.equal(view.awaitingGate, 'detailed_design');
  assert.equal(view.awaitingSince, '2026-09-13T12:00:00Z');
  assert.equal('awaitingUntil' in view, false);
  assert.equal(view.redraftExhausted, true);
  assert.equal(view.attempt, 2);
  assert.equal(view.attemptBudget, 10);
});

void test('an escalation carries its deadline', () => {
  const { view } = mapConstructionSession({
    projectId: 'archistrator',
    activityId: 'C-billing-manager',
    stage: 4,
    awaitingGate: 'takeover',
    awaitingSince: '2026-09-13T12:00:00Z',
    awaitingUntil: '2026-09-13T13:00:00Z',
    redraftExhausted: false,
    attempt: 1,
    attemptBudget: 10,
  });
  assert.equal(view.awaitingGate, 'takeover');
  assert.equal(view.awaitingUntil, '2026-09-13T13:00:00Z');
});

void test('a view from an older server, without the new keys, maps without them', () => {
  const older = {
    projectId: 'archistrator',
    activityId: 'C-billing-manager',
    stage: 2,
  } as unknown as WireSession;
  const { view } = mapConstructionSession(older);
  for (const key of [
    'awaitingGate',
    'awaitingSince',
    'awaitingUntil',
    'redraftExhausted',
    'attempt',
    'attemptBudget',
  ]) {
    assert.equal(key in view, false, `${key} must be absent, not invented`);
  }
});
