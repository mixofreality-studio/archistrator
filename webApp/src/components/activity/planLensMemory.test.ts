/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { lastPlanLens, rememberPlanLens } from './planLensMemory.ts';

// Module memory: these run in one module instance, in order, and the first one
// deliberately reads it before anything has written.
void test('before the plan has rendered, the way back is the plan’s own default', () => {
  assert.equal(lastPlanLens(), 'list');
});

void test('the lens the plan rendered is the lens ✕ returns to', () => {
  rememberPlanLens('graph');
  assert.equal(lastPlanLens(), 'graph');
});

void test('the LAST lens wins, so a reader who moved on does not go back two steps', () => {
  rememberPlanLens('tasks');
  rememberPlanLens('list');
  assert.equal(lastPlanLens(), 'list');
});
