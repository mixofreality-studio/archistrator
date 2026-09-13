/**
 * The hover card's provenance marks (hoverCard.ts) — designer P0-1: the one
 * legible surface at fit zoom never lets a reconstructed "passed" read as fact.
 */
/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import type { ConstructionRow, RecordOriginRow, TaskAttemptRow } from '../../../contracts/types.ts';
import { buildActivityTree, type ActivityNode } from '../list/activityTree.ts';
import { hoverCardStampFor, hoverLaneMarksFor, laneChipFor } from './hoverCard.ts';

function attempt(origin: RecordOriginRow): TaskAttemptRow {
  return {
    attemptId: 'a1',
    task: 'srsReview',
    phase: 'requirements',
    attempt: 1,
    outcome: 'passed',
    evidence: { kind: '', ref: '' },
    provenance: {
      origin,
      ...(origin === 'observed' ? {} : { basis: 'founderRuling[2026-09-09]' }),
    },
  };
}

function node(activityId: string, origin: RecordOriginRow | undefined): ActivityNode {
  const row: ConstructionRow = {
    activityId,
    classified: true,
    hasBuildEvidence: origin !== undefined,
    recorded: origin !== undefined,
    kind: 'service',
    phases: [],
    attempts: origin !== undefined ? [attempt(origin)] : [],
    ...(origin !== undefined ? { status: 'integrated' as const, worstOrigin: origin } : {}),
  };
  const [n] = buildActivityTree([row]);
  assert.ok(n !== undefined);
  return n;
}

const backfilled = node('C-backfilled', 'backfilled');
const synthesized = node('C-synthesized', 'synthesized');
const observed = node('C-observed', 'observed');
const unknown = node('C-unknown', undefined);

void test('a RECONSTRUCTED lane line carries its stamp AND its state chip', () => {
  const m = hoverLaneMarksFor(backfilled, undefined);
  assert.equal(m.stamp, true);
  assert.equal(m.chip?.state, 'passed');
  assert.equal(hoverLaneMarksFor(synthesized, undefined).stamp, true);
});

void test('an UNKNOWN lane stays unmarked', () => {
  assert.deepEqual(hoverLaneMarksFor(unknown, undefined), { stamp: false });
});

void test('an OBSERVED lane needs no stamp', () => {
  assert.equal(hoverLaneMarksFor(observed, undefined).stamp, false);
});

void test('the header stamp: any reconstructed lane on the card, never an unknown-only card', () => {
  assert.equal(hoverCardStampFor([observed, backfilled]), true);
  assert.equal(hoverCardStampFor([unknown]), false);
  assert.equal(hoverCardStampFor([observed, unknown]), false);
  assert.equal(hoverCardStampFor([]), false);
});

// Integration merge: the lane's chip is the list row's (owed set only, owed words).
void test('a lane chip reads the owed mark, never head-state in-review', () => {
  const inReview = { ...backfilled.row, status: 'in-review' as const };
  assert.notEqual(laneChipFor(inReview, undefined)?.state, 'awaitingHuman');
  assert.deepEqual(laneChipFor(inReview, { reason: 'gate' }), {
    label: 'Awaiting you',
    size: 'sm',
    state: 'awaitingHuman',
  });
});

void test('an owed chip carries the owed word: a takeover is "Steer needed", a failure "Failed"', () => {
  assert.equal(laneChipFor(backfilled.row, { reason: 'takeover' })?.label, 'Steer needed');
  assert.equal(laneChipFor(backfilled.row, { reason: 'takeover' })?.state, 'awaitingHuman');
  assert.equal(laneChipFor(backfilled.row, { reason: 'failed' })?.state, 'failed');
});

void test('a reconstructed lane line in the hover card carries the owed chip too', () => {
  assert.equal(hoverLaneMarksFor(backfilled, { reason: 'takeover' }).chip?.label, 'Steer needed');
});
