/**
 * The segment-code fit rule (segmentCode.ts): a code renders only when its
 * measured text fits its segment — otherwise nothing, never "R…".
 */
/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import { SEGMENT_CODE_LETTER_SPACING, segmentCodeFits } from './segmentCode.ts';

void test('a code fits when its measured text is no wider than its segment', () => {
  assert.equal(segmentCodeFits(14.2, 30), true);
  assert.equal(segmentCodeFits(30, 30), true);
});

void test('a code wider than its segment does not fit — it renders nothing, never a clipped fragment', () => {
  assert.equal(segmentCodeFits(14.2, 14.1), false);
  assert.equal(segmentCodeFits(22, 9), false);
});

void test('an unmeasured or empty width never fits', () => {
  assert.equal(segmentCodeFits(0, 30), false);
  assert.equal(segmentCodeFits(Number.NaN, 30), false);
  assert.equal(segmentCodeFits(10, Number.NaN), false);
  assert.equal(segmentCodeFits(10, 0), false);
});

void test('codes are set with no letter-spacing, so the measure is the text alone', () => {
  assert.equal(SEGMENT_CODE_LETTER_SPACING, 0);
});
