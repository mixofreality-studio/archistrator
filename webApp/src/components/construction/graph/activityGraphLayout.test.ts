/**
 * The GRAPH lens's layout (activityGraphLayout.ts). The one property the lens
 * cannot live without: the SAME STATE GIVES THE SAME POSITIONS — and "state"
 * means the architecture and the activity set, never a status, an attempt or
 * the order anything arrived in. A card that moved when an activity completed
 * would throw the operator's eye off every 1.5s poll.
 */
/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import {
  buildActivityGraphModel,
  type ActivityGraphModel,
  type GraphActivityLike,
  type GraphComponentLike,
  type GraphRelationshipLike,
} from './activityGraphModel.ts';
import { CARD_W, layoutActivityGraph, type GraphLayout } from './activityGraphLayout.ts';

// ---------------------------------------------------------------------------
// Fixture
// ---------------------------------------------------------------------------

interface Activity extends GraphActivityLike {
  /** Status-like payload the layout must never read. */
  status?: string;
  attempts?: number;
}

function comp(id: string, layer: GraphComponentLike['layer']): GraphComponentLike {
  return { id, name: id, layer };
}

function rel(from: string, to: string): GraphRelationshipLike {
  return { from, to, mode: 'sync' };
}

function act(
  activityId: string,
  layer: GraphActivityLike['layer'],
  componentId?: string,
  extra: Partial<Activity> = {}
): Activity {
  return {
    activityId,
    label: activityId,
    ...(layer !== undefined
      ? { layer, layerBand: 'layered' as const }
      : { layerBand: 'projectWide' as const }),
    ...(componentId !== undefined ? { componentId } : {}),
    ...extra,
  };
}

const COMPONENTS: GraphComponentLike[] = [
  comp('web-client', 'client'),
  comp('x-manager', 'manager'),
  comp('y-manager', 'manager'),
  comp('a-engine', 'engine'),
  comp('b-engine', 'engine'),
  comp('s-access', 'resourceAccess'),
  comp('db', 'resource'),
  comp('logging', 'utility'),
  comp('security', 'utility'),
];

const RELATIONSHIPS: GraphRelationshipLike[] = [
  rel('web-client', 'y-manager'),
  rel('y-manager', 'b-engine'),
  rel('x-manager', 'a-engine'),
  rel('y-manager', 's-access'),
  rel('s-access', 'db'),
  rel('x-manager', 'logging'),
];

const ACTIVITIES: Activity[] = [
  act('C-x-manager', 'manager', 'x-manager'),
  act('C-y-manager', 'manager', 'y-manager'),
  act('C-y-manager-2', 'manager', 'y-manager'),
  act('C-a-engine', 'engine', 'a-engine'),
  act('R-db', 'resource', 'db'),
  act('N-STP', undefined),
];

function modelOf(activities: Activity[] = ACTIVITIES): ActivityGraphModel<Activity> {
  return buildActivityGraphModel({
    components: COMPONENTS,
    relationships: RELATIONSHIPS,
    activities,
  });
}

/** Positions as a plain, key-sorted object, so deepEqual compares values. */
function positions(layout: GraphLayout): Record<string, [number, number]> {
  const out: Record<string, [number, number]> = {};
  for (const id of [...layout.pos.keys()].sort()) {
    const p = layout.pos.get(id);
    assert.ok(p !== undefined);
    out[id] = [p.x, p.y];
  }
  return out;
}

function at(layout: GraphLayout, id: string): { x: number; y: number } {
  const p = layout.pos.get(id);
  assert.ok(p !== undefined, `no position for ${id}`);
  return p;
}

function heightOf(layout: GraphLayout, id: string): number {
  const s = layout.size.get(id);
  assert.ok(s !== undefined, `no size for ${id}`);
  return s.h;
}

// ---------------------------------------------------------------------------
// The golden layout — every coordinate pinned
// ---------------------------------------------------------------------------

void test('GOLDEN: the fixture lays out at exactly these coordinates', () => {
  const layout = layoutActivityGraph(modelOf());
  assert.deepEqual(positions(layout), {
    // The barycenter sweep puts y-manager (called by web-client at x 0) left of
    // x-manager (no caller), then b-engine under y-manager, a-engine under
    // x-manager. The 2-lane y-manager card makes the Managers row taller.
    'a-engine': [220, 350],
    'activity:N-STP': [0, 824],
    'b-engine': [0, 350],
    db: [0, 666],
    logging: [512, 34],
    's-access': [0, 508],
    security: [512, 136],
    'web-client': [0, 0],
    'x-manager': [220, 158],
    'y-manager': [0, 158],
  });
  assert.deepEqual(
    layout.rows.map((r) => [r.row, r.y, r.height]),
    [
      ['client', 0, 86],
      ['manager', 158, 120],
      ['engine', 350, 86],
      ['resourceAccess', 508, 86],
      ['resource', 666, 86],
      ['systemWide', 824, 86],
    ]
  );
  assert.deepEqual(layout.bar, { x: 512, top: 0, bottom: 238 });
  assert.equal(layout.width, 512 + CARD_W);
  assert.equal(layout.height, 910);
});

