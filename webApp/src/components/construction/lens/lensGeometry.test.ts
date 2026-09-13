import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  centeredScrollFor,
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

// Designer final N1: the linked row is centred in the band below the toolbar —
// and where the list ends too soon to scroll that far, a runway makes the room.
void test('a row mid-list is centred with no runway', () => {
  // Band 86..837 (mid 461.5); row mid at 1500 → scrollTop 1039; room to 1500.
  assert.deepEqual(
    centeredScrollFor({
      rowTop: 1490,
      rowHeight: 20,
      bandTop: 86,
      bandBottom: 837,
      contentHeight: 2400,
      clientHeight: 837,
    }),
    { scrollTop: 1039, runwayPx: 0 }
  );
});

void test('a row near the list’s end gets the runway it needs instead of landing low', () => {
  // The measured 1280 case: max scroll 870, but centring needs 1100.
  const plan = centeredScrollFor({
    rowTop: 1551,
    rowHeight: 20,
    bandTop: 86,
    bandBottom: 837,
    contentHeight: 1707,
    clientHeight: 837,
  });
  assert.equal(plan.scrollTop, 1100);
  assert.equal(plan.runwayPx, 230);
});

void test('a row near the top never scrolls negative, and needs no runway', () => {
  assert.deepEqual(
    centeredScrollFor({
      rowTop: 100,
      rowHeight: 20,
      bandTop: 86,
      bandBottom: 837,
      contentHeight: 3000,
      clientHeight: 837,
    }),
    { scrollTop: 0, runwayPx: 0 }
  );
});
