/**
 * The hover card's provenance marks (hoverCard.ts) — designer P0-1: the one
 * legible surface at fit zoom never lets a reconstructed "passed" read as fact.
 */
/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import type { ConstructionRow, RecordOriginRow, TaskAttemptRow } from '../../../contracts/types.ts';
import { buildActivityTree, type ActivityNode } from '../list/activityTree.ts';
import { hoverCardStampFor, hoverLaneMarksFor } from './hoverCard.ts';

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
  const m = hoverLaneMarksFor(backfilled);
  assert.equal(m.stamp, true);
  assert.equal(m.chip?.state, 'passed');
  assert.equal(hoverLaneMarksFor(synthesized).stamp, true);
});

void test('an UNKNOWN lane stays unmarked', () => {
  assert.deepEqual(hoverLaneMarksFor(unknown), { stamp: false });
});

void test('an OBSERVED lane needs no stamp', () => {
  assert.equal(hoverLaneMarksFor(observed).stamp, false);
});

void test('the header stamp: any reconstructed lane on the card, never an unknown-only card', () => {
  assert.equal(hoverCardStampFor([observed, backfilled]), true);
  assert.equal(hoverCardStampFor([unknown]), false);
  assert.equal(hoverCardStampFor([observed, unknown]), false);
  assert.equal(hoverCardStampFor([]), false);
});
