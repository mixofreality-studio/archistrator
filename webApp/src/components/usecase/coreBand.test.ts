/// <reference types="node" />
/**
 * Unit tests for the ch. 4 core-count band context on the use-case corpus
 * summary line: the Method targets 2–6 core use cases, so the label is a
 * constant "target 2–6" and inBand flips the warning accent when the committed
 * core count falls outside that band.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { coreBand, CORE_TARGET_MIN, CORE_TARGET_MAX } from './coreBand.ts';

void test('the band label names the 2–6 target', () => {
  assert.equal(CORE_TARGET_MIN, 2);
  assert.equal(CORE_TARGET_MAX, 6);
  assert.equal(coreBand(4).label, 'target 2–6');
});

void test('counts inside the band are in-band (inclusive bounds)', () => {
  assert.equal(coreBand(2).inBand, true);
  assert.equal(coreBand(4).inBand, true);
  assert.equal(coreBand(6).inBand, true);
});

void test('counts outside the band carry the warning flag', () => {
  assert.equal(coreBand(0).inBand, false);
  assert.equal(coreBand(1).inBand, false);
  assert.equal(coreBand(7).inBand, false);
});
