/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import { CODE_CANVAS_MIN_WIDTH, codeTabModeFor } from './contractCode.ts';
import {
  codeCanvasWindowFor,
  FOCUS_RAIL_STORAGE_KEY,
  focusWindowFor,
  needsRoomCopy,
  readRailCollapsed,
  writeRailCollapsed,
  type RailStorage,
} from './focusRail.ts';

/** The artifact column a focus window leaves, rail open or collapsed (the inverse of focusWindowFor). */
function column(windowWidth: number, railOpen: boolean): number {
  return windowWidth - focusWindowFor(0, railOpen);
}

void test('the code diagram needs a 1712px window with the rail open, 1392 collapsed', () => {
  assert.equal(codeCanvasWindowFor(true), 1712);
  assert.equal(codeCanvasWindowFor(false), 1392);
  assert.equal(focusWindowFor(CODE_CANVAS_MIN_WIDTH, true), 1712);
});

void test('the threshold, pinned either side, with the rail open and collapsed', () => {
  assert.equal(codeTabModeFor(column(1711, true), true), 'list');
  assert.equal(codeTabModeFor(column(1712, true), true), 'canvas');
  assert.equal(codeTabModeFor(column(1391, false), true), 'list');
  assert.equal(codeTabModeFor(column(1392, false), true), 'canvas');
});

void test('a 14-inch MacBook (1512) draws the diagram with the rail collapsed, lists with it open', () => {
  assert.equal(column(1512, true), 1144);
  assert.equal(codeTabModeFor(column(1512, true), true), 'list');
  assert.equal(column(1512, false), 1464);
  assert.equal(codeTabModeFor(column(1512, false), true), 'canvas');
  // The windows that listed before still list with the rail open.
  for (const w of [1100, 1280, 1366, 1600]) {
    assert.equal(codeTabModeFor(column(w, true), true), 'list', `open at ${String(w)}`);
  }
  assert.equal(codeTabModeFor(column(1760, true), true), 'canvas');
});

void test('the needs-room copy says the window, and offers the collapse where it helps', () => {
  const open = needsRoomCopy({ railOpen: true, collapsible: true });
  assert.equal(open.text, 'Needs a window about 1712 px wide — or collapse the side panel');
  assert.equal(open.action, 'collapse');
  const collapsed = needsRoomCopy({ railOpen: false, collapsible: true });
  assert.equal(collapsed.text, 'Needs a window about 1392 px wide');
  assert.equal(collapsed.action, undefined);
  // Below 600px the rail stacks above; there is nothing to collapse here.
  const stacked = needsRoomCopy({ railOpen: true, collapsible: false });
  assert.equal(stacked.text, 'Needs a window about 1392 px wide, with the side panel collapsed');
  assert.equal(stacked.action, undefined);
});

function memory(): RailStorage & { data: Map<string, string> } {
  const data = new Map<string, string>();
  return {
    data,
    getItem: (k) => data.get(k) ?? null,
    setItem: (k, v): void => {
      data.set(k, v);
    },
  };
}

void test('the collapse is remembered per viewer, and nothing unreadable collapses it', () => {
  const s = memory();
  assert.equal(readRailCollapsed(s), false, 'no stored value: open');
  writeRailCollapsed(true, s);
  assert.equal(s.data.get(FOCUS_RAIL_STORAGE_KEY), '1');
  assert.equal(readRailCollapsed(s), true);
  writeRailCollapsed(false, s);
  assert.equal(readRailCollapsed(s), false);
  s.data.set(FOCUS_RAIL_STORAGE_KEY, 'yes');
  assert.equal(readRailCollapsed(s), false, 'a foreign value: open');
  assert.equal(readRailCollapsed(undefined), false, 'no storage at all: open');
});

void test('a storage that throws is survived, read and write', () => {
  const throwing: RailStorage = {
    getItem: () => {
      throw new Error('SecurityError');
    },
    setItem: () => {
      throw new Error('QuotaExceededError');
    },
  };
  assert.equal(readRailCollapsed(throwing), false);
  assert.doesNotThrow(() => {
    writeRailCollapsed(true, throwing);
  });
});
