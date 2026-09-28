/// <reference types="node" />
/**
 * `overrideActionFor` — the STEER / REOPEN split (stage 4b1 Task 14, controller
 * ruling 3). `OverrideActivity` means two things keyed by liveness, and the screen
 * must decide which one it is offering; the server can only say which one is
 * possible.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { overrideActionFor } from './activityOverride.ts';
import type { ActivityViewWire } from './activityViewToGraph.ts';

type TaskWire = ActivityViewWire['tasks'][number];
type RevisionWire = TaskWire['revisions'][number];

function rev(n: number, outcome: RevisionWire['outcome']): RevisionWire {
  return { n, outcome, attemptIds: [], commentCount: 0, comments: [], provenance: 'observed' };
}

function task(id: string, revisions: RevisionWire[]): TaskWire {
  return {
    id,
    kind: 'dispatch',
    title: id,
    phase: 'p',
    dependsOn: [],
    state: 'running',
    revisions,
  };
}

function view(state: ActivityViewWire['state'], tasks: TaskWire[] = []): ActivityViewWire {
  return {
    activityId: 'C-billing-state-access',
    name: 'Billing state access',
    type: 'service',
    state,
    phases: [],
    tasks,
  };
}

void test('a DONE activity is re-opened, never steered — there is no child to steer', () => {
  assert.equal(overrideActionFor(view('done', [task('srs', [rev(1, 'passed')])])), 'reopen');
});

void test('a FAILED activity is re-opened too, and the failed attempt does not make it a steer', () => {
  // The ordering is the assertion: a failed activity HAS a failed latest attempt, so
  // asking "is anything failed?" first would offer a steer with nothing to reach.
  assert.equal(overrideActionFor(view('failed', [task('srs', [rev(1, 'failed')])])), 'reopen');
});

void test('a LIVE activity whose latest attempt failed is escalated: it is steered', () => {
  const v = view('awaitingHuman', [
    task('srs', [rev(1, 'passed')]),
    task('detailedDesign', [rev(1, 'passed'), rev(2, 'failed')]),
  ]);
  assert.equal(overrideActionFor(v), 'steer');
});

void test('a failure the walk already re-dispatched is NOT an escalation any more', () => {
  // `escalatedTaskOf`'s own rule: the task's HIGHEST-numbered attempt must be the
  // failed one. Revision 3 running over a failed 2 is the retry in flight.
  const v = view('running', [task('detailedDesign', [rev(2, 'failed'), rev(3, 'running')])]);
  assert.equal(overrideActionFor(v), 'none');
});

void test('a healthy live activity offers neither — a control that cannot be used is not one', () => {
  assert.equal(overrideActionFor(view('running', [task('srs', [rev(1, 'running')])])), 'none');
  assert.equal(
    overrideActionFor(view('awaitingHuman', [task('srsReview', [rev(1, 'awaitingHuman')])])),
    'none',
    'an APPROVAL gate is awaitingHuman too, and it is decided at the gate, not steered'
  );
});

void test('an activity that has never started offers neither, and an unread view offers neither', () => {
  assert.equal(overrideActionFor(view('notStarted')), 'none');
  assert.equal(overrideActionFor(undefined), 'none');
});
