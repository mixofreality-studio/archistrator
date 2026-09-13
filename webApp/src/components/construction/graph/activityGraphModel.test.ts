/**
 * The GRAPH lens's model (activityGraphModel.ts) — placement by the activity's
 * own layer (R5), hollow coverage, the App C edge alarm and its one carve-out,
 * and determinism. Pure, so pinned directly under node:test.
 */
/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import {
  buildActivityGraphModel,
  type ActivityGraphModel,
  type GraphActivityLike,
  type GraphCard,
  type GraphComponentLike,
  type GraphEdge,
  type GraphRelationshipLike,
} from './activityGraphModel.ts';

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

function comp(id: string, layer: GraphComponentLike['layer'], name = id): GraphComponentLike {
  return { id, name, layer };
}

function rel(
  from: string,
  to: string,
  mode: GraphRelationshipLike['mode'] = 'sync'
): GraphRelationshipLike {
  return { from, to, mode };
}

function act(activityId: string, over: Partial<GraphActivityLike> = {}): GraphActivityLike {
  return { activityId, label: activityId, ...over };
}

const COMPONENTS: GraphComponentLike[] = [
  comp('web-client', 'client', 'WebClient'),
  comp('mcp-client', 'client', 'MCPClient'),
  comp('x-manager', 'manager', 'XManager'),
  comp('y-manager', 'manager', 'YManager'),
  comp('a-engine', 'engine', 'AEngine'),
  comp('b-engine', 'engine', 'BEngine'),
  comp('s-access', 'resourceAccess', 'SAccess'),
  comp('db', 'resource', 'Db'),
  comp('logging', 'utility', 'Logging'),
];

const ACTIVITIES: GraphActivityLike[] = [
  act('C-x-manager', { layer: 'manager', layerBand: 'layered', componentId: 'x-manager' }),
  act('C-a-engine', { layer: 'engine', layerBand: 'layered', componentId: 'a-engine' }),
  act('C-s-access', { layer: 'resourceAccess', layerBand: 'layered', componentId: 's-access' }),
  act('R-db', { layer: 'resource', layerBand: 'layered', componentId: 'db' }),
  act('U-SPA-web-client', { layer: 'client', layerBand: 'layered', componentId: 'web-client' }),
  act('N-STP', { layerBand: 'projectWide' }),
  act('N-IT', { layerBand: 'projectWide' }),
];

function model(
  activities: GraphActivityLike[] = ACTIVITIES,
  relationships: GraphRelationshipLike[] = [],
  components: GraphComponentLike[] = COMPONENTS
): ActivityGraphModel {
  return buildActivityGraphModel({ components, relationships, activities });
}

/** A value the test requires to exist — asserted, never `!`-asserted. */
function must<T>(value: T | undefined, what: string): T {
  assert.ok(value !== undefined, `missing: ${what}`);
  return value;
}

function card(m: ActivityGraphModel, id: string): GraphCard {
  return must(
    m.cards.find((c) => c.id === id),
    `card ${id}`
  );
}

function cardFor(m: ActivityGraphModel, activityId: string): GraphCard {
  return card(m, must(m.cardOfActivity[activityId], `card of ${activityId}`));
}

function onlyEdge(m: ActivityGraphModel): GraphEdge {
  assert.equal(m.edges.length, 1);
  return must(m.edges.at(0), 'the edge');
}

function laneIds(c: GraphCard): string[] {
  return c.lanes.map((l) => l.activityId);
}

// ---------------------------------------------------------------------------
// Placement
// ---------------------------------------------------------------------------

void test('every activity is in exactly one card', () => {
  const m = model();
  for (const a of ACTIVITIES) {
    const hosts = m.cards.filter((c) => laneIds(c).includes(a.activityId));
    assert.equal(hosts.length, 1, `${a.activityId} is in ${String(hosts.length)} cards`);
    assert.equal(must(hosts.at(0), a.activityId).id, m.cardOfActivity[a.activityId]);
  }
});

void test('a component activity is a lane on its component card, in the component layer row', () => {
  const c = card(model(), 'x-manager');
  assert.equal(c.kind, 'component');
  assert.equal(c.row, 'manager');
  assert.deepEqual(laneIds(c), ['C-x-manager']);
  assert.equal(c.hollow, false);
});

