/// <reference types="node" />
/**
 * Unit tests for seqChipLabel: the FRAGMENT-MODE-ONLY gate on the canvas'
 * current-call chip using the alt-aware label (fix round 1, call-chain
 * rollout Task 5 review, FINDING 1 — the same-call contradiction between the
 * chip and StepBar's step-through caption).
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { seqChipLabel } from './seqChipLabel.ts';

void test('fragment mode: an alt-labeled call shows its altLabel, not the raw seq', () => {
  assert.equal(seqChipLabel(true, { seq: 18, altLabel: '1a' }), '1a');
});

void test('fragment mode: a plain call (no altLabel) shows the raw seq', () => {
  assert.equal(seqChipLabel(true, { seq: 18 }), 18);
});

void test('step-through mode (fragmentMode=false): ALWAYS the raw seq, even when altLabel is present', () => {
  // FINDING 1 — the same-call contradiction: the step-through's StepBar caption
  // states this call's position as "Step N of Total" off the raw seq; the chip
  // must agree with it, never with a step-local alt relabeling.
  assert.equal(seqChipLabel(false, { seq: 18, altLabel: '1a' }), 18);
});

void test('step-through mode with no altLabel: unaffected — matches pre-fix behavior', () => {
  assert.equal(seqChipLabel(false, { seq: 18 }), 18);
});
