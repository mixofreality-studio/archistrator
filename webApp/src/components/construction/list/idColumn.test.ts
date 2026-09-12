import { test } from 'node:test';
import assert from 'node:assert/strict';
import { ID_COLUMN_MAX_CH, ID_COLUMN_MIN_CH, idColumnWidthCh } from './activityRowPresentation.ts';

void test('the id column fits the longest id on screen, plus one ch of clearance', () => {
  assert.equal(idColumnWidthCh(['C-review-engine', 'C-operated-system-state-access']), 31);
});

void test('the width is clamped to 12..32 ch', () => {
  assert.equal(ID_COLUMN_MIN_CH, 12);
  assert.equal(ID_COLUMN_MAX_CH, 32);
  assert.equal(idColumnWidthCh(['N-IT']), 12);
  assert.equal(idColumnWidthCh([]), 12);
  assert.equal(idColumnWidthCh(['R-an-id-that-is-far-too-long-to-fit-anywhere']), 32);
  // The longest live id (31 chars) still fits inside the clamp.
  assert.equal(idColumnWidthCh(['R-construction-pipeline-runtime']), 32);
});
