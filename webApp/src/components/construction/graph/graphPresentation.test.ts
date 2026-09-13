/**
 * The GRAPH lens's presentation rules (graphPresentation.ts) — which token
 * paints each spine state, what the ribbon's count says, and how the layering
 * check reads. Pure, so pinned here; the .tsx renderers only apply them.
 *
 * The token bag is a Proxy that answers every key with the key's own NAME, so
 * a test asserts WHICH token a state uses — never a hex value that one of the
 * five themes happens to share.
 */
/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import type { Tokens } from '../../../utilities/theme/themes';
import {
  SEGMENT_STATES,
  SEGMENT_STATE_LABEL,
  UTILITY_HOVER_TEXT,
  hollowKeyText,
  layeringCheckText,
  segmentCodeFor,
  ribbonCountLabel,
  segmentPaint,
  tickPaint,
} from './graphPresentation.ts';

const t = new Proxy(
  {},
  {
    get: (_target, key): string => String(key),
  }
) as Tokens;

// ---------------------------------------------------------------------------
// Spine paint (spec §7.2)
// ---------------------------------------------------------------------------

void test('every segment state has a paint', () => {
  for (const s of SEGMENT_STATES) assert.ok(segmentPaint(t, s).border.length > 0, s);
});

void test('complete is a solid committed fill — the olive of a passed gate', () => {
  const p = segmentPaint(t, 'complete');
  assert.equal(p.fill, 'committedDot');
  assert.equal(p.borderStyle, 'solid');
});

void test('failed is dangerFg on awaitingBg — "needs you", never "dead"', () => {
  const p = segmentPaint(t, 'failed');
  assert.equal(p.fill, 'awaitingBg');
  assert.equal(p.border, 'dangerFg');
});

void test('awaitingHuman is awaitingBg with an accent edge — the loudest mark', () => {
  const p = segmentPaint(t, 'awaitingHuman');
  assert.equal(p.fill, 'awaitingBg');
  assert.equal(p.accentEdge, 'accent');
});

void test('running is the ONLY animated state', () => {
  const animated = SEGMENT_STATES.filter((s) => segmentPaint(t, s).animated);
  assert.deepEqual(animated, ['running']);
});

void test('unknown is a dashed hairline with no fill; notStarted a solid hairline, hollow', () => {
  const unknown = segmentPaint(t, 'unknown');
  assert.equal(unknown.fill, 'transparent');
  assert.equal(unknown.borderStyle, 'dashed');
  const notStarted = segmentPaint(t, 'notStarted');
  assert.equal(notStarted.fill, 'transparent');
  assert.equal(notStarted.borderStyle, 'solid');
});

void test('absent is a gap at 40% opacity with no box of its own', () => {
  const p = segmentPaint(t, 'absent');
  assert.equal(p.opacity, 0.4);
  assert.equal(p.borderStyle, 'none');
  assert.equal(p.fill, 'transparent');
});

void test('only the states that assert something happened are filled', () => {
  const filled = SEGMENT_STATES.filter((s) => segmentPaint(t, s).fill !== 'transparent');
  assert.deepEqual(filled.sort(), ['awaitingHuman', 'complete', 'failed', 'running']);
});

// ---------------------------------------------------------------------------
// The ribbon's count (§9.2)
// ---------------------------------------------------------------------------

void test('the count reads k/n only when the ribbon model carries one', () => {
  assert.equal(ribbonCountLabel({ feeders: ['a', 'b', 'c'], gates: [], complete: 2 }), '2/3');
});

void test('§9.2: with no trusted count the label is an em dash, never a number', () => {
  assert.equal(ribbonCountLabel({ feeders: ['a', 'b'], gates: [] }), '—');
});

void test('a milestone with no feeders says what it gates instead', () => {
  assert.equal(ribbonCountLabel({ feeders: [], gates: ['x', 'y', 'z'] }), 'gates 3');
});

