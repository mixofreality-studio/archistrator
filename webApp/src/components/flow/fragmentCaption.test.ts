/// <reference types="node" />
/**
 * Unit tests for fragmentCaption's fragmentCallLessHeading: the three-way
 * caption split for a fragment-mode step that authors no calls (founder QA
 * round 3) — the multi-root entry chooser, a real realization gap (a
 * call-eligible node with nothing realized), and a by-design control-flow
 * step (merge/decision/fork/join/start/end/…).
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { fragmentCallLessHeading } from './fragmentCaption.ts';

void test('the entry chooser (blank node id) gets the neutral prompt regardless of kind', () => {
  assert.equal(fragmentCallLessHeading('', undefined), 'Choose an entry to begin.');
  assert.equal(fragmentCallLessHeading('', 'action'), 'Choose an entry to begin.');
  assert.equal(fragmentCallLessHeading('', 'merge'), 'Choose an entry to begin.');
});

void test('a call-eligible kind with nothing realized is a real gap', () => {
  assert.equal(fragmentCallLessHeading('n1', 'action'), 'No realization for this step');
  assert.equal(fragmentCallLessHeading('n1', 'timeEvent'), 'No realization for this step');
  assert.equal(fragmentCallLessHeading('n1', 'acceptEvent'), 'No realization for this step');
});

void test('every other control-flow kind is by-design, no calls', () => {
  for (const kind of ['merge', 'decision', 'fork', 'join', 'start', 'end', 'switch'] as const) {
    assert.equal(fragmentCallLessHeading('n1', kind), 'Control-flow step — no calls');
  }
});

void test('an unknown kind (no focusStepKind wired) degrades to the conservative real-gap caption', () => {
  assert.equal(fragmentCallLessHeading('n1', undefined), 'No realization for this step');
});
