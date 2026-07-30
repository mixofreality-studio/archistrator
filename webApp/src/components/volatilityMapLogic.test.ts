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
 *     with the counts UNTIL the height cap (then the vertical pitch scales down
 *     to fit — the compact-quadrant rule), a minimum width for the axis labels,
 *     dotless axes still drawing a visible axis;
 *   • the rejected-candidate classification labels;
 *   • the volatility → component "encapsulated by" join (encapsulationOwners) —
 *     the typed encapsulatesVolatilities field wins by EXACT name whenever any
 *     component carries it (multiple owners preserved, component order), the
 *     name-normalized prose-substring fallback only serves older committed
 *     states where no component carries the field;
 *   • the compact per-item axis indicator label (axisShortLabel).
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  AXES_HEIGHT_CAP,
  AXES_MIN_WIDTH,
  axesLayout,
  axisLabel,
  axisShortLabel,
  encapsulationOwners,
  laneKeyAction,
  normalizeName,
  rejectionClassLabel,
  selectionAnnouncement,
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
  const yGaps = l.yDots.map((d, k) =>
    k === 0 ? l.origin.y - d.y : (l.yDots[k - 1]?.y ?? 0) - d.y
  );
  assert.equal(new Set(yGaps).size, 1); // identical pitch, including origin→first
  const xGaps = l.xDots.map((d, k) =>
    k === 0 ? d.x - l.origin.x : d.x - (l.xDots[k - 1]?.x ?? 0)
  );
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

void test('axesLayout viewport grows with the dot counts until the height cap', () => {
  const small = axesLayout(1, 1);
  const tall = axesLayout(6, 1);
  const wide = axesLayout(1, 9); // past the minimum-width floor
  assert.ok(tall.height > small.height);
  assert.ok(tall.height <= AXES_HEIGHT_CAP);
  assert.equal(tall.width, small.width);
  assert.ok(wide.width > small.width);
  assert.equal(wide.height, small.height);
});

void test('axesLayout caps the height and scales the vertical pitch to fit', () => {
  const base = axesLayout(2, 2);
  const big = axesLayout(40, 2);
  assert.ok(big.height <= AXES_HEIGHT_CAP, 'height never exceeds the cap');
  assert.equal(big.yDots.length, 40);
  // Every dot still strictly between origin and arrow tip on the capped axis.
  for (const d of big.yDots) {
    assert.ok(d.y < big.origin.y && d.y > big.yArrowTip.y);
  }
  // The pitch SHRANK below the uncapped base pitch instead of the diagram towering…
  const basePitch = base.origin.y - (base.yDots[0]?.y ?? 0);
  const bigPitch = big.origin.y - (big.yDots[0]?.y ?? 0);
  assert.ok(bigPitch < basePitch);
  // …and the spacing stays even at the scaled (fractional) pitch.
  for (let k = 1; k < big.yDots.length; k++) {
    const gap = (big.yDots[k - 1]?.y ?? 0) - (big.yDots[k]?.y ?? 0);
    assert.ok(Math.abs(gap - bigPitch) < 1e-9);
  }
});

void test('axesLayout keeps a minimum width so the axis labels always fit', () => {
  assert.equal(axesLayout(1, 1).width, AXES_MIN_WIDTH);
  assert.equal(axesLayout(6, 1).width, AXES_MIN_WIDTH);
});

void test('axesLayout draws a visible axis even with zero dots on it', () => {
  const l = axesLayout(0, 0);
  assert.deepEqual(l.yDots, []);
  assert.deepEqual(l.xDots, []);
  assert.ok(l.origin.y - l.yArrowTip.y > 0, 'vertical axis has length');
  assert.ok(l.xArrowTip.x - l.origin.x > 0, 'horizontal axis has length');
});

void test('axesLayout pads keep the sketch off its frame corner (F4 rebalance)', () => {
  // origin.x is LEFT_PAD, yArrowTip.y is TOP_PAD, height − origin.y is
  // BOTTOM_PAD — pinned so the fit-content frame keeps its breathing room.
  const l = axesLayout(3, 3);
  assert.equal(l.origin.x, 28);
  assert.equal(l.yArrowTip.y, 24);
  assert.equal(l.height - l.origin.y, 32);
});

