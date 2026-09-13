/**
 * The GRAPH lens's viewport memory and LOD rule (graphViewport.ts) — pure, so
 * pinned directly under node:test.
 */
/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import {
  CANVAS_BOTTOM_PAD_PX,
  CANVAS_MIN_PX,
  LOD1_MIN_ZOOM,
  VIEWPORT_STORE_LIMIT,
  canvasHeightPx,
  graphMountFor,
  graphSignatureOf,
  loadGraphViewport,
  lodFor,
  saveGraphViewport,
} from './graphViewport.ts';

// ---------------------------------------------------------------------------
// The signature
// ---------------------------------------------------------------------------

void test('the signature ignores the order the ids arrive in', () => {
  assert.equal(
    graphSignatureOf('p', ['b', 'a'], ['C-y', 'C-x']),
    graphSignatureOf('p', ['a', 'b'], ['C-x', 'C-y'])
  );
});

void test('the signature changes when the component set changes', () => {
  assert.notEqual(
    graphSignatureOf('p', ['a'], ['C-x']),
    graphSignatureOf('p', ['a', 'b'], ['C-x'])
  );
});

void test('the signature changes when the activity set changes', () => {
  assert.notEqual(
    graphSignatureOf('p', ['a'], ['C-x']),
    graphSignatureOf('p', ['a'], ['C-x', 'C-y'])
  );
});

void test('a component id can never collide with an activity id in the signature', () => {
  assert.notEqual(graphSignatureOf('p', ['a', 'b'], []), graphSignatureOf('p', ['a'], ['b']));
});

void test('the signature is per project', () => {
  assert.notEqual(graphSignatureOf('p', ['a'], ['x']), graphSignatureOf('q', ['a'], ['x']));
});

// ---------------------------------------------------------------------------
// The store
// ---------------------------------------------------------------------------

void test('nothing is stored for a signature never saved — the canvas then fits the view', () => {
  assert.equal(loadGraphViewport('never-saved'), undefined);
});

void test('the store round-trips a viewport and keeps signatures apart', () => {
  saveGraphViewport('sig-a', { x: 10, y: -20, zoom: 0.5 });
  saveGraphViewport('sig-b', { x: 1, y: 2, zoom: 1.1 });
  assert.deepEqual(loadGraphViewport('sig-a'), { x: 10, y: -20, zoom: 0.5 });
  assert.deepEqual(loadGraphViewport('sig-b'), { x: 1, y: 2, zoom: 1.1 });
});

void test('the store hands back a copy, so a caller cannot rewrite it by mutation', () => {
  saveGraphViewport('sig-c', { x: 0, y: 0, zoom: 1 });
  const got = loadGraphViewport('sig-c');
  assert.ok(got !== undefined);
  got.zoom = 9;
  assert.deepEqual(loadGraphViewport('sig-c'), { x: 0, y: 0, zoom: 1 });
});

void test('the store is bounded: past the limit the least recently saved signature is dropped', () => {
  const sig = (i: number): string => `bound-${String(i)}`;
  for (let i = 0; i < VIEWPORT_STORE_LIMIT; i += 1)
    saveGraphViewport(sig(i), { x: i, y: 0, zoom: 1 });
  // Re-saving bound-0 makes it the most recent; bound-1 is now the oldest.
  saveGraphViewport(sig(0), { x: 0, y: 1, zoom: 1 });
  saveGraphViewport(sig(VIEWPORT_STORE_LIMIT), { x: 99, y: 0, zoom: 1 });
  assert.equal(loadGraphViewport(sig(1)), undefined, 'the oldest is evicted');
  assert.deepEqual(loadGraphViewport(sig(0)), { x: 0, y: 1, zoom: 1 }, 'a re-save refreshes');
  assert.deepEqual(loadGraphViewport(sig(VIEWPORT_STORE_LIMIT)), { x: 99, y: 0, zoom: 1 });
  assert.deepEqual(loadGraphViewport(sig(2)), { x: 2, y: 0, zoom: 1 });
});

void test('the signature sorts by code unit, never by locale', () => {
  // localeCompare puts "a" before "B"; code-unit order puts "B" (0x42) first.
  assert.equal(graphSignatureOf('p', ['a', 'B'], []), 'p|c2:B,a|a0:');
});

void test('a non-finite viewport is refused rather than restored as NaN', () => {
  saveGraphViewport('sig-d', { x: Number.NaN, y: 0, zoom: 1 });
  assert.equal(loadGraphViewport('sig-d'), undefined);
});

// ---------------------------------------------------------------------------
// Level of detail
// ---------------------------------------------------------------------------

void test('LOD-0 below the threshold', () => {
  assert.equal(lodFor(LOD1_MIN_ZOOM - 0.01, false), 0);
});

void test('LOD-1 at and above the 0.8 threshold', () => {
  assert.equal(LOD1_MIN_ZOOM, 0.8);
  assert.equal(lodFor(0.8, false), 1);
  assert.equal(lodFor(1.3, false), 1);
});

void test('LOD-1 under hover at any zoom', () => {
  assert.equal(lodFor(0.3, true), 1);
});

// ---------------------------------------------------------------------------
// The mount read
// ---------------------------------------------------------------------------

void test('a deep link with no remembered viewport frames its card once', () => {
  const m = graphMountFor('mount-a', 'C-x', { 'C-x': 'x-card' });
  assert.equal(m.stored, undefined);
  assert.equal(m.initialFocus, 'x-card');
});

void test('a remembered viewport wins over a deep link — no framing', () => {
  saveGraphViewport('mount-b', { x: 5, y: 6, zoom: 0.7 });
  const m = graphMountFor('mount-b', 'C-x', { 'C-x': 'x-card' });
  assert.deepEqual(m.stored, { x: 5, y: 6, zoom: 0.7 });
  assert.equal(m.initialFocus, undefined);
});

void test('nothing is framed without a selection, or for an activity with no card', () => {
  assert.equal(graphMountFor('mount-c', undefined, { 'C-x': 'x-card' }).initialFocus, undefined);
  assert.equal(graphMountFor('mount-d', 'C-gone', { 'C-x': 'x-card' }).initialFocus, undefined);
});

// ---------------------------------------------------------------------------
// The canvas height
// ---------------------------------------------------------------------------

void test('the canvas fills from its resting top to the scroller bottom, less the pad', () => {
  assert.equal(canvasHeightPx(1000, 380), 1000 - 380 - CANVAS_BOTTOM_PAD_PX);
});

void test("the scroller's own bottom padding is room the canvas cannot take", () => {
  assert.equal(canvasHeightPx(1000, 380, 24), 1000 - 24 - 380 - CANVAS_BOTTOM_PAD_PX);
});

void test('P1-2: the floor is about 280px, so the canvas fits above the fold at 1366×768', () => {
  assert.equal(CANVAS_MIN_PX, 280);
  // 768 tall, canvas resting at ~350 under a one-line ribbon and check row:
  // the measured height, not the floor, decides.
  assert.equal(canvasHeightPx(768, 350, 24), 768 - 24 - 350 - CANVAS_BOTTOM_PAD_PX);
});

void test('the canvas never shrinks below its minimum — the page scrolls instead', () => {
  assert.equal(canvasHeightPx(600, 400), CANVAS_MIN_PX);
});

void test('a non-finite measurement falls back to the minimum, never NaN', () => {
  assert.equal(canvasHeightPx(Number.NaN, 0), CANVAS_MIN_PX);
});
