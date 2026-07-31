/// <reference types="node" />
/**
 * Unit tests for fragmentCaption: the caption text for a fragment-mode step that
 * authors no calls (founder QA round 3's three-way split, re-cut in round 4
 * around the visited trail), and the chain-wide position label.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  fragmentCallLessBody,
  fragmentCallLessHeading,
  fragmentPositionLabel,
} from './fragmentCaption.ts';

void test('with no trail yet, a by-design control-flow step says the chain has not started', () => {
  for (const kind of ['merge', 'decision', 'fork', 'join', 'start', 'end', 'switch'] as const) {
    assert.equal(
      fragmentCallLessHeading('n1', kind, false),
      'No calls yet — step forward to begin the chain.'
    );
  }
});

void test('once a trail exists, a control-flow step says the walked chain stays lit', () => {
  for (const kind of ['merge', 'decision', 'fork', 'join', 'start', 'end', 'switch'] as const) {
    assert.equal(
      fragmentCallLessHeading('n1', kind, true),
      'Control flow — no calls; the chain so far stays lit'
    );
  }
});

void test('the entry chooser (blank node id) never reads as a realization gap', () => {
  assert.equal(
    fragmentCallLessHeading('', undefined, false),
    'No calls yet — step forward to begin the chain.'
  );
  assert.equal(
    fragmentCallLessHeading('', 'action', false),
    'No calls yet — step forward to begin the chain.'
  );
  assert.equal(fragmentCallLessHeading('', 'merge', true), 'Choose an entry to begin.');
});

void test('a call-eligible kind with nothing realized is a real gap, trail or not', () => {
  for (const kind of ['action', 'timeEvent', 'acceptEvent'] as const) {
    assert.equal(fragmentCallLessHeading('n1', kind, false), 'No realization for this step');
    assert.equal(fragmentCallLessHeading('n1', kind, true), 'No realization for this step');
  }
});

void test('an unknown kind (no focusStepKind wired) degrades to the conservative real-gap caption', () => {
  assert.equal(fragmentCallLessHeading('n1', undefined, false), 'No realization for this step');
  assert.equal(fragmentCallLessHeading('n1', undefined, true), 'No realization for this step');
});

void test('the explanatory body only appears when something is actually lit behind you', () => {
  assert.equal(fragmentCallLessBody(false), undefined);
  assert.equal(
    fragmentCallLessBody(true),
    'Nothing new is called here — what stays lit is the chain you have already walked.'
  );
});

void test('a single-call fragment reports one position; a multi-call fragment a range', () => {
  assert.equal(fragmentPositionLabel([5], 22), 'call 5 of 22');
  assert.equal(fragmentPositionLabel([5, 6, 7], 22), 'call 5–7 of 22');
});

void test('the range is the fragment min/max whatever order the seqs arrive in', () => {
  assert.equal(fragmentPositionLabel([9, 6, 7], 22), 'call 6–9 of 22');
});

void test('nothing lit, or nothing to be lit, yields no position label', () => {
  assert.equal(fragmentPositionLabel([], 22), undefined);
  assert.equal(fragmentPositionLabel([1], 0), undefined);
});
