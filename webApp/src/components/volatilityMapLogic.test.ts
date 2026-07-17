/// <reference types="node" />
/**
 * Unit tests for the VolatilityMap's pure logic
 * (src/components/volatilityMapLogic.ts). Covers:
 *
 *   • the single-select listbox semantics extracted from the lanes — roving
 *     tab stop: ↑/↓ move and CLAMP at the lane bounds (no wrap), Home/End jump,
 *     Enter/Space select the focused option, anything else (including Tab and
 *     Escape, which the map root owns) is a no-op; an empty lane never
 *     produces an action; the live-region announcement composes the shared
 *     axis label;
 *   • the axes-overview geometry (axesLayout) — dots evenly spaced ALONG their
 *     own axis line, strictly between origin and arrow tip, viewport growing
 *     with the counts, dotless axes still drawing a visible axis;
 *   • the rejected-candidate classification labels and dot-label truncation.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  axesLayout,
  axisLabel,
  laneKeyAction,
  rejectionClassLabel,
  selectionAnnouncement,
  truncateLabel,
} from './volatilityMapLogic.ts';

void test('ArrowDown/ArrowUp move the focus position by one', () => {
  assert.deepEqual(laneKeyAction('ArrowDown', 1, 5), { kind: 'move', index: 2 });
  assert.deepEqual(laneKeyAction('ArrowUp', 3, 5), { kind: 'move', index: 2 });
});

void test('movement clamps at the lane bounds instead of wrapping', () => {
  assert.deepEqual(laneKeyAction('ArrowDown', 4, 5), { kind: 'move', index: 4 });
  assert.deepEqual(laneKeyAction('ArrowUp', 0, 5), { kind: 'move', index: 0 });
});

void test('Home/End jump to the first/last option', () => {
  assert.deepEqual(laneKeyAction('Home', 3, 5), { kind: 'move', index: 0 });
  assert.deepEqual(laneKeyAction('End', 1, 5), { kind: 'move', index: 4 });
});

void test('Enter and Space select the focused option', () => {
  assert.deepEqual(laneKeyAction('Enter', 2, 5), { kind: 'select' });
  assert.deepEqual(laneKeyAction(' ', 0, 1), { kind: 'select' });
});

void test('other keys are a no-op — Tab and Escape are not lane concerns', () => {
  assert.deepEqual(laneKeyAction('Tab', 2, 5), { kind: 'none' });
  assert.deepEqual(laneKeyAction('Escape', 2, 5), { kind: 'none' });
  assert.deepEqual(laneKeyAction('a', 2, 5), { kind: 'none' });
});

void test('an empty lane never produces an action', () => {
  assert.deepEqual(laneKeyAction('ArrowDown', 0, 0), { kind: 'none' });
  assert.deepEqual(laneKeyAction('Enter', 0, 0), { kind: 'none' });
});

void test('axisLabel names each Löwy axis', () => {
  assert.equal(axisLabel('sameCustomerOverTime'), 'Axis 1 — over time');
  assert.equal(axisLabel('allCustomersAtOneTime'), 'Axis 2 — across customers');
});

void test('selectionAnnouncement composes name + axis label', () => {
  assert.equal(
    selectionAnnouncement('Billing', 'allCustomersAtOneTime'),
    'Selected: Billing, Axis 2 — across customers'
  );
  assert.equal(
    selectionAnnouncement('Work Tracking', 'sameCustomerOverTime'),
    'Selected: Work Tracking, Axis 1 — over time'
  );
});

// ── rejectionClassLabel ──────────────────────────────────────────────────────

void test('rejectionClassLabel humanizes all four classes', () => {
  assert.equal(rejectionClassLabel('variableNotVolatile'), 'variable, not volatile');
  assert.equal(rejectionClassLabel('natureOfTheBusiness'), 'nature of the business');
  assert.equal(rejectionClassLabel('speculative'), 'speculative');
  assert.equal(rejectionClassLabel('foldedInto'), 'folded into another');
});

// ── axesLayout ───────────────────────────────────────────────────────────────

void test('axesLayout puts every dot exactly ON its own axis line', () => {
  const l = axesLayout(3, 4);
  for (const d of l.yDots) assert.equal(d.x, l.origin.x); // vertical axis
  for (const d of l.xDots) assert.equal(d.y, l.origin.y); // horizontal axis
  assert.equal(l.yDots.length, 3);
  assert.equal(l.xDots.length, 4);
});

void test('axesLayout spaces dots evenly from the origin (no fabricated positions)', () => {
  const l = axesLayout(4, 3);
  const yGaps = l.yDots.map((d, k) => (k === 0 ? l.origin.y - d.y : (l.yDots[k - 1]?.y ?? 0) - d.y));
  assert.equal(new Set(yGaps).size, 1); // identical pitch, including origin→first
  const xGaps = l.xDots.map((d, k) => (k === 0 ? d.x - l.origin.x : d.x - (l.xDots[k - 1]?.x ?? 0)));
  assert.equal(new Set(xGaps).size, 1);
});

void test('axesLayout keeps all dots strictly between origin and arrow tip', () => {
  const l = axesLayout(5, 5);
  for (const d of l.yDots) {
    assert.ok(d.y < l.origin.y, 'y dot above the origin');
    assert.ok(d.y > l.yArrowTip.y, 'y dot below the arrow tip');
  }
  for (const d of l.xDots) {
    assert.ok(d.x > l.origin.x, 'x dot right of the origin');
    assert.ok(d.x < l.xArrowTip.x, 'x dot short of the arrow tip');
  }
});

void test('axesLayout arrows leave the shared origin along the two axes', () => {
  const l = axesLayout(2, 2);
  assert.equal(l.yArrowTip.x, l.origin.x); // straight up
  assert.ok(l.yArrowTip.y < l.origin.y);
  assert.equal(l.xArrowTip.y, l.origin.y); // straight right
  assert.ok(l.xArrowTip.x > l.origin.x);
});

void test('axesLayout viewport grows with the dot counts (never crowds the pitch)', () => {
  const small = axesLayout(1, 1);
  const tall = axesLayout(8, 1);
  const wide = axesLayout(1, 8);
  assert.ok(tall.height > small.height);
  assert.equal(tall.width, small.width);
  assert.ok(wide.width > small.width);
  assert.equal(wide.height, small.height);
});

void test('axesLayout draws a visible axis even with zero dots on it', () => {
  const l = axesLayout(0, 0);
  assert.deepEqual(l.yDots, []);
  assert.deepEqual(l.xDots, []);
  assert.ok(l.origin.y - l.yArrowTip.y > 0, 'vertical axis has length');
  assert.ok(l.xArrowTip.x - l.origin.x > 0, 'horizontal axis has length');
});

void test('axesLayout fits every dot and tip inside the viewport', () => {
  const l = axesLayout(7, 7);
  const all = [...l.yDots, ...l.xDots, l.origin, l.yArrowTip, l.xArrowTip];
  for (const p of all) {
    assert.ok(p.x >= 0 && p.x <= l.width);
    assert.ok(p.y >= 0 && p.y <= l.height);
  }
});

// ── truncateLabel ────────────────────────────────────────────────────────────

void test('truncateLabel passes short names through and caps long ones with an ellipsis', () => {
  assert.equal(truncateLabel('Billing'), 'Billing');
  assert.equal(truncateLabel('exactly-eighteen!!'), 'exactly-eighteen!!'); // 18 chars, at the cap
  const long = truncateLabel('Notification Delivery Channels');
  assert.ok(long.endsWith('…'));
  assert.ok(long.length <= 18);
  assert.equal(truncateLabel('Notification Deli x', 16), 'Notification De…'); // trims the trailing space
});