void test('axesLayout fits every dot and tip inside the viewport', () => {
  const l = axesLayout(7, 7);
  const all = [...l.yDots, ...l.xDots, l.origin, l.yArrowTip, l.xArrowTip];
  for (const p of all) {
    assert.ok(p.x >= 0 && p.x <= l.width);
    assert.ok(p.y >= 0 && p.y <= l.height);
  }
});

// ── encapsulationOwners — the volatility → component join ────────────────────

/** Shorthand component fixture for the join tests. */
function comp(
  name: string,
  encapsulates: string,
  encapsulatesVolatilities: string[] = []
): { name: string; encapsulates: string; encapsulatesVolatilities: string[] } {
  return { name, encapsulates, encapsulatesVolatilities };
}

void test('typed join: exact volatility names resolve their owning components', () => {
  const owners = encapsulationOwners(
    ['Payment Methods', 'Notification Channels'],
    [
      comp('BillingEngine', 'How we settle charges.', ['Payment Methods']),
      comp('NotifyAccess', 'Delivery of notifications.', ['Notification Channels']),
    ]
  );
  assert.deepEqual(owners.get('Payment Methods'), ['BillingEngine']);
  assert.deepEqual(owners.get('Notification Channels'), ['NotifyAccess']);
});

void test('typed join: multiple owners are preserved in component order', () => {
  const owners = encapsulationOwners(
    ['Payment Methods'],
    [
      comp('BillingEngine', '', ['Payment Methods']),
      comp('SettlementAccess', '', ['Payment Methods']),
    ]
  );
  assert.deepEqual(owners.get('Payment Methods'), ['BillingEngine', 'SettlementAccess']);
});

void test('typed join: exact match only — a typed name never substring-matches', () => {
  const owners = encapsulationOwners(['Payment Methods'], [comp('BillingEngine', '', ['Payment'])]);
  assert.equal(owners.has('Payment Methods'), false);
});

void test('any component carrying the typed field switches the whole join to typed mode', () => {
  // NotifyAccess would win "Notification Channels" by prose substring, but the
  // presence of ONE typed carrier means the state post-dates the field — the
  // fragile prose join is retired wholesale, not mixed per volatility.
  const owners = encapsulationOwners(
    ['Payment Methods', 'Notification Channels'],
    [
      comp('BillingEngine', '', ['Payment Methods']),
      comp('NotifyAccess', 'Notification Channels: which channel delivers.'),
    ]
  );
  assert.deepEqual(owners.get('Payment Methods'), ['BillingEngine']);
  assert.equal(owners.has('Notification Channels'), false);
});

void test('fallback join: prose substring match, punctuation/case tolerant', () => {
  const owners = encapsulationOwners(
    ['Payment Methods'],
    [comp('BillingEngine', 'payment-methods: card vs invoice vs crypto.')]
  );
  assert.deepEqual(owners.get('Payment Methods'), ['BillingEngine']);
});

void test('fallback join: multiple prose owners are preserved in component order', () => {
  const owners = encapsulationOwners(
    ['Payment Methods'],
    [
      comp('BillingEngine', 'Payment Methods: settle.'),
      comp('SettlementAccess', 'Also Payment Methods adjacent.'),
    ]
  );
  assert.deepEqual(owners.get('Payment Methods'), ['BillingEngine', 'SettlementAccess']);
});

void test('unowned volatilities are absent from the join result', () => {
  const owners = encapsulationOwners(
    ['Orphan Volatility'],
    [comp('BillingEngine', 'Payment Methods: settle.', ['Payment Methods'])]
  );
  assert.equal(owners.has('Orphan Volatility'), false);
  assert.deepEqual(encapsulationOwners([], []), new Map());
});

void test('typed join tolerates stray whitespace around the recorded name', () => {
  const owners = encapsulationOwners(
    ['Payment Methods'],
    [comp('BillingEngine', '', [' Payment Methods '])]
  );
  assert.deepEqual(owners.get('Payment Methods'), ['BillingEngine']);
});

void test('normalizeName strips punctuation/case to bare alphanumerics', () => {
  assert.equal(normalizeName('Payment-Methods!'), 'paymentmethods');
  assert.equal(normalizeName('  Notification  Channels '), 'notificationchannels');
});

// ── axisShortLabel — the compact per-item axis indicator ─────────────────────

void test('axisShortLabel names the Löwy axis compactly', () => {
  assert.equal(axisShortLabel('sameCustomerOverTime'), 'A1');
  assert.equal(axisShortLabel('allCustomersAtOneTime'), 'A2');
});
