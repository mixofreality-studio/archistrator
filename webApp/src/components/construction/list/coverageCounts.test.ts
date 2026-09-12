/**
 * coverageCounts.ts — pure, so tested directly with no renderer in the way.
 *
 * The brief's own acceptance test: feed a fixture with a DIFFERENT split than
 * the real project's 40/31/9 ‖ 69/9/60 and assert the strip follows — proving
 * the six numbers are computed, never hand-typed.
 */
/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import {
  computeCoverageCounts,
  coverageStripText,
  derivedNameSet,
  isOrphanedLegacyActivity,
  partitionLegacy,
  type DerivedActivityRef,
} from './coverageCounts.ts';

function derivedActivity(name: string, componentId?: string): DerivedActivityRef {
  return componentId !== undefined ? { name, componentId } : { name };
}

// ---------------------------------------------------------------------------
// The real project's shape, as a regression pin — NOT what proves the numbers
// are computed (the differently-shaped fixture below is), but worth pinning:
// this is the exact 40/31/9 ‖ 69/9/60 the brief predicts.
// ---------------------------------------------------------------------------

void test('computeCoverageCounts: the real project shape (40/31/9 ‖ 69/9/60)', () => {
  const derived = [
    ...Array.from({ length: 31 }, (_, i) =>
      derivedActivity(`C-mapped-${String(i)}`, `comp-${String(i)}`)
    ),
    ...['G-SPA', 'N-IT', 'N-PERF', 'N-QA', 'N-RTH', 'N-SMOKE', 'N-STH', 'N-STP', 'U-SPA-S'].map(
      (n) => derivedActivity(n)
    ),
  ];
  assert.equal(derived.length, 40);

  const legacyIds = [
    ...['G-SPA', 'N-IT', 'N-PERF', 'N-QA', 'N-RTH', 'N-SMOKE', 'N-STH', 'N-STP', 'U-SPA-S'],
    ...Array.from({ length: 60 }, (_, i) => `C-legacy-${String(i)}`),
  ];
  assert.equal(legacyIds.length, 69);

  const counts = computeCoverageCounts(derived, legacyIds);
  assert.deepEqual(counts, {
    derivedTotal: 40,
    mapped: 31,
    crossCutting: 9,
    legacyTotal: 69,
    reconcile: 9,
    orphaned: 60,
  });
  assert.equal(
    coverageStripText(counts),
    'COVERAGE  40 derived · 31 mapped · 9 cross-cutting ‖ 69 legacy · 9 reconcile · 60 orphaned ⚠'
  );
});

// ---------------------------------------------------------------------------
// A DIFFERENTLY-SHAPED fixture — the acceptance test the brief names. If the
// strip's numbers were hardcoded to 40/31/9 ‖ 69/9/60 this would fail.
// ---------------------------------------------------------------------------

void test('computeCoverageCounts: follows a fixture shaped nothing like the real project', () => {
  const derived = [
    derivedActivity('C-one', 'comp-a'),
    derivedActivity('C-two', 'comp-b'),
    derivedActivity('C-three', 'comp-c'),
    derivedActivity('N-cross-1'),
    derivedActivity('N-cross-2'),
  ];
  const legacyIds = ['N-cross-1', 'C-old-1', 'C-old-2', 'C-old-3', 'C-old-4', 'C-old-5'];

  const counts = computeCoverageCounts(derived, legacyIds);
  assert.deepEqual(counts, {
    derivedTotal: 5,
    mapped: 3,
    crossCutting: 2,
    legacyTotal: 6,
    reconcile: 1,
    orphaned: 5,
  });
  assert.equal(
    coverageStripText(counts),
    'COVERAGE  5 derived · 3 mapped · 2 cross-cutting ‖ 6 legacy · 1 reconcile · 5 orphaned ⚠'
  );
});

void test('computeCoverageCounts: empty derived list is 0 mapped / 0 cross-cutting, never NaN', () => {
  const counts = computeCoverageCounts([], ['A', 'B']);
  assert.deepEqual(counts, {
    derivedTotal: 0,
    mapped: 0,
    crossCutting: 0,
    legacyTotal: 2,
    reconcile: 0,
    orphaned: 2,
  });
});

void test('computeCoverageCounts: an empty componentId string counts as no component, not a mapping', () => {
  const counts = computeCoverageCounts([derivedActivity('C-x', '')], []);
  assert.equal(counts.mapped, 0);
  assert.equal(counts.crossCutting, 1);
});

// ---------------------------------------------------------------------------
// The legacy/current partition — the read-only bottom group's membership.
// ---------------------------------------------------------------------------

void test('isOrphanedLegacyActivity: true only for a name the derived set does not carry', () => {
  const names = derivedNameSet([derivedActivity('A'), derivedActivity('B', 'comp-1')]);
  assert.equal(isOrphanedLegacyActivity('A', names), false);
  assert.equal(isOrphanedLegacyActivity('B', names), false);
  assert.equal(isOrphanedLegacyActivity('C', names), true);
});

void test('partitionLegacy: the 9 reconcile rows stay ordinary; only the orphaned ones move', () => {
  const names = derivedNameSet([
    derivedActivity('G-SPA'),
    derivedActivity('C-artifact-access', 'comp-1'),
  ]);
  const rows = [
    { activityId: 'G-SPA', tag: 'reconcile' },
    { activityId: 'C-AA', tag: 'orphan-1' },
    { activityId: 'C-BG', tag: 'orphan-2' },
  ];
  const { current, legacy } = partitionLegacy(rows, names);
  assert.deepEqual(
    current.map((r) => r.activityId),
    ['G-SPA']
  );
  assert.deepEqual(
    legacy.map((r) => r.activityId),
    ['C-AA', 'C-BG']
  );
});

void test('partitionLegacy: nothing orphaned when every row reconciles', () => {
  const names = derivedNameSet([derivedActivity('A'), derivedActivity('B')]);
  const { current, legacy } = partitionLegacy([{ activityId: 'A' }, { activityId: 'B' }], names);
  assert.equal(current.length, 2);
  assert.equal(legacy.length, 0);
});
