/**
 * mapConstructionSession carries the session view's gate occurrences (B1.2, made PLURAL by
 * stage 4b3) to the ops-client session hooks: which tasks the activity is waiting at, the
 * gate CLASS each waits at, when each occurrence began, when an escalation gives up, whether
 * each gate's send-back budget is spent, who is reviewing — and, when nobody could be asked,
 * why not. A session holding no gate maps to a view with no awaiting list at all.
 *
 * THE SHAPE CHANGED AND THE TESTS CHANGED WITH IT. Until this wave the six gate facts were
 * flat members of the session, describing an ACTIVITY while the facts are per TASK: on a fork
 * they could only ever name whichever gate was entered last. The first case below is what
 * that could not say — two gates, in one answer, each with its own clock and its own roster.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mapConstructionSession } from './wire.ts';
import type { components } from './schema.ts';

type WireSession = components['schemas']['DeliveryConstructionSessionView'];

void test('a fork maps BOTH gates, each with its own occurrence and its own roster', () => {
  const wire: WireSession = {
    projectId: 'archistrator',
    activityId: 'C-billing-manager',
    stage: 7,
    awaitingTasks: [
      {
        taskId: 'designReview',
        gate: 'designReview',
        awaitingSince: '2026-09-13T12:00:00Z',
        redraftExhausted: true,
        reviewSet: {
          reviewers: [{ role: 'architect', perspective: 'architecture', mayAmend: true }],
        },
      },
      {
        taskId: 'stpReview',
        gate: 'stpReview',
        awaitingSince: '2026-09-13T12:30:00Z',
        redraftExhausted: false,
      },
    ],
    attempt: 2,
    attemptBudget: 10,
  };
  const { view } = mapConstructionSession(wire);
  const gates = view.awaitingTasks ?? [];
  // Asserted as a PROJECTION of the whole list rather than by index: the claim is about
  // both entries and their order (the server sorts by task id), and each gate's occurrence
  // being its OWN — a shared clock or a shared roster is the defect the per-task key removed.
  assert.deepEqual(
    gates.map((g) => [g.taskId, g.gate, g.awaitingSince, g.redraftExhausted, 'awaitingUntil' in g]),
    [
      ['designReview', 'designReview', '2026-09-13T12:00:00Z', true, false],
      ['stpReview', 'stpReview', '2026-09-13T12:30:00Z', false, false],
    ]
  );
  assert.deepEqual(
    gates.map((g) => g.reviewSet?.reviewers?.map((r) => r.role) ?? null),
    [['architect'], null]
  );
  assert.equal(view.attempt, 2);
  assert.equal(view.attemptBudget, 10);
});

void test('an escalation carries its deadline, and its gate class is not its task', () => {
  const { view } = mapConstructionSession({
    projectId: 'archistrator',
    activityId: 'C-billing-manager',
    stage: 4,
    awaitingTasks: [
      {
        taskId: 'detailedDesign',
        gate: 'takeover',
        awaitingSince: '2026-09-13T12:00:00Z',
        awaitingUntil: '2026-09-13T13:00:00Z',
        redraftExhausted: false,
      },
    ],
    attempt: 1,
    attemptBudget: 10,
  });
  // The one shape where the two differ: an escalation is keyed by the task that escalated
  // and waits at "takeover". A view that folded them would lose which task is stuck.
  assert.deepEqual(
    (view.awaitingTasks ?? []).map((g) => [g.taskId, g.gate, g.awaitingUntil]),
    [['detailedDesign', 'takeover', '2026-09-13T13:00:00Z']]
  );
});

void test("the review engine's refusal rides the gate it belongs to", () => {
  // reviewSetError had NO producer until stage 4b3 — every write in the Manager was the
  // empty string, because the engine's error failed the task instead of surfacing. It has
  // one now (the gate opens with an empty roster and says why), so the mapper carrying it
  // is load-bearing rather than defensive.
  const { view } = mapConstructionSession({
    projectId: 'archistrator',
    stage: 7,
    awaitingTasks: [
      {
        taskId: 'designReview',
        gate: 'designReview',
        awaitingSince: '2026-09-13T12:00:00Z',
        redraftExhausted: false,
        reviewSetError: 'ProposeReviews: unrecognised artifactKind',
      },
    ],
    attempt: 1,
    attemptBudget: 10,
  });
  assert.deepEqual(
    (view.awaitingTasks ?? []).map((g) => [g.reviewSetError, 'reviewSet' in g]),
    [['ProposeReviews: unrecognised artifactKind', false]]
  );
});

void test('a session holding no gate maps without an awaiting list', () => {
  const quiet = {
    projectId: 'archistrator',
    activityId: 'C-billing-manager',
    stage: 2,
  } as unknown as WireSession;
  const { view } = mapConstructionSession(quiet);
  // The ABSENCE is the answer — "nobody is being waited on" — and it must not be invented
  // as an empty array, which would read to a caller as a list it had already looked at.
  for (const key of ['awaitingTasks', 'attempt', 'attemptBudget']) {
    assert.equal(key in view, false, `${key} must be absent, not invented`);
  }
});
