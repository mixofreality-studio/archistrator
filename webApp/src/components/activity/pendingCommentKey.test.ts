/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { activityCommentKey, designCommentKey } from './pendingCommentKey.ts';

void test('the activity key carries all five segments, in order', () => {
  assert.equal(
    activityCommentKey({
      projectId: 'archistrator',
      activityId: 'C-billing-state-access',
      taskId: 'designReview',
      revision: 2,
    }),
    'activity:archistrator:C-billing-state-access:designReview:2'
  );
});

void test('a task with no revisions still keys stably at zero', () => {
  assert.equal(
    activityCommentKey({ projectId: 'p', activityId: 'A', taskId: 'srs', revision: 0 }),
    'activity:p:A:srs:0'
  );
});

void test('two revisions of one task are different slots', () => {
  const at = (revision: number): string =>
    activityCommentKey({ projectId: 'p', activityId: 'A', taskId: 'srsReview', revision });
  assert.notEqual(at(1), at(2));
});

void test('two tasks of one activity are different slots', () => {
  const on = (taskId: string): string =>
    activityCommentKey({ projectId: 'p', activityId: 'A', taskId, revision: 1 });
  assert.notEqual(on('srsReview'), on('codeReview'));
});

void test('the design rails’ key shape is untouched', () => {
  // Byte-for-byte what SystemDesignContainer.tsx and ProjectDesignExperience.tsx
  // pass today, so their stored drafts are not orphaned by the new shape (they
  // keep using it until stage 6).
  assert.equal(designCommentKey('archistrator', 'glossary'), 'archistrator:glossary');
});

void test('an activity key can never collide with a design key', () => {
  assert.ok(
    activityCommentKey({ projectId: 'p', activityId: 'A', taskId: 't', revision: 1 }).startsWith(
      'activity:'
    )
  );
  assert.ok(!designCommentKey('p', 'A').startsWith('activity:'));
});
