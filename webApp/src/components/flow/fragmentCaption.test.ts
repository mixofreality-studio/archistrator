/// <reference types="node" />
/**
 * Unit tests for fragmentCaption: the caption for a fragment-mode step that
 * authors no calls (founder QA round 3's split, re-cut in round 4 around the
 * visited trail, and collapsed into ONE function in fix round 1 so the heading
 * and its gloss can never disagree), plus the chain-wide position label.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  fragmentCallLessCaption,
  fragmentPositionLabel,
  fragmentRowLabel,
  ccChecksChipLabel,
} from './fragmentCaption.ts';

const TRAIL_BODY =
  'Nothing new is called here — what stays lit is the chain you have already walked.';

const CONTROL_FLOW_KINDS = ['merge', 'decision', 'fork', 'join', 'start', 'end', 'switch'] as const;
const CALL_ELIGIBLE_KINDS = ['action', 'timeEvent', 'acceptEvent'] as const;

// --- the defect signal is never softened (fix round 1, FINDING 1) -----------

void test('a real realization gap NEVER carries the reassuring trail body', () => {
  // The regression this pins: mid-chain (trail present) an unrealized step used
  // to print the defect heading with "what stays lit is the chain you have
  // already walked" beneath it, diluting the very signal the lens exists for.
  for (const kind of CALL_ELIGIBLE_KINDS) {
    for (const hasTrail of [true, false]) {
      const c = fragmentCallLessCaption('n1', kind, hasTrail, undefined);
      assert.equal(c.heading, 'No realization for this step');
      assert.equal(c.body, undefined, `gap with hasTrail=${String(hasTrail)} must stand alone`);
    }
  }
});

void test('an unknown kind degrades to the gap caption — and stays bodyless too', () => {
  assert.deepEqual(fragmentCallLessCaption('n1', undefined, true, undefined), {
    heading: 'No realization for this step',
    body: undefined,
  });
  assert.deepEqual(fragmentCallLessCaption('n1', undefined, false, undefined), {
    heading: 'No realization for this step',
    body: undefined,
  });
});

void test('the trail body appears ONLY under the control-flow-with-trail heading', () => {
  // Enumerate every non-decider call-less state and assert the body is emitted
  // in exactly one of them — the guard against the two lines drifting apart.
  const states: { c: ReturnType<typeof fragmentCallLessCaption>; name: string }[] = [];
  for (const hasTrail of [true, false]) {
    states.push({ c: fragmentCallLessCaption('', 'merge', hasTrail, undefined), name: 'chooser' });
    for (const kind of CALL_ELIGIBLE_KINDS) {
      states.push({ c: fragmentCallLessCaption('n1', kind, hasTrail, undefined), name: 'gap' });
    }
    for (const kind of CONTROL_FLOW_KINDS) {
      states.push({
        c: fragmentCallLessCaption('n1', kind, hasTrail, undefined),
        name: `controlFlow/${String(hasTrail)}`,
      });
    }
  }
  const withBody = states.filter((s) => s.c.body !== undefined);
  assert.ok(withBody.length > 0, 'the body must still be reachable');
  for (const s of withBody) {
    assert.equal(s.name, 'controlFlow/true');
    assert.equal(s.c.heading, 'Control flow — no calls; the chain so far stays lit');
    assert.equal(s.c.body, TRAIL_BODY);
  }
});

// --- the trail-state split (founder QA round 4) -----------------------------

void test('with no trail yet, a by-design control-flow step says the chain has not started', () => {
  for (const kind of CONTROL_FLOW_KINDS) {
    assert.deepEqual(fragmentCallLessCaption('n1', kind, false, undefined), {
      heading: 'No calls yet — step forward to begin the chain.',
      body: undefined,
    });
  }
});

void test('once a trail exists, a control-flow step says the walked chain stays lit', () => {
  for (const kind of CONTROL_FLOW_KINDS) {
    assert.deepEqual(fragmentCallLessCaption('n1', kind, true, undefined), {
      heading: 'Control flow — no calls; the chain so far stays lit',
      body: TRAIL_BODY,
    });
  }
});

// --- the entry chooser keeps its own affordance (fix round 1, OPTIONAL) -----

void test('the multi-root entry chooser asks you to CHOOSE, not to step forward', () => {
  // "step forward" is the wrong affordance where the reader picks among entries.
  assert.deepEqual(fragmentCallLessCaption('', undefined, false, undefined), {
    heading: 'Choose an entry to begin.',
    body: undefined,
  });
  // The blank id keys no node, so no kind can make it read as a realization gap.
  for (const kind of [...CALL_ELIGIBLE_KINDS, ...CONTROL_FLOW_KINDS]) {
    assert.equal(
      fragmentCallLessCaption('', kind, false, undefined).heading,
      'Choose an entry to begin.'
    );
  }
});

// --- the decider outranks the rest (founder QA round 3 addendum) ------------

void test('a resolved decider names itself, and takes the trail gloss with a trail', () => {
  assert.deepEqual(fragmentCallLessCaption('n1', 'decision', true, 'SystemDesignManager'), {
    heading: 'Decided by SystemDesignManager',
    body: TRAIL_BODY,
  });
  assert.deepEqual(fragmentCallLessCaption('n1', 'switch', false, 'Architect User'), {
    heading: 'Decided by Architect User',
    body: undefined,
  });
});

// --- the chain-wide position label ------------------------------------------

void test('a single-call fragment reports one position; a multi-call fragment a range', () => {
  assert.equal(fragmentPositionLabel([5], 22), 'call 5 of 22');
  assert.equal(fragmentPositionLabel([5, 6, 7], 22), 'call 5–7 of 22');
});

void test('the range is the fragment min/max whatever order the seqs arrive in', () => {
  assert.equal(fragmentPositionLabel([9, 6, 7], 22), 'call 6–9 of 22');
});

void test('nothing lit, or nothing to be lit, yields no position label', () => {
  assert.equal(fragmentPositionLabel([], 22), undefined);
  assert.equal(fragmentPositionLabel([1], 0), undefined);
});

// --- the per-row global/local scale bridge (fix round 1, FINDING 2) ---------

void test('a plain call (no altLabel) shows only its seq — unaffected, no parenthetical noise', () => {
  assert.equal(fragmentRowLabel({ seq: 22 }), '22');
});

void test('an alt-labeled call shows its altLabel PLUS the true global seq in parentheses', () => {
  // The bridge to fragmentPositionLabel's heading ("call 18–22 of 22"): a
  // founder reading "1a (call 18)" beneath that heading can see these are the
  // SAME calls, not a contradiction.
  assert.equal(fragmentRowLabel({ seq: 18, altLabel: '1a' }), '1a (call 18)');
});

void test('a plain call WITHIN an alt-containing step (its own altLabel has no letter) still gets the parenthetical', () => {
  assert.equal(fragmentRowLabel({ seq: 22, altLabel: '3' }), '3 (call 22)');
});

// --- the FragmentBar CC-checks chip label (Task 6, call-chain rollout) ------

void test('a clean fragment reads "CC checks · passing", ignoring findingsCount', () => {
  assert.equal(ccChecksChipLabel('green', 0), 'CC checks · passing');
  assert.equal(ccChecksChipLabel('green', 4), 'CC checks · passing');
});

void test('a flagged fragment names the count, pluralized', () => {
  assert.equal(ccChecksChipLabel('red', 1), 'CC checks · 1 finding');
  assert.equal(ccChecksChipLabel('red', 2), 'CC checks · 2 findings');
});

void test('a flagged fragment with a somehow-absent count reads 0 findings rather than throwing', () => {
  assert.equal(ccChecksChipLabel('red', -1), 'CC checks · 0 findings');
});
