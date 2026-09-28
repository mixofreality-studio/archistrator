/// <reference types="node" />
/**
 * `m0CostBasisNotice` — the M0 gate's defaulted-assumptions line (stage 4b1 Task 14
 * Step 3a). Approving M0 binds the plan of record and starts spending, so a cost
 * computed on assumed numbers must say so; and it must say NOTHING when nothing was
 * assumed, because a notice that always shows is a notice nobody reads.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { m0CostBasisNotice, PLATFORM_DEFAULTS_NOTE_PREFIX } from './m0CostBasis.ts';
import type { ArtifactSlotView } from '../../contracts/types.ts';

function planningAssumptions(notes: string): ArtifactSlotView {
  return {
    kind: 'planningAssumptions',
    stage: 4,
    model: {
      kind: 'planningAssumptions',
      model: {
        calendarDaysPerWeek: 5,
        declaredUsage: { expectedDailyActiveUsers: 1, requestsPerMinute: 1, avgPayloadBytes: 4096 },
        indirectDailyRate: { minorUnits: 0, currency: 'USD' },
        infrastructureKind: 1,
        notes,
        resources: ['senior-developer'],
        terms: {
          computeCost: 1,
          computeMarkupPercent: 0,
          revenueShare: 2,
          revenueSharePercent: 0,
          schedule: 1,
        },
      },
    },
  } as unknown as ArtifactSlotView;
}

const sdpReview = { kind: 'sdpReview', stage: 4, model: { kind: 'sdpReview' } } as ArtifactSlotView;

void test('no planning-assumptions slot at all: the cost rode on every family, and the line says which', () => {
  assert.equal(
    m0CostBasisNotice([sdpReview]),
    'Cost computed on assumed every planning assumption — no planning assumptions are committed for this project yet'
  );
});

void test('a slot the platform wrote itself is still assumed — the founder authored none of it', () => {
  const notice = m0CostBasisNotice([
    sdpReview,
    planningAssumptions(
      `${PLATFORM_DEFAULTS_NOTE_PREFIX} no planning assumptions were authored, so the platform assumed …`
    ),
  ]);
  assert.match(notice, /^Cost computed on assumed every planning assumption/);
  assert.match(
    notice,
    /the platform's own defaults, not this project's/,
    'the committed case is a different fact from the absent one, and a different exit'
  );
});

void test('an AUTHORED slot says nothing — ABSENT, not a reassurance nobody reads', () => {
  assert.equal(
    m0CostBasisNotice([
      sdpReview,
      planningAssumptions('Two developers, four days a week, our own negotiated rate card.'),
    ]),
    ''
  );
  // Empty notes are not the platform's signature either: nothing claims a default.
  assert.equal(m0CostBasisNotice([planningAssumptions('')]), '');
});

void test('a slot whose envelope does not hold the model answers nothing rather than throwing', () => {
  const broken = {
    kind: 'planningAssumptions',
    stage: 4,
    model: { kind: 'network' },
  } as ArtifactSlotView;
  assert.equal(m0CostBasisNotice([broken]), '');
});