void test('R5 TRAP: an activity is placed by ITS layer, not its component layer', () => {
  // A pre-D9 U-SPA-<manager>: componentId names the manager, but the server's
  // layer projection says the activity is a Client-layer surface.
  const m = model([
    act('C-x-manager', { layer: 'manager', layerBand: 'layered', componentId: 'x-manager' }),
    act('U-SPA-x-manager', { layer: 'client', layerBand: 'layered', componentId: 'x-manager' }),
  ]);
  assert.deepEqual(
    laneIds(card(m, 'x-manager')),
    ['C-x-manager'],
    'the manager card must not host the client surface'
  );
  const surface = cardFor(m, 'U-SPA-x-manager');
  assert.equal(surface.kind, 'surface');
  assert.equal(surface.row, 'client');
  assert.equal(surface.buildsComponent, 'XManager');
});

void test('an activity whose component is not in the architecture gets its own surface card', () => {
  const m = model([
    act('C-ghost', { layer: 'engine', layerBand: 'layered', componentId: 'ghost' }),
  ]);
  const c = cardFor(m, 'C-ghost');
  assert.equal(c.kind, 'surface');
  assert.equal(c.row, 'engine');
  assert.equal(c.buildsComponent, 'ghost');
});

void test('a project-wide activity joins the System-wide band with no component', () => {
  const m = model();
  for (const id of ['N-STP', 'N-IT']) {
    const c = cardFor(m, id);
    assert.equal(c.kind, 'projectWide', id);
    assert.equal(c.row, 'systemWide', id);
    assert.equal(c.componentId, undefined, id);
  }
});

void test('an activity with no layer is project-wide — a layer is never guessed', () => {
  const m = model([act('X-1', { componentId: 'a-engine' })]);
  assert.equal(cardFor(m, 'X-1').row, 'systemWide');
});

void test('a U-SPA rides on the client component whose layer it shares', () => {
  const c = card(model(), 'web-client');
  assert.deepEqual(laneIds(c), ['U-SPA-web-client']);
  assert.equal(c.row, 'client');
});

void test('one component hosts 1..n lanes, ordered by activity id', () => {
  const m = model([
    act('C-x-manager-b', { layer: 'manager', layerBand: 'layered', componentId: 'x-manager' }),
    act('C-x-manager-a', { layer: 'manager', layerBand: 'layered', componentId: 'x-manager' }),
  ]);
  assert.deepEqual(laneIds(card(m, 'x-manager')), ['C-x-manager-a', 'C-x-manager-b']);
});

// ---------------------------------------------------------------------------
// Coverage
// ---------------------------------------------------------------------------

void test('a LAYERED component with no activity is hollow, and counted', () => {
  const m = model();
  const hollow = m.cards
    .filter((c) => c.hollow)
    .map((c) => c.id)
    .sort();
  assert.deepEqual(hollow, ['b-engine', 'mcp-client', 'y-manager']);
  assert.equal(m.hollowCount, 3);
});

void test('P1-5: a utility is never hollow, and never counted — no activity is its design', () => {
  const logging = card(model(), 'logging');
  assert.equal(logging.lanes.length, 0);
  assert.equal(logging.hollow, false);
  assert.equal(model().hollowCount, 3, 'the one utility is not in the count');
});

void test('a utility sits in the utility row (the side bar), never a layer row', () => {
  assert.equal(card(model(), 'logging').row, 'utility');
});

void test('a surface or project-wide card is never hollow', () => {
  const m = model([
    ...ACTIVITIES,
    act('U-SPA-x-manager', { layer: 'client', layerBand: 'layered', componentId: 'x-manager' }),
  ]);
  for (const c of m.cards) {
    if (c.kind !== 'component') assert.equal(c.hollow, false, c.id);
  }
});

// ---------------------------------------------------------------------------
// Edges
// ---------------------------------------------------------------------------

void test('every edge touching a utility is dropped (no lines to the bar)', () => {
  const m = model(ACTIVITIES, [rel('x-manager', 'logging'), rel('logging', 'a-engine')]);
  assert.deepEqual(m.edges, []);
});

void test('an edge to a component the architecture does not carry is dropped', () => {
  assert.deepEqual(model(ACTIVITIES, [rel('x-manager', 'nowhere')]).edges, []);
});

void test('a downward call is down, no alarm', () => {
  const e = onlyEdge(model(ACTIVITIES, [rel('x-manager', 'a-engine')]));
  assert.equal(e.direction, 'down');
  assert.equal(e.alarm, false);
});

