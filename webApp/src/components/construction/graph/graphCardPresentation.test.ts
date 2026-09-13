/**
 * How a card is framed (graphCardPresentation.ts) — coverage, hover-focus, and
 * the designer's P1-4 filter rule: a card dims as a whole when a filter is
 * active and none of its lanes match, which includes every hollow card and
 * every utility.
 */
/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import type { GraphCard } from './activityGraphModel.ts';
import { cardDimmed, cardFrameFor } from './graphCardPresentation.ts';

function card(
  row: GraphCard['row'],
  laneIds: string[]
): Pick<GraphCard, 'hollow' | 'row' | 'lanes'> {
  return {
    // The model's own rule: a layered component with no lane; never a utility.
    hollow: laneIds.length === 0 && row !== 'systemWide' && row !== 'utility',
    row,
    lanes: laneIds.map((activityId) => ({ activityId, label: activityId })),
  };
}

const TWO_LANES = card('manager', ['C-a', 'C-b']);
const HOLLOW = card('engine', []);
const UTILITY = card('utility', []);

void test('at rest nothing is dimmed', () => {
  for (const c of [TWO_LANES, HOLLOW, UTILITY]) {
    assert.equal(cardDimmed(cardFrameFor(c, { outsideFocus: false })), false);
  }
});

void test('P1-4: with a filter active, a card none of whose lanes match dims as a whole', () => {
  const f = cardFrameFor(TWO_LANES, {
    outsideFocus: false,
    filterActive: true,
    unmatched: new Set(['C-a', 'C-b']),
  });
  assert.equal(f.filterDimmed, true);
  assert.equal(cardDimmed(f), true);
});

void test('P1-4: one matching lane keeps the card lit', () => {
  const f = cardFrameFor(TWO_LANES, {
    outsideFocus: false,
    filterActive: true,
    unmatched: new Set(['C-a']),
  });
  assert.equal(f.filterDimmed, false);
});

void test('P1-4: hollow and utility cards dim whenever a filter is active', () => {
  for (const c of [HOLLOW, UTILITY]) {
    const f = cardFrameFor(c, { outsideFocus: false, filterActive: true, unmatched: new Set() });
    assert.equal(f.filterDimmed, true, c.row);
  }
});

void test('no filter active: an all-unmatched card is not filter-dimmed', () => {
  const f = cardFrameFor(TWO_LANES, {
    outsideFocus: false,
    filterActive: false,
    unmatched: new Set(['C-a', 'C-b']),
  });
  assert.equal(f.filterDimmed, false);
});

void test('hover-focus mutes a card outside the neighbourhood, but never a utility', () => {
  assert.equal(cardFrameFor(TWO_LANES, { outsideFocus: true }).muted, true);
  assert.equal(cardFrameFor(UTILITY, { outsideFocus: true }).muted, false);
});

void test('P1-5: a utility is solid and muted, with no layer edge — never dashed', () => {
  const f = cardFrameFor(UTILITY, { outsideFocus: false });
  assert.equal(f.utility, true);
  assert.equal(f.hollow, false);
  assert.equal(f.borderStyle, 'solid');
  assert.equal(f.layerEdge, false);
});

void test('a hollow card is dashed and carries no layer edge', () => {
  const f = cardFrameFor(HOLLOW, { outsideFocus: false });
  assert.equal(f.borderStyle, 'dashed');
  assert.equal(f.layerEdge, false);
});
