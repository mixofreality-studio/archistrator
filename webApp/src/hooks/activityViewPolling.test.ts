/// <reference types="node" />
/**
 * Unit tests for the activity-view poll cadence (src/hooks/activityViewPolling.ts) —
 * one case per row of its decision table.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { ApiError } from '../contracts/errors.ts';
import {
  ACTIVITY_DEGRADED_POLL_MS,
  ACTIVITY_GATE_POLL_MS,
  ACTIVITY_LIVE_POLL_MS,
  activityViewPollIntervalMs,
} from './activityViewPolling.ts';

const view = (
  state: string,
  ...taskStates: string[]
): { state: string; tasks: { state: string }[] } => ({
  state,
  tasks: taskStates.map((s) => ({ state: s })),
});

void test('a running task polls at the live cadence, even while another branch waits at a gate', () => {
  assert.equal(
    activityViewPollIntervalMs(view('awaitingHuman', 'passed', 'awaitingHuman', 'running'), null),
    ACTIVITY_LIVE_POLL_MS
  );
  assert.equal(
    activityViewPollIntervalMs(view('running', 'passed', 'running'), null),
    ACTIVITY_LIVE_POLL_MS
  );
});

void test('a running activity between tasks still polls at the live cadence', () => {
  assert.equal(
    activityViewPollIntervalMs(view('running', 'passed', 'pending'), null),
    ACTIVITY_LIVE_POLL_MS
  );
});

void test('a gate with nothing running polls slowly: the human is the actor', () => {
  assert.equal(
    activityViewPollIntervalMs(view('awaitingHuman', 'passed', 'awaitingHuman', 'locked'), null),
    ACTIVITY_GATE_POLL_MS
  );
});

void test('done, failed and not-started stop; not-started polls when the caller asks', () => {
  for (const state of ['done', 'failed', 'notStarted']) {
    assert.equal(activityViewPollIntervalMs(view(state, 'passed'), null), false, state);
  }
  assert.equal(activityViewPollIntervalMs(view('notStarted', 'pending'), null, 4000), 4000);
  assert.equal(activityViewPollIntervalMs(view('done', 'passed'), null, 4000), false);
});

void test('a pristine mount adds no interval', () => {
  assert.equal(activityViewPollIntervalMs(undefined, null), false);
  assert.equal(activityViewPollIntervalMs(undefined, undefined), false);
});

void test('an unknown activity (404) stops; any other fault degrades and never stops', () => {
  const notFound = new ApiError(
    404,
    'not_found',
    'no activity C-nope in the committed activity list'
  );
  const blip = new ApiError(503, 'unavailable', 'blip');
  assert.equal(activityViewPollIntervalMs(undefined, notFound), false);
  assert.equal(activityViewPollIntervalMs(view('running', 'running'), notFound), false);
  assert.equal(activityViewPollIntervalMs(undefined, blip), ACTIVITY_DEGRADED_POLL_MS);
  assert.equal(activityViewPollIntervalMs(view('done', 'passed'), blip), ACTIVITY_DEGRADED_POLL_MS);
  assert.equal(
    activityViewPollIntervalMs(view('running', 'running'), new Error('ECONNREFUSED')),
    ACTIVITY_DEGRADED_POLL_MS
  );
});
