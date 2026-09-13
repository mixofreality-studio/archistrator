/**
 * M0 (m0Gate.ts) — the PM's Q4 ruling: state from `project.phase`, staleness
 * only from the SDP review slot, the binding chip and hover copy, and nothing
 * the ruling leaves off (dates, options, costs).
 */
/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import {
  M0_OPEN_SDP_REVIEW,
  m0FactsFor,
  m0PresentationFor,
  m0StateOf,
  staleCauseText,
  type M0SlotLike,
} from './m0Gate.ts';

/** Today's live shape: slots 11–16 stale from the activity list's revision 3. */
const STALE_SLOTS: M0SlotLike[] = [
  { kind: 'mission' },
  { kind: 'operationalConcepts', staleCauseKind: 'system', staleCauseRevision: 2 },
  { kind: 'planningAssumptions', staleCauseKind: 'system', staleCauseRevision: 2 },
  { kind: 'activityList' },
  { kind: 'network' },
  ...[
    'normalSolution',
    'subcriticalSolution',
    'compressedSolution',
    'decompressedSolution',
    'riskModel',
    'sdpReview',
  ].map(
    (kind): M0SlotLike => ({
      kind,
      staleBasis: true,
      staleCauseKind: 'activityList',
      staleCauseRevision: 3,
    })
  ),
];

// ---------------------------------------------------------------------------
// State — project.phase, the pump's own gate
// ---------------------------------------------------------------------------

void test('construction is passed; an earlier phase is not passed; unreadable is unknown', () => {
  assert.equal(m0StateOf('construction'), 'passed');
  assert.equal(m0StateOf('projectDesign'), 'notPassed');
  assert.equal(m0StateOf('systemDesign'), 'notPassed');
  assert.equal(m0StateOf('unknown'), 'unknown');
  assert.equal(m0StateOf(undefined), 'unknown');
});

void test('no project read at all reads "—", never a guess', () => {
  const p = m0PresentationFor(m0FactsFor(undefined), 18);
  assert.deepEqual(p.chipParts, ['M0', 'SDP review', '—', 'gates 18']);
  assert.equal(p.title, 'M0 — state not available');
  assert.equal(
    p.body,
    "The project's phase could not be read, so no state is shown rather than a guess."
  );
});

void test('the state is the PHASE, never the SDP review contents', () => {
  // A stale, even absent, SDP slot does not make the gate "not passed".
  assert.equal(m0FactsFor({ phase: 'construction', slots: [] }).state, 'passed');
  assert.equal(m0FactsFor({ phase: 'projectDesign', slots: STALE_SLOTS }).state, 'notPassed');
});

// ---------------------------------------------------------------------------
// Staleness — only slots.16, a separate flag
// ---------------------------------------------------------------------------

void test('stale comes from the SDP review slot ONLY', () => {
  const others = STALE_SLOTS.map((s) => (s.kind === 'sdpReview' ? { kind: 'sdpReview' } : s));
  assert.equal(m0FactsFor({ phase: 'construction', slots: others }).stale, false);
  assert.equal(m0FactsFor({ phase: 'construction', slots: STALE_SLOTS }).stale, true);
});

void test('the cause names the upstream slot and its revision', () => {
  assert.equal(staleCauseText('activityList', 3), 'the activity list (revision 3)');
  assert.equal(staleCauseText('network', undefined), 'the project network');
  assert.equal(
    m0FactsFor({ phase: 'construction', slots: STALE_SLOTS }).cause,
    'the activity list (revision 3)'
  );
});

void test('k counts the stale PROJECT DESIGN artifacts, the SDP review included', () => {
  const withPhase1Stale: M0SlotLike[] = [...STALE_SLOTS, { kind: 'system', staleBasis: true }];
  assert.equal(m0FactsFor({ phase: 'construction', slots: withPhase1Stale }).staleCount, 6);
});

// ---------------------------------------------------------------------------
// The binding copy
// ---------------------------------------------------------------------------

void test('passed', () => {
  const p = m0PresentationFor(m0FactsFor({ phase: 'construction', slots: [] }), 18);
  assert.deepEqual(p.chipParts, ['M0', 'SDP review', 'passed', 'gates 18']);
  assert.equal(p.title, 'M0 — SDP review passed');
  assert.equal(
    p.body,
    'Phase 2 is sealed, so construction is authorized. 18 activities start here.'
  );
  assert.equal(p.link, undefined);
});

void test('passed, stale — the amber flag, the cause, k, and the way back', () => {
  const p = m0PresentationFor(m0FactsFor({ phase: 'construction', slots: STALE_SLOTS }), 18);
  assert.deepEqual(p.chipParts, ['M0', 'SDP review', 'passed', 'basis changed', 'gates 18']);
  assert.equal(p.stale, true);
  assert.equal(p.title, 'M0 — SDP review passed; the plan has changed since');
  assert.equal(
    p.body,
    'Phase 2 is sealed, so construction is authorized. Since then, the activity list (revision 3) was amended, and 6 Project Design artifacts, including the SDP review, are marked stale. Their durations, costs and risk no longer describe the plan being built. This does not block construction. To bring the approval back in line, reconcile the SDP review by amendment, or mark it reviewed — unaffected.'
  );
  assert.equal(p.link, M0_OPEN_SDP_REVIEW);
  assert.equal(M0_OPEN_SDP_REVIEW, 'Open the SDP review →');
});

void test('not passed — and a stale slot does not decorate a gate never passed', () => {
  const p = m0PresentationFor(m0FactsFor({ phase: 'projectDesign', slots: STALE_SLOTS }), 18);
  assert.deepEqual(p.chipParts, ['M0', 'SDP review', 'not passed', 'gates 18']);
  assert.equal(p.title, 'M0 — SDP review not passed');
  assert.equal(
    p.body,
    'Construction cannot start until Phase 2 is sealed at the SDP review. 18 activities wait on it.'
  );
  assert.equal(p.stale, false);
});

void test('LEFT OFF: no date, no option, no cost or duration in any variant', () => {
  const variants = [
    m0PresentationFor(m0FactsFor({ phase: 'construction', slots: [] }), 18),
    m0PresentationFor(m0FactsFor({ phase: 'construction', slots: STALE_SLOTS }), 18),
    m0PresentationFor(m0FactsFor({ phase: 'projectDesign', slots: [] }), 18),
    m0PresentationFor(m0FactsFor(undefined), 18),
  ];
  for (const p of variants) {
    const text = [...p.chipParts, p.title].join(' ');
    assert.doesNotMatch(text, /\d{4}-\d{2}-\d{2}|\$|week|normal|compressed|option/i);
  }
});
