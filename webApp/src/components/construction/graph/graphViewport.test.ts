/**
 * The GRAPH lens's viewport memory and LOD rule (graphViewport.ts) — pure, so
 * pinned directly under node:test.
 */
/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import {
  LOD1_MIN_ZOOM,
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
