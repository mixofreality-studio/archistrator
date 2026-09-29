/// <reference types="node" />
/**
 * `m0CostBasisNotice` — the M0 gate's defaulted-assumptions line, re-pointed at the
 * attempt `Detail` (stage 4b2 Task 9 Step 5). Approving M0 binds the plan of record
 * and starts spending, so a cost computed on assumed numbers must say so IN THE
 * SERVER'S OWN WORDS; and it must say NOTHING when nothing was assumed, because an
 * absent detail means the compute defaulted nothing, not that the wire broke.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { m0CostBasisNotice } from './m0CostBasis.ts';

/** `defaultedDetail`'s sentence, verbatim (deliverymanager.go). */
const DEFAULTED =
  "the plan's cost was computed on ASSUMED values for the settlement terms, the declared usage — " +
  "the founder authored none, so these are the platform's documented defaults and not decisions";

void test('the defaulted revision renders the server’s own sentence, verbatim', () => {
  // Verbatim, not re-worded: the client must not hold a second copy of the rule that
  // decided WHICH families were defaulted.
  assert.equal(m0CostBasisNotice({ outcome: 'awaitingHuman', detail: DEFAULTED }), DEFAULTED);
});

void test('a revision with no detail renders NOTHING — "nothing was assumed", not a missing value', () => {
  // THE LIVE CASE ON THIS REPO. Removing revenue share made slot 8 fully authored, so
  // the compute defaults nothing and `defaultedDetail` returns ''; the wire then omits
  // the field. That is the fix working. It must not render an empty bullet, an
  // ellipsis, or a reassurance nobody reads.
  assert.equal(m0CostBasisNotice({ outcome: 'awaitingHuman' }), '');
  assert.equal(m0CostBasisNotice({ outcome: 'awaitingHuman', detail: null }), '');
  assert.equal(m0CostBasisNotice({ outcome: 'awaitingHuman', detail: '' }), '');
});

void test('no revision at all answers nothing rather than throwing', () => {
  assert.equal(m0CostBasisNotice(undefined), '');
});

void test('a FAILED revision’s detail is its error, and is never filed as a cost basis', () => {
  // `sdpComputeStrategy` records `err.Error()` as the detail of a failed attempt. The
  // lifecycle already says the compute failed; repeating that text under "what your
  // cost was computed on" would misfile a failure as a pricing assumption.
  assert.equal(
    m0CostBasisNotice({ outcome: 'failed', detail: 'estimation engine refused: no rate card' }),
    ''
  );
});
