/**
 * The kind → (activity, task) inverse the kind-addressed screens need.
 *
 * These assertions are deliberately CONCRETE — the actual ids the server resolves
 * through `artifactKindForTask` — because the value of deriving the table from
 * `lifecycles.gen.ts` is only realised if a lifecycle change that moves an artifact
 * is visible here as a failure rather than as a silent re-point.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { dispatchRefFor, reviewRefFor } from './designTaskRef.ts';
import { SLOT_KIND } from './taskArtifactFor.ts';
import { lifecycleFor } from './lifecycles.gen.ts';

void test('the four Requirements artifacts resolve to their own draft and review tasks', () => {
  assert.deepEqual(dispatchRefFor('mission'), {
    activityId: 'requirements',
    taskId: 'missionDraft',
  });
  assert.deepEqual(reviewRefFor('mission'), {
    activityId: 'requirements',
    taskId: 'missionReview',
  });
  assert.deepEqual(dispatchRefFor('glossary'), {
    activityId: 'requirements',
    taskId: 'glossaryDraft',
  });
  assert.deepEqual(reviewRefFor('volatilities'), {
    activityId: 'requirements',
    taskId: 'volatilitiesReview',
  });
  assert.deepEqual(dispatchRefFor('coreUseCases'), {
    activityId: 'requirements',
    taskId: 'coreUseCasesDraft',
  });
});

void test('the System artifact lives on the Architecture activity, not Requirements', () => {
  assert.deepEqual(dispatchRefFor('system'), {
    activityId: 'architecture',
    taskId: 'architectureDraft',
  });
  assert.deepEqual(reviewRefFor('system'), {
    activityId: 'architecture',
    taskId: 'architectureReview',
  });
});

void test("the M0 gate's one task is its own review — a computed plan has no separate dispatch", () => {
  // DispatchActivityTask maps this task id onto RequestSDPCommit server-side, so
  // both refs are the same task even though its kind is `review`.
  const task = lifecycleFor('projectDesign')?.tasks.find((t) => t.id === 'sdpReview');
  assert.equal(task?.kind, 'review', 'the M0 task is a review, not a dispatch');
  assert.deepEqual(dispatchRefFor('sdpReview'), {
    activityId: 'projectDesign',
    taskId: 'sdpReview',
  });
  assert.deepEqual(reviewRefFor('sdpReview'), {
    activityId: 'projectDesign',
    taskId: 'sdpReview',
  });
});

void test('a construction artifact resolves to nothing — it is never addressed by kind', () => {
  // 'SRS' etc. name no slot: SLOT_KIND holds exactly the six design kinds, and a
  // construction screen always has the activityId in its route.
  assert.equal(dispatchRefFor('srs' as never), undefined);
  assert.equal(reviewRefFor('detailedDesign' as never), undefined);
});

void test('every SLOT_KIND artifact resolves both ways (no design kind is unaddressable)', () => {
  for (const kind of Object.values(SLOT_KIND)) {
    assert.notEqual(dispatchRefFor(kind), undefined, `${kind} has no dispatch task`);
    assert.notEqual(reviewRefFor(kind), undefined, `${kind} has no review task`);
  }
});

void test('a resolved task really exists in the lifecycle it names', () => {
  for (const kind of Object.values(SLOT_KIND)) {
    for (const ref of [dispatchRefFor(kind), reviewRefFor(kind)]) {
      if (ref === undefined) {
        assert.fail(`${kind} resolved to no ref`);
      }
      const found = lifecycleFor(ref.activityId)?.tasks.some((t) => t.id === ref.taskId);
      assert.equal(found, true, `${ref.activityId} has no task ${ref.taskId}`);
    }
  }
});