void test('an upward call alarms', () => {
  const m = model(ACTIVITIES, [rel('a-engine', 'x-manager')]);
  const e = onlyEdge(m);
  assert.equal(e.direction, 'up');
  assert.equal(e.alarm, true);
  assert.deepEqual(m.alarms, { up: 1, sideways: 0 });
});

void test('a sideways call between Engines alarms', () => {
  const m = model(ACTIVITIES, [rel('a-engine', 'b-engine')]);
  const e = onlyEdge(m);
  assert.equal(e.direction, 'sideways');
  assert.equal(e.alarm, true);
  assert.deepEqual(m.alarms, { up: 0, sideways: 1 });
});

void test('APP C §3.4: a QUEUED Manager→Manager call is sanctioned, not an alarm', () => {
  const m = model(ACTIVITIES, [rel('x-manager', 'y-manager', 'queued')]);
  const e = onlyEdge(m);
  assert.equal(e.direction, 'sanctionedSideways');
  assert.equal(e.alarm, false);
  assert.equal(m.sanctionedSideways, 1);
  assert.deepEqual(m.alarms, { up: 0, sideways: 0 });
});

void test('APP C §3.4: the same Manager→Manager pair called SYNC alarms', () => {
  const m = model(ACTIVITIES, [rel('x-manager', 'y-manager', 'sync')]);
  const e = onlyEdge(m);
  assert.equal(e.direction, 'sideways');
  assert.equal(e.alarm, true);
  assert.equal(m.sanctionedSideways, 0);
});

void test('the carve-out does not extend to a queued call between non-Managers', () => {
  assert.equal(onlyEdge(model(ACTIVITIES, [rel('a-engine', 'b-engine', 'queued')])).alarm, true);
});

void test('ARCHITECT Q1: a QUEUED Client→Client sideways call alarms — `queued` alone never exempts', () => {
  // The exemption needs BOTH endpoints to be Managers AND mode = queued. A
  // predicate that tests only `mode === 'queued'` (the server's looser rule,
  // designhealthengine.go:2522) would sanction this edge; App C does not.
  const m = model(ACTIVITIES, [rel('web-client', 'mcp-client', 'queued')]);
  const e = onlyEdge(m);
  assert.equal(e.direction, 'sideways');
  assert.equal(e.alarm, true);
  assert.equal(m.sanctionedSideways, 0);
  assert.deepEqual(m.alarms, { up: 0, sideways: 1 });
});

void test('ARCHITECT Q1: an eventPubSub Manager→Manager call alarms — pub/sub goes through a Utility', () => {
  const m = model(ACTIVITIES, [rel('x-manager', 'y-manager', 'eventPubSub')]);
  const e = onlyEdge(m);
  assert.equal(e.direction, 'sideways');
  assert.equal(e.alarm, true);
  assert.equal(m.sanctionedSideways, 0);
});

void test('edge ids stay unique when a pair repeats', () => {
  const m = model(ACTIVITIES, [rel('x-manager', 'a-engine'), rel('x-manager', 'a-engine')]);
  assert.equal(new Set(m.edges.map((e) => e.id)).size, 2);
});

// ---------------------------------------------------------------------------
// Determinism
// ---------------------------------------------------------------------------

void test('the model does not depend on the order activities arrive in', () => {
  const rels = [rel('x-manager', 'a-engine'), rel('a-engine', 's-access')];
  const forward = model(ACTIVITIES, rels);
  const reversed = model([...ACTIVITIES].reverse(), rels);
  assert.deepEqual(
    reversed.cards.map((c) => [c.id, laneIds(c)]),
    forward.cards.map((c) => [c.id, laneIds(c)])
  );
  assert.deepEqual(reversed.cardOfActivity, forward.cardOfActivity);
  assert.deepEqual(reversed.edges, forward.edges);
});

void test('component cards come in architecture order, then surfaces, then project-wide by id', () => {
  const m = model([
    ...ACTIVITIES,
    act('U-SPA-x-manager', { layer: 'client', layerBand: 'layered', componentId: 'x-manager' }),
  ]);
  const kinds = m.cards.map((c) => c.kind);
  const lastComponent = kinds.lastIndexOf('component');
  const firstSurface = kinds.indexOf('surface');
  const firstProjectWide = kinds.indexOf('projectWide');
  assert.ok(lastComponent < firstSurface && firstSurface < firstProjectWide, kinds.join(','));
  assert.deepEqual(m.cards.filter((c) => c.kind === 'projectWide').flatMap(laneIds), [
    'N-IT',
    'N-STP',
  ]);
});
