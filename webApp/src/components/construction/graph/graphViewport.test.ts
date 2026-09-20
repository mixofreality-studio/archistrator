/**
 * The GRAPH lens's viewport memory and LOD rule (graphViewport.ts) — pure, so
 * pinned directly under node:test.
 */
/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

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
  railGapPx,
  selectionOutlinePx,
  visibleCanvasWidthPx,
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
// STATUS-INVARIANCE — the property the viewport memory rests on (spec §7.6,
// AC6; final main review I3)
//
// The store is keyed by the signature, so anything the signature reads becomes
// something that RESETS the operator's pan and zoom when it changes. Statuses,
// attempts and provenance change on the 1.5s cascade poll: folding any of them
// in re-fits the canvas mid-glance — the recorded, previously-fixed bug §7.6
// calls a mandatory carry-over. The tests above all vary ids, so that mistake
// used to pass them silently. These three make it fail:
//   - the shape pin: the whole signature string, for a known input;
//   - the arity pin: a fourth input cannot be added unnoticed;
//   - the source pins: neither the function nor its ONE call site may name
//     status-ish evidence. A call site can reach the same bug without touching
//     the function at all, by folding a status into the ids it passes.
// ---------------------------------------------------------------------------

/** Evidence vocabulary: none of it may reach the signature, here or at the call site. */
const EVIDENCE_WORDS = [
  'status',
  'attempt',
  'provenance',
  'origin',
  'percent',
  'complete',
  'phase',
  'evidence',
  'owed',
  'failed',
  'observed',
];

function source(file: string): string {
  return readFileSync(new URL(file, import.meta.url), 'utf8');
}

/** The text of `name(...)`, from its opening paren to the paren that closes it. */
function callText(src: string, name: string): string {
  const at = src.indexOf(`${name}(`);
  assert.ok(at >= 0, `${name}( is called`);
  let depth = 0;
  for (let i = at + name.length; i < src.length; i += 1) {
    if (src[i] === '(') depth += 1;
    else if (src[i] === ')') {
      depth -= 1;
      if (depth === 0) return src.slice(at, i + 1);
    }
  }
  throw new Error(`${name}( is never closed`);
}

function namesNoEvidence(text: string, where: string): void {
  for (const word of EVIDENCE_WORDS) {
    assert.ok(
      !new RegExp(word, 'i').test(text),
      `${where} names "${word}" — the graph viewport signature must not depend on evidence that moves under the poll`
    );
  }
}

void test('the signature is exactly project + component ids + activity ids — nothing else', () => {
  // The whole string, so a fourth field (or a fourth input folded into an
  // existing one) changes it and fails here.
  assert.equal(graphSignatureOf('p', ['a', 'b'], ['C-x']), 'p|c2:a,b|a1:C-x');
  assert.equal(
    graphSignatureOf.length,
    3,
    'graphSignatureOf takes exactly projectId, componentIds, activityIds'
  );
});

void test('the signature function itself reads no status, attempt or provenance', () => {
  const src = source('./graphViewport.ts');
  const at = src.indexOf('export function graphSignatureOf(');
  assert.ok(at >= 0, 'graphSignatureOf is declared');
  const end = src.indexOf('\n}', at);
  assert.ok(end > at, 'its body closes');
  // From the declaration (NOT the docblock above it, which names those words to
  // rule them out) to the end of the body.
  namesNoEvidence(src.slice(at, end), 'graphSignatureOf');
});

void test("the lens's call site passes only ids, so no poll-borne value reaches the signature", () => {
  namesNoEvidence(
    callText(source('./ActivityGraphLens.tsx'), 'graphSignatureOf'),
    'the ActivityGraphLens call to graphSignatureOf'
  );
});

void test('two polls of the same plan that differ only in status share one signature', () => {
  // The call site's own derivation (ids off the model), over the same plan read
  // twice: once mid-flight, once with an activity completed.
  const sig = (activities: readonly { activityId: string; status: string }[]): string =>
    graphSignatureOf(
      'archistrator',
      ['billing-manager', 'usage-access'],
      activities.map((a) => a.activityId)
    );
  assert.equal(
    sig([
      { activityId: 'C-billing-manager', status: 'running' },
      { activityId: 'C-usage-access', status: 'not-started' },
    ]),
    sig([
      { activityId: 'C-billing-manager', status: 'done' },
      { activityId: 'C-usage-access', status: 'running' },
    ])
  );
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
// The selection outline (designer P2)
// ---------------------------------------------------------------------------

void test('the selection outline is 2/zoom, clamped to 2–5px', () => {
  assert.equal(selectionOutlinePx(1), 2);
  assert.equal(selectionOutlinePx(0.5), 4);
  assert.equal(selectionOutlinePx(0.4), 5);
  assert.equal(selectionOutlinePx(0.2), 5, 'clamped at 5');
  assert.equal(selectionOutlinePx(1.4), 2, 'clamped at 2');
});

void test('a zoom that is not a positive number falls back to the minimum', () => {
  assert.equal(selectionOutlinePx(0), 2);
  assert.equal(selectionOutlinePx(Number.NaN), 2);
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

void test('the rail gap keeps the critical edge and the provenance rail 2 SCREEN px apart from fit down to 0.25', () => {
  for (let zoom = 0.25; zoom <= 3; zoom += 0.05) {
    assert.ok(
      railGapPx(zoom) * zoom >= 2 - 1e-9,
      `zoom ${zoom.toFixed(2)}: ${String(railGapPx(zoom))}`
    );
    assert.ok(railGapPx(zoom) >= 4, 'never under the old 4px');
    assert.ok(railGapPx(zoom) <= 8, 'never wider than 8px');
  }
  assert.equal(railGapPx(0), 4);
  assert.equal(railGapPx(Number.NaN), 4);
});

void test('a framed card centres in the canvas the drawer leaves visible', () => {
  // No drawer (or one past the canvas): the whole canvas.
  assert.equal(visibleCanvasWidthPx(32, 1068, undefined), 1036);
  assert.equal(visibleCanvasWidthPx(32, 1068, 1100), 1036);
  // The 1100 layout: a 480px drawer from x 620 covers the canvas's right 448px.
  assert.equal(visibleCanvasWidthPx(32, 1068, 620), 588);
  // A drawer over nearly all of it: never under the floor, never over the canvas.
  assert.equal(visibleCanvasWidthPx(32, 1068, 40), 240);
  assert.equal(visibleCanvasWidthPx(0, 200, 10), 200);
});