// ---------------------------------------------------------------------------
// Same state, same positions
// ---------------------------------------------------------------------------

void test('the same state lays out identically twice', () => {
  assert.deepEqual(
    positions(layoutActivityGraph(modelOf())),
    positions(layoutActivityGraph(modelOf()))
  );
});

void test('STATUS IS NOT AN INPUT: changing every activity status moves nothing', () => {
  const before = positions(layoutActivityGraph(modelOf()));
  const changed = ACTIVITIES.map((a) => ({ ...a, status: 'integrated', attempts: 12 }));
  assert.deepEqual(positions(layoutActivityGraph(modelOf(changed))), before);
});

void test('ARRIVAL ORDER IS NOT AN INPUT: reversed activities lay out identically', () => {
  assert.deepEqual(
    positions(layoutActivityGraph(modelOf([...ACTIVITIES].reverse()))),
    positions(layoutActivityGraph(modelOf()))
  );
});

// ---------------------------------------------------------------------------
// Shape
// ---------------------------------------------------------------------------

void test('rows run top→down in Method order, System-wide last, and never overlap', () => {
  const layout = layoutActivityGraph(modelOf());
  const order = layout.rows.map((r) => r.row);
  assert.deepEqual(order, [
    'client',
    'manager',
    'engine',
    'resourceAccess',
    'resource',
    'systemWide',
  ]);
  for (let i = 1; i < layout.rows.length; i += 1) {
    const prev = layout.rows[i - 1];
    const cur = layout.rows[i];
    assert.ok(prev !== undefined && cur !== undefined);
    assert.ok(prev.y + prev.height < cur.y, `${prev.row} overlaps ${cur.row}`);
  }
});

void test('a row with no cards is skipped', () => {
  const model = buildActivityGraphModel({
    components: [comp('m', 'manager'), comp('r', 'resourceAccess')],
    relationships: [rel('m', 'r')],
    activities: [],
  });
  assert.deepEqual(
    layoutActivityGraph(model).rows.map((r) => r.row),
    ['manager', 'resourceAccess']
  );
});

void test('every utility sits in the side bar, clear of every row', () => {
  const layout = layoutActivityGraph(modelOf());
  const bar = layout.bar;
  assert.ok(bar !== undefined);
  for (const id of ['logging', 'security']) assert.equal(at(layout, id).x, bar.x, id);
  for (const id of [
    'web-client',
    'x-manager',
    'y-manager',
    'a-engine',
    'b-engine',
    's-access',
    'db',
    'activity:N-STP',
  ]) {
    assert.ok(at(layout, id).x + CARD_W < bar.x, `${id} reaches the bar`);
  }
});

void test('no bar is drawn when the architecture has no utilities', () => {
  const model = buildActivityGraphModel({
    components: [comp('m', 'manager')],
    relationships: [],
    activities: [],
  });
  assert.equal(layoutActivityGraph(model).bar, undefined);
});

void test('a 2-lane card is taller than a 1-lane card, and its row grows to fit', () => {
  const layout = layoutActivityGraph(modelOf());
  assert.ok(heightOf(layout, 'y-manager') > heightOf(layout, 'x-manager'));
  const managers = layout.rows.find((r) => r.row === 'manager');
  assert.ok(managers !== undefined);
  assert.equal(managers.height, heightOf(layout, 'y-manager'));
});

void test('a hollow card keeps a one-lane height, so "no activity" has room', () => {
  const layout = layoutActivityGraph(modelOf());
  assert.equal(heightOf(layout, 'web-client'), heightOf(layout, 'x-manager'));
});

void test('each row has a label, and System-wide reads as such', () => {
  const labels = layoutActivityGraph(modelOf()).rows.map((r) => r.label);
  assert.deepEqual(labels, [
    'Clients',
    'Managers',
    'Engines',
    'Resource\nAccess',
    'Resources',
    'System-wide',
  ]);
});

void test('a row is as tall as its TALLEST card, even when that card is not first', () => {
  // The golden fixture's tall card happens to sit first in its row; this one
  // puts it second, so a "first card's height" shortcut cannot pass.
  const model = buildActivityGraphModel({
    components: [comp('m1', 'manager'), comp('m2', 'manager')],
    relationships: [],
    activities: [
      act('C-m1', 'manager', 'm1'),
      act('C-m2-a', 'manager', 'm2'),
      act('C-m2-b', 'manager', 'm2'),
    ],
  });
  const layout = layoutActivityGraph(model);
  assert.ok(at(layout, 'm1').x < at(layout, 'm2').x, 'fixture: the tall card must be second');
  const managers = layout.rows.find((r) => r.row === 'manager');
  assert.ok(managers !== undefined);
  assert.equal(managers.height, heightOf(layout, 'm2'));
  assert.ok(heightOf(layout, 'm2') > heightOf(layout, 'm1'));
});
