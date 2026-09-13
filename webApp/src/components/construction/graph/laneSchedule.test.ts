/**
 * A lane's schedule channels (laneSchedule.ts) — the architect's Q2 tests,
 * each run through the WHOLE chain the console uses: the one shared join
 * (activityMetaFor) → the one activity tree (buildActivityTree) → the lane.
 *
 *   - a missing computed entry draws NO rail, never a 0;
 *   - the critical path never reaches a card or an edge;
 *   - positions do not change when float changes.
 */
/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import type { ConstructionRow, Layer } from '../../../contracts/types.ts';
import { activityMetaFor, type NetworkComputedLike } from '../list/activityMeta.ts';
import { buildActivityTree, type ActivityNode } from '../list/activityTree.ts';
import { buildActivityGraphModel } from './activityGraphModel.ts';
import { layoutActivityGraph } from './activityGraphLayout.ts';
import { cardFrameFor } from './graphCardPresentation.ts';
import { edgePresentationFor } from './graphEdges.ts';
import { laneSpineFor } from './laneSpine.ts';
import {
  SCHEDULE_CAPTION,
  effortText,
  laneScheduleFor,
  maxEffortOf,
  scheduleLine,
} from './laneSchedule.ts';

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

function row(activityId: string, layer?: Layer): ConstructionRow {
  return {
    activityId,
    classified: true,
    hasBuildEvidence: false,
    recorded: false,
    kind: 'service',
    phases: [],
    attempts: [],
    ...(layer !== undefined
      ? { layer, layerBand: 'layered' as const }
      : { layerBand: 'projectWide' as const }),
  };
}

const ROWS = [row('C-m1', 'manager'), row('C-e1', 'engine'), row('N-STP')];

const LIST = [
  { name: 'C-m1', effortDays: 30, componentId: 'm1' },
  { name: 'C-e1', effortDays: 10, componentId: 'e1' },
  { name: 'N-STP', effortDays: 15 },
];

const COMPONENTS = [
  { id: 'm1', name: 'M1', layer: 'manager' as const },
  { id: 'e1', name: 'E1', layer: 'engine' as const },
];

function tree(computed: Record<string, NetworkComputedLike> | undefined): ActivityNode[] {
  return buildActivityTree(ROWS, { meta: activityMetaFor(LIST, computed) });
}

function node(nodes: readonly ActivityNode[], id: string): ActivityNode {
  const n = nodes.find((x) => x.activityId === id);
  assert.ok(n !== undefined, `no node ${id}`);
  return n;
}

function scheduleOf(
  nodes: readonly ActivityNode[],
  id: string
): ReturnType<typeof laneScheduleFor> {
  return laneScheduleFor(node(nodes, id), maxEffortOf(nodes));
}

function modelOf(
  nodes: readonly ActivityNode[]
): ReturnType<typeof buildActivityGraphModel<ActivityNode>> {
  return buildActivityGraphModel({
    components: COMPONENTS,
    relationships: [{ from: 'm1', to: 'e1', mode: 'sync' }],
    activities: nodes,
  });
}

const ALL_CRITICAL: Record<string, NetworkComputedLike> = {
  'C-m1': { totalFloat: 0, onCriticalPath: true, band: 'critical' },
  'C-e1': { totalFloat: 0, onCriticalPath: true, band: 'critical' },
  'N-STP': { totalFloat: 0, onCriticalPath: true, band: 'critical' },
};

const NONE_CRITICAL: Record<string, NetworkComputedLike> = {
  'C-m1': { totalFloat: 25, onCriticalPath: false, band: 'green' },
  'C-e1': { totalFloat: 6, onCriticalPath: false, band: 'yellow' },
  'N-STP': { totalFloat: 40, onCriticalPath: false, band: 'green' },
};

// ---------------------------------------------------------------------------
// Float
// ---------------------------------------------------------------------------

void test('NO computed entry: no rail and no numeral — never a fabricated 0', () => {
  const nodes = tree({ 'C-m1': { totalFloat: 6, onCriticalPath: false, band: 'yellow' } });
  const s = scheduleOf(nodes, 'C-e1');
  assert.equal(s.float, undefined);
  assert.equal(s.critical, false);
  assert.equal(s.borderPx, 2);
});

void test('no network at all: no lane carries a rail', () => {
  const nodes = tree(undefined);
  for (const n of nodes) assert.equal(laneScheduleFor(n, 30).float, undefined, n.activityId);
});

