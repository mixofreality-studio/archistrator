import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  isToolbarStuck,
  lensGeometryVars,
  PANE_MAX_HEIGHT,
  PANE_STICKY_TOP,
} from './lensGeometry.ts';

void test('the published toolbar height and row offset round up; the scroller height rounds down', () => {
  assert.deepEqual(lensGeometryVars(85.2, 811.7, 190.1), {
    '--lens-toolbar-h': '86px',
    '--lens-scroll-h': '811px',
    '--lens-row-top': '191px',
  });
  // Scrolled past: the row offset goes negative, and the cap's max() clamps it.
  assert.equal(lensGeometryVars(51, 700, -412.6)['--lens-row-top'], '-412px');
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
