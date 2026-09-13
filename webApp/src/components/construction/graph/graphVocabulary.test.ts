/**
 * The graph's words and ink (designer P2s): one vocabulary with the list's lane
 * chip, and a check that turns red only when something is actually wrong.
 */
/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import { ROW_STATE_LABEL, chipFor } from '../list/activityRowPresentation.ts';
import { SEGMENT_STATE_LABEL, layeringCheckTone } from './graphPresentation.ts';

void test('one vocabulary: a complete segment says "Passed", exactly as the lane chip does', () => {
  assert.equal(SEGMENT_STATE_LABEL.complete, 'Passed');
  assert.equal(SEGMENT_STATE_LABEL.complete, ROW_STATE_LABEL.passed);
  assert.equal(SEGMENT_STATE_LABEL.complete, chipFor('passed')?.label);
  assert.equal(SEGMENT_STATE_LABEL.incomplete, 'Not passed');
  for (const label of Object.values(SEGMENT_STATE_LABEL)) {
    assert.doesNotMatch(label, /^Gate /, label);
  }
});

void test('the check is quiet at zero alarms and an alarm at one', () => {
  assert.equal(layeringCheckTone({ alarms: { up: 0, sideways: 0 } }), 'quiet');
  assert.equal(layeringCheckTone({ alarms: { up: 1, sideways: 0 } }), 'alarm');
  assert.equal(layeringCheckTone({ alarms: { up: 0, sideways: 2 } }), 'alarm');
});