void test('a computed entry draws the rail with its numeral and the server band', () => {
  const s = scheduleOf(tree(NONE_CRITICAL), 'C-e1');
  assert.deepEqual(s.float, { days: 6, numeral: '6', band: 'yellow' });
});

void test('a real zero float is drawn as 0, with the critical band', () => {
  const s = scheduleOf(tree(ALL_CRITICAL), 'C-m1');
  assert.deepEqual(s.float, { days: 0, numeral: '0', band: 'critical' });
});

void test('an unrecognised band keeps the numeral and drops the band', () => {
  const s = scheduleOf(
    tree({ 'C-m1': { totalFloat: 3, onCriticalPath: false, band: 'mauve' } }),
    'C-m1'
  );
  assert.deepEqual(s.float, { days: 3, numeral: '3' });
});

// ---------------------------------------------------------------------------
// Critical path
// ---------------------------------------------------------------------------

void test('the critical path is the lane edge: 3px on the path, 2px off it', () => {
  assert.equal(scheduleOf(tree(ALL_CRITICAL), 'C-m1').critical, true);
  assert.equal(scheduleOf(tree(ALL_CRITICAL), 'C-m1').borderPx, 3);
  assert.equal(scheduleOf(tree(NONE_CRITICAL), 'C-m1').borderPx, 2);
});

void test('the critical path NEVER reaches a card or an edge', () => {
  const on = modelOf(tree(ALL_CRITICAL));
  const off = modelOf(tree(NONE_CRITICAL));
  const none = modelOf(tree(undefined));
  assert.ok(
    on.cards.some((c) => c.lanes.some((l) => l.onCriticalPath === true)),
    'fixture'
  );
  for (const [i, card] of on.cards.entries()) {
    for (const outsideFocus of [false, true]) {
      const frame = cardFrameFor(card, { outsideFocus });
      assert.deepEqual(frame, cardFrameFor(off.cards[i] ?? card, { outsideFocus }), card.id);
      assert.deepEqual(frame, cardFrameFor(none.cards[i] ?? card, { outsideFocus }), card.id);
    }
  }
  assert.equal(on.edges.length, 1);
  for (const [i, e] of on.edges.entries()) {
    const other = off.edges[i];
    assert.ok(other !== undefined);
    assert.deepEqual(edgePresentationFor(e, null), edgePresentationFor(other, null));
  }
});

// ---------------------------------------------------------------------------
// Determinism
// ---------------------------------------------------------------------------

void test('positions and sizes do NOT change when float or criticality changes', () => {
  const base = layoutActivityGraph(modelOf(tree(undefined)));
  for (const computed of [ALL_CRITICAL, NONE_CRITICAL]) {
    const next = layoutActivityGraph(modelOf(tree(computed)));
    assert.deepEqual([...next.pos.entries()], [...base.pos.entries()]);
    assert.deepEqual([...next.size.entries()], [...base.size.entries()]);
  }
});

// ---------------------------------------------------------------------------
// Effort
// ---------------------------------------------------------------------------

void test('effort sets the spine length against the widest effort in the plan', () => {
  const nodes = tree(NONE_CRITICAL);
  assert.equal(scheduleOf(nodes, 'C-m1').spineFraction, 1);
  assert.equal(scheduleOf(nodes, 'C-e1').spineFraction, 10 / 30);
  assert.equal(scheduleOf(nodes, 'N-STP').spineFraction, 15 / 30);
});

void test('the segments keep their Table A-1 proportions whatever the effort', () => {
  const nodes = tree(NONE_CRITICAL);
  const long = laneSpineFor(node(nodes, 'C-m1')).segments.map((s) => s.fraction);
  const short = laneSpineFor(node(nodes, 'C-e1')).segments.map((s) => s.fraction);
  assert.deepEqual(short, long);
});

void test('no effort on record: no spine fraction, and the spine says so', () => {
  const s = laneScheduleFor({}, 30);
  assert.equal(s.spineFraction, undefined);
  assert.equal(effortText(s), 'effort not on record');
});

// ---------------------------------------------------------------------------
// Words
// ---------------------------------------------------------------------------

void test('the caption names the figures as the unstaffed derived network', () => {
  assert.equal(SCHEDULE_CAPTION, 'Float and critical path of the derived network, unstaffed.');
});

void test('the hover line carries only what is known', () => {
  assert.equal(
    scheduleLine(scheduleOf(tree(ALL_CRITICAL), 'C-m1')),
    '30 days of effort · total float 0 (unstaffed) · critical path'
  );
  assert.equal(scheduleLine(scheduleOf(tree(undefined), 'C-e1')), '10 days of effort');
});
