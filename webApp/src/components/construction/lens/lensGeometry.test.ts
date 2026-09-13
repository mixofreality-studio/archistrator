import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  PANE_BOTTOM_GAP_PX,
  isToolbarStuck,
  lensGeometryVars,
  PANE_MAX_HEIGHT,
  PANE_STICKY_TOP,
  varsToWrite,
} from './lensGeometry.ts';

void test('the published toolbar height and row offset round up; the scroller height rounds down', () => {
  assert.deepEqual(lensGeometryVars(85.2, 811.7, 190.1), {
    '--lens-toolbar-h': '86px',
    '--lens-scroll-h': '811px',
    '--lens-row-top': '191px',
  });
});

void test('the row offset never goes below the pinned offset, so scrolling past stops changing it', () => {
  // Scrolled past: the raw offset goes negative; published, it holds at toolbar + 8.
  assert.equal(lensGeometryVars(51, 700, -412.6)['--lens-row-top'], '59px');
  assert.equal(lensGeometryVars(51, 700, -9000)['--lens-row-top'], '59px');
  // At rest, below the pinned offset's reach, the real offset is published.
  assert.equal(lensGeometryVars(51, 700, 190.1)['--lens-row-top'], '191px');
});

void test('only a value that changed is written', () => {
  const first = lensGeometryVars(51, 700, 190);
  assert.deepEqual(varsToWrite({}, first).length, 3);
  assert.deepEqual(varsToWrite(first, lensGeometryVars(51, 700, 190)), []);
  // Two scroll steps past the pinned offset: nothing to write.
  const pinned = lensGeometryVars(51, 700, -100);
  assert.deepEqual(varsToWrite(pinned, lensGeometryVars(51, 700, -300)), []);
  assert.deepEqual(varsToWrite(first, lensGeometryVars(86, 700, 190)), [
    ['--lens-toolbar-h', '86px'],
  ]);
});

void test('the pane pins below the MEASURED toolbar, never at a constant offset', () => {
  assert.equal(PANE_STICKY_TOP, 'calc(var(--lens-toolbar-h, 0px) + 8px)');
  assert.doesNotMatch(PANE_STICKY_TOP + PANE_MAX_HEIGHT, /76px/);
});

void test('the cap is the room the pane really has — at rest (row offset) and pinned (toolbar) alike', () => {
  // Not 100vh (which ignores the app chrome above the scroller) and not only the
  // pinned offset (which leaves the action bar below the fold at rest).
  assert.equal(
    PANE_MAX_HEIGHT,
    'calc(var(--lens-scroll-h, 100vh) - max(var(--lens-row-top, 0px), var(--lens-toolbar-h, 0px) + 8px) - 16px)'
  );
});

void test('the toolbar is stuck only once the scroller has moved and it sits at the top edge', () => {
  assert.equal(isToolbarStuck({ toolbarTop: 150, scrollerTop: 63, scrollTop: 0 }), false);
  assert.equal(isToolbarStuck({ toolbarTop: 63, scrollerTop: 63, scrollTop: 0 }), false);
  assert.equal(isToolbarStuck({ toolbarTop: 63.5, scrollerTop: 63, scrollTop: 240 }), true);
  // Scrolled a little, but the header is still partly on screen above it.
  assert.equal(isToolbarStuck({ toolbarTop: 120, scrollerTop: 63, scrollTop: 30 }), false);
});

void test("the published scroll height nets out the scroller's bottom padding — the pane ends where content does", () => {
  // At 1280 the scroller is 737px tall with 23px of bottom padding: a pane capped
  // against 737 overflowed it by 23 − 16 = 7px (designer re-check 2).
  assert.equal(lensGeometryVars(86, 737, 190, 23)['--lens-scroll-h'], '714px');
  assert.equal(lensGeometryVars(86, 737, 190)['--lens-scroll-h'], '737px');
  // The pane's bottom (row top + cap) plus the padding never passes the client height.
  const scrollH = Number.parseFloat(lensGeometryVars(86, 737, 190, 23)['--lens-scroll-h'] ?? '0');
  const cap = scrollH - 190 - PANE_BOTTOM_GAP_PX;
  assert.ok(190 + cap + 23 <= 737);
});