// ---------------------------------------------------------------------------
// The layering check (R5, App C §3.4)
// ---------------------------------------------------------------------------

void test('the layering check states both alarm counts, zero included', () => {
  assert.equal(
    layeringCheckText({ alarms: { up: 0, sideways: 0 }, sanctionedSideways: 0 }),
    'Up/sideways check: 0 upward · 0 sideways'
  );
});

void test('the sanctioned queued Manager→Manager calls are counted apart, citing App C', () => {
  assert.equal(
    layeringCheckText({ alarms: { up: 1, sideways: 0 }, sanctionedSideways: 2 }),
    'Up/sideways check: 1 upward · 0 sideways · 2 queued Manager→Manager (sanctioned, App C §3.4)'
  );
});

// ---------------------------------------------------------------------------
// Coverage words (designer P1-5 / Q3)
// ---------------------------------------------------------------------------

void test('P1-5: a utility reads as shared infrastructure, never as a gap', () => {
  assert.equal(
    UTILITY_HOVER_TEXT,
    'Utility — shared infrastructure. The Method plans no activity for a utility.'
  );
});

void test('P1-5: the key counts only layered components with no activity', () => {
  assert.equal(hollowKeyText(6), '6 components with no activity (dashed)');
  assert.equal(hollowKeyText(1), '1 component with no activity (dashed)');
});

// ---------------------------------------------------------------------------
// Segment codes (designer P1-3)
// ---------------------------------------------------------------------------

void test('the five canonical phases read REQ / DD / TP / CON / INT', () => {
  assert.deepEqual(
    [
      ['requirements', 'Requirements'],
      ['detailed_design', 'Detailed Design'],
      ['test_plan', 'Test Plan'],
      ['construction', 'Construction'],
      ['integration', 'Integration'],
    ].map(([phase, name]) => segmentCodeFor({ phase: phase ?? '', name: name ?? '' })),
    ['REQ', 'DD', 'TP', 'CON', 'INT']
  );
});

void test("a renamed phase is not the canonical one — it takes its label's initials", () => {
  assert.equal(segmentCodeFor({ phase: 'requirements', name: 'UX Requirements' }), 'UR');
  assert.equal(segmentCodeFor({ phase: 'requirements', name: 'Use-Case Trace' }), 'UCT');
  assert.equal(segmentCodeFor({ phase: 'test_plan', name: 'Flows' }), 'F');
  assert.equal(segmentCodeFor({ phase: 'integration', name: 'Regression & Sign-off' }), 'RSO');
  assert.equal(segmentCodeFor({ phase: 'detailed_design', name: 'Perf Scenario Design' }), 'PSD');
});

void test('initials stop at three letters, and an absent gap has no code', () => {
  assert.equal(segmentCodeFor({ phase: 'construction', name: 'A Very Long Phase Name' }), 'AVL');
  assert.equal(segmentCodeFor({ phase: 'test_plan' }), undefined);
});

// ---------------------------------------------------------------------------
// Task ticks and state words
// ---------------------------------------------------------------------------

void test('a tick that happened is filled in its state token; one that did not is hollow', () => {
  assert.deepEqual(tickPaint(t, 'passed'), { color: 'committedDot', hollow: false, dashed: false });
  assert.deepEqual(tickPaint(t, 'failed'), { color: 'dangerFg', hollow: false, dashed: false });
  assert.equal(tickPaint(t, 'notStarted').hollow, true);
  assert.deepEqual(tickPaint(t, 'unknown'), { color: 'line', hollow: true, dashed: true });
});

void test('every segment state has words, and absent says it is by design', () => {
  for (const s of SEGMENT_STATES) assert.ok(SEGMENT_STATE_LABEL[s].length > 0, s);
  assert.equal(SEGMENT_STATE_LABEL.absent, 'Not in this profile');
  assert.equal(SEGMENT_STATE_LABEL.unknown, 'Unknown');
});
