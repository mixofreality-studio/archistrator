/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import { fitToWidth } from './fitContent.ts';

const BASE = { minZoom: 0.3, maxZoom: 1, gutter: 24, frame: 3, minHeight: 160 } as const;

void test('a small drawing in a wide pane is drawn at 1.0, never enlarged', () => {
  // The two-node relationships fact: 470 × 330 in a 1389px pane fit to 1.12–1.23 before.
  const fit = fitToWidth({
    ...BASE,
    paneWidth: 1389,
    bounds: { x: 0, y: 0, width: 470, height: 330 },
  });
  assert.equal(fit.zoom, 1);
  // The canvas is the drawing's height, not a guess: 330 + 2 × 24 + 3.
  assert.equal(fit.height, 381);
});

void test('the drawing is centred across and top-aligned at the gutter', () => {
  const fit = fitToWidth({
    ...BASE,
    paneWidth: 1000,
    bounds: { x: -100, y: -40, width: 560, height: 400 },
  });
  assert.equal(fit.zoom, 1);
  assert.equal(fit.x, (1000 - 560) / 2 + 100);
  // The drawing's top edge (y = -40) lands at the gutter.
  assert.equal(fit.y + -40 * fit.zoom, 24);
});

void test('a drawing wider than the pane zooms out by the width, down to the floor', () => {
  const wide = fitToWidth({
    ...BASE,
    minZoom: 0.9,
    paneWidth: 1389,
    bounds: { x: 0, y: 0, width: 1458, height: 900 },
  });
  assert.ok(Math.abs(wide.zoom - (1389 - 48) / 1458) < 1e-9);
  assert.equal(wide.height, Math.ceil(900 * wide.zoom) + 48 + 3);
  // Past the floor the zoom holds and the reader pans.
  const floored = fitToWidth({
    ...BASE,
    minZoom: 0.9,
    paneWidth: 800,
    bounds: { x: 0, y: 0, width: 1458, height: 900 },
  });
  assert.equal(floored.zoom, 0.9);
});

void test('the height stays within its bounds', () => {
  const tiny = fitToWidth({
    ...BASE,
    paneWidth: 800,
    bounds: { x: 0, y: 0, width: 100, height: 20 },
  });
  assert.equal(tiny.height, 160);
  const tall = fitToWidth({
    ...BASE,
    maxHeight: 640,
    paneWidth: 800,
    bounds: { x: 0, y: 0, width: 300, height: 2000 },
  });
  assert.equal(tall.height, 640);
});

void test('an unmeasured drawing (zero width) takes the cap, not a division by zero', () => {
  const fit = fitToWidth({ ...BASE, paneWidth: 800, bounds: { x: 0, y: 0, width: 0, height: 0 } });
  assert.equal(fit.zoom, 1);
  assert.ok(Number.isFinite(fit.x));
});
