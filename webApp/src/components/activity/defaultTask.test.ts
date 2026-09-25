/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { defaultTaskId, selectedTaskId } from './defaultTask.ts';
import type { LifecycleNode } from './lifecycleGraphTypes.ts';

function node(id: string, state: LifecycleNode['state']): LifecycleNode {
  return {
    id,
    kind: 'dispatch',
    title: id,
    phase: 'phase',
    state,
    dependsOn: [],
    revisions: [],
  };
}

void test('awaiting-human wins over failed and running', () => {
  const nodes = [
    node('a', 'done'),
    node('b', 'failed'),
    node('c', 'awaitingHuman'),
    node('d', 'running'),
  ];
  assert.equal(defaultTaskId(nodes), 'c');
});

void test('failed wins over running when nothing awaits a human', () => {
  const nodes = [node('a', 'done'), node('b', 'running'), node('c', 'failed')];
  assert.equal(defaultTaskId(nodes), 'c');
});

void test('running wins over a later passed task', () => {
  const nodes = [node('a', 'done'), node('b', 'running'), node('c', 'done')];
  assert.equal(defaultTaskId(nodes), 'b');
});

void test('with nothing live it lands on the LAST passed, not the first', () => {
  const nodes = [node('a', 'done'), node('b', 'done'), node('c', 'pending')];
  assert.equal(defaultTaskId(nodes), 'b');
});

void test('a pristine activity — nothing has run — lands on nodes[0]', () => {
  const nodes = [node('a', 'pending'), node('b', 'locked')];
  assert.equal(defaultTaskId(nodes), 'a');
});

void test('defaultTaskId of an empty activity is undefined', () => {
  assert.equal(defaultTaskId([]), undefined);
});

void test('selectedTaskId honours a requested task the activity has', () => {
  const nodes = [node('a', 'done'), node('b', 'running')];
  assert.equal(selectedTaskId(nodes, 'a'), 'a');
});

void test('selectedTaskId falls back to the default when the URL names a task the activity does not have', () => {
  const nodes = [node('a', 'done'), node('b', 'running')];
  assert.equal(selectedTaskId(nodes, 'noSuchTask'), 'b');
});

void test('selectedTaskId falls back to the default when the URL names none', () => {
  const nodes = [node('a', 'done'), node('b', 'running')];
  assert.equal(selectedTaskId(nodes, undefined), 'b');
});
