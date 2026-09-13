/**
 * The row-label gutter (rowGutter.ts) — designer P1-6: labels track their
 * row's band as the canvas pans and zooms, at a size that never scales.
 */
/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import {
  CONTROLS_OFFSET_PX,
  GUTTER_LABEL_PX,
  GUTTER_PX,
  GUTTER_RAIL_PX,
  GUTTER_SOLID_PX,
  gutterLabelFontPx,
  gutterWidthFor,
  rowGutterLabels,
} from './rowGutter.ts';

const ROWS = [
  { row: 'client', y: 0, height: 86, label: 'Clients' },
  { row: 'manager', y: 158, height: 120, label: 'Managers' },
  { row: 'systemWide', y: 824, height: 86, label: 'System-wide' },
];

void test('a band is the row, scaled by zoom and moved by the y translation', () => {
  const managers = rowGutterLabels(ROWS, [0, 20, 0.5], 600).find((l) => l.row === 'manager');
  assert.ok(managers !== undefined);
  assert.equal(managers.top, 20 + 158 * 0.5);
  assert.equal(managers.height, 120 * 0.5);
});

void test('panning up or down moves every band by exactly the pan', () => {
  const a = rowGutterLabels(ROWS, [0, 0, 0.4], 600);
  const b = rowGutterLabels(ROWS, [0, -50, 0.4], 600);
  for (const [i, l] of a.entries()) assert.equal(b[i]?.top, l.top - 50, l.row);
});

void test('the gutter is PINNED: a sideways pan never moves a label', () => {
  assert.deepEqual(
    rowGutterLabels(ROWS, [-300, 12, 0.4], 600),
    rowGutterLabels(ROWS, [250, 12, 0.4], 600)
  );
});

void test('the label size never scales with zoom — readable at fit', () => {
  for (const zoom of [0.15, 0.4, 0.8, 1.4]) assert.equal(gutterLabelFontPx(zoom), GUTTER_LABEL_PX);
  assert.equal(GUTTER_LABEL_PX, 10);
});

void test('a band wholly above or below the canvas is not shown; a partial one is', () => {
  const zoomedIn = rowGutterLabels(ROWS, [0, -300, 1], 400);
  const byRow = new Map(zoomedIn.map((l) => [l.row, l]));
  assert.equal(byRow.get('client')?.visible, false, 'client band ends at -214');
  assert.equal(byRow.get('manager')?.visible, false, 'manager band ends at -22');
  assert.equal(byRow.get('systemWide')?.visible, false, 'system-wide starts at 524 > 400');
  const partial = rowGutterLabels(ROWS, [0, -100, 1], 400);
  assert.equal(partial.find((l) => l.row === 'client')?.visible, false);
  assert.equal(partial.find((l) => l.row === 'manager')?.visible, true, 'band 58..178');
});

void test('the zoom controls sit just clear of the gutter', () => {
  assert.equal(CONTROLS_OFFSET_PX, GUTTER_PX + 8);
});

void test('the full gutter stands while the first card column is clear of its solid ground', () => {
  assert.equal(GUTTER_SOLID_PX, 57);
  assert.equal(gutterWidthFor(GUTTER_SOLID_PX), GUTTER_PX);
  assert.equal(gutterWidthFor(200), GUTTER_PX);
});

void test('once cards slide beneath its solid ground, the gutter is a 14px rail', () => {
  assert.equal(GUTTER_RAIL_PX, 14);
  assert.equal(gutterWidthFor(GUTTER_SOLID_PX - 1), GUTTER_RAIL_PX);
  assert.equal(gutterWidthFor(0), GUTTER_RAIL_PX);
  assert.equal(gutterWidthFor(-400), GUTTER_RAIL_PX);
  assert.equal(gutterWidthFor(Number.NaN), GUTTER_RAIL_PX);
});
