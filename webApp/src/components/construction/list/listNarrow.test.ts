/**
 * The list below LIST_NARROW_BELOW_PX (designer final pass, item 6): at 500px the
 * id and title were crushed to "C-…" and "B…". There the grid drops the kind and
 * provenance TRACKS — a 0px track would still carry a gap on each side — and the
 * view takes those two cells out of the grid, so every later cell keeps its column.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  LIST_COMPACT_BELOW_PX,
  LIST_NARROW_BELOW_PX,
  PROVENANCE_SLOT_PX,
  activityGridColumns,
  listSlotVars,
} from './activityRowPresentation.ts';

void test('narrow is below compact, at 600px', () => {
  assert.equal(LIST_NARROW_BELOW_PX, 600);
  assert.equal(LIST_COMPACT_BELOW_PX, 1000);
});

void test('the full grid carries nine tracks, the provenance width from its variable', () => {
  const full = activityGridColumns(4).split(' var(').length;
  assert.ok(activityGridColumns(4).includes('var(--list-kind-w) var(--list-provenance-w)'));
  assert.equal(listSlotVars('wide')['--list-provenance-w'], `${String(PROVENANCE_SLOT_PX)}px`);
  assert.equal(listSlotVars('compact')['--list-provenance-w'], `${String(PROVENANCE_SLOT_PX)}px`);
  assert.ok(full > 1);
});

void test('the narrow grid is the full grid without the kind and provenance tracks', () => {
  const narrow = activityGridColumns(4, 'narrow');
  assert.equal(
    narrow,
    '4px 18px var(--list-float-w) var(--list-effort-w) minmax(0, 1fr) var(--list-progress-w) 100px'
  );
  assert.ok(!narrow.includes('--list-kind-w'));
  assert.ok(!narrow.includes('--list-provenance-w'));
  // Every other track is the full grid's, in order.
  const full = activityGridColumns(4)
    .replace(' var(--list-kind-w)', '')
    .replace(' var(--list-provenance-w)', '');
  assert.equal(narrow, full);
});
