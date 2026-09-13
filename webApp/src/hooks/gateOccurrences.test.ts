/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import { observeGate, type GateOccurrence } from './gateOccurrences.ts';

void test('each entry into awaitingApproval is a new occurrence; staying there is not', () => {
  let o: GateOccurrence | undefined;
  o = observeGate(o, 'awaitingApproval', 1);
  assert.equal(o.epoch, 1);
  o = observeGate(o, 'awaitingApproval', 2);
  assert.equal(o.epoch, 1);
  o = observeGate(o, 'pipelineRunning', 3); // a send-back's redraft
  assert.equal(o.epoch, 1);
  assert.equal(o.leftAt, 3);
  o = observeGate(o, 'pipelineRunning', 4);
  assert.equal(o.leftAt, 3); // first seen away, not latest
  o = observeGate(o, 'awaitingApproval', 5); // round 2
  assert.equal(o.epoch, 2);
  assert.equal(o.leftAt, undefined);
});

void test('a read no newer than the last one folded changes nothing', () => {
  const o = observeGate(undefined, 'awaitingApproval', 10);
  assert.equal(observeGate(o, 'pipelineRunning', 10), o);
  assert.equal(observeGate(o, 'pipelineRunning', 9), o);
});

void test('a session that no longer exists has left the gate', () => {
  const o = observeGate(observeGate(undefined, 'awaitingApproval', 1), null, 2);
  assert.equal(o.stage, null);
  assert.equal(o.leftAt, 2);
  assert.equal(o.epoch, 1);
});
