/// <reference types="node" />
/**
 * Unit tests for the artifact-header StageChip mapping (src/components/design/
 * headerChipStage.ts). Covers the chip-honesty fix: after a founder "Send back"
 * the session stage is 'redrafting' but the header chip read "NOT DRAFTED"
 * (the old inline ternary only special-cased awaitingReview). drafting and
 * redrafting must surface as generation-in-progress chip stages; every other
 * precedence rule of the original mapping is preserved byte-identically.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { headerChipStage } from './headerChipStage.ts';

void test('redrafting (post send-back) reads as REDRAFTING, not NOT DRAFTED', () => {
  assert.equal(headerChipStage(false, 'redrafting'), 'redrafting');
});

void test('initial drafting reads as DRAFTING, not NOT DRAFTED', () => {
  assert.equal(headerChipStage(false, 'drafting'), 'drafting');
});

void test('a committed slot wins over any session stage (unchanged precedence)', () => {
  assert.equal(headerChipStage(true, 'redrafting'), 'committed');
  assert.equal(headerChipStage(true, 'awaitingReview'), 'committed');
  assert.equal(headerChipStage(true, undefined), 'committed');
});

void test('awaitingReview maps to the review chip (unchanged)', () => {
  assert.equal(headerChipStage(false, 'awaitingReview'), 'awaitingReview');
});

void test('no session at all is genuinely NOT DRAFTED', () => {
  assert.equal(headerChipStage(false, undefined), 'empty');
});

void test('terminal/other session stages still fall back to empty (unchanged)', () => {
  assert.equal(headerChipStage(false, 'withdrawn'), 'empty');
  assert.equal(headerChipStage(false, 'refused'), 'empty');
  assert.equal(headerChipStage(false, 'draftFailed'), 'empty');
  assert.equal(headerChipStage(false, 'committed'), 'empty');
  assert.equal(headerChipStage(false, 'unknown'), 'empty');
});
