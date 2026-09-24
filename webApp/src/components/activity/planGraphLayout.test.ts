/// <reference types="node" />
/**
 * Unit tests for the plan GRAPH view's pure layout (planGraphLayout.ts): the
 * front-end chain and its milestone, the build-order stack (resources first,
 * clients last, system testing terminal), the M0 fan into the graph's BUILD
 * ROOTS, N-STP's side lane, the dependency-edge direction (predecessor →
 * dependent, pointing down), and the hover focus.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  PLAN_COL_W,
  PLAN_MILESTONE,
  PLAN_ROW_H,
  PLAN_TILE,
  layoutPlanGraph,
  planFocusFor,
  type PlanTileInput,
} from './planGraphLayout.ts';

const TILES: PlanTileInput[] = [
  { id: '1', row: 'frontEnd', calls: [] },
  { id: '2', row: 'frontEnd', calls: [] },
  { id: '3', row: 'frontEnd', calls: [] },
  { id: 'X-db', row: 'resource', calls: [] },
  { id: 'R-p', row: 'resourceAccess', calls: ['X-db', 'ghost'] },
  { id: 'R-q', row: 'resourceAccess', calls: ['X-db'] },
  { id: 'R-lonely', row: 'resourceAccess', calls: [] },
  { id: 'E-y', row: 'engine', calls: ['R-q'] },
  { id: 'E-x', row: 'engine', calls: ['R-p'] },
  { id: 'M-a', row: 'manager', calls: ['E-x', 'R-p'] },
  { id: 'M-b', row: 'manager', calls: ['E-y', 'R-q'] },
  { id: 'U-web', row: 'client', calls: ['M-a', 'M-b'] },
  { id: 'N-STP', row: 'sideLane', calls: [] },
  { id: 'N-IT', row: 'systemTesting', calls: ['U-web', 'N-STP'] },
];
const L = layoutPlanGraph(TILES, 'M0');

function at(id: string): { x: number; y: number } {
  const p = L.pos.get(id);
  assert.ok(p !== undefined, `no position for ${id}`);
  return p;
}

void test('rows run front end → the build-order stack → system testing, one pitch apart', () => {
  assert.deepEqual(
    L.rows.map((r) => [r.row, r.y]),
    ['frontEnd', 'resource', 'resourceAccess', 'engine', 'manager', 'client', 'systemTesting'].map(
      (row, i) => [row, i * PLAN_ROW_H]
    )
  );
  assert.equal(L.height, 6 * PLAN_ROW_H + PLAN_TILE.h);
});

void test('the front end is a left-to-right chain ending in the milestone', () => {
  assert.deepEqual(
    ['1', '2', '3'].map((id) => at(id)),
    [0, 1, 2].map((i) => ({ x: i * PLAN_COL_W, y: 0 }))
  );
  assert.deepEqual(at('M0'), { x: 3 * PLAN_COL_W, y: (PLAN_TILE.h - PLAN_MILESTONE.h) / 2 });
  assert.deepEqual(
    L.edges.filter((e) => e.kind === 'sequence').map((e) => e.id),
    ['1>2', '2>3', '3>M0']
  );
});

void test('M0 fans into the graph’s BUILD ROOTS only — no known predecessor', () => {
  assert.deepEqual(
    L.edges.filter((e) => e.kind === 'milestone').map((e) => e.to),
    ['X-db', 'R-lonely', 'N-STP']
  );
});

void test('a tile sits under what it depends on (the house barycenter sweep, run in build order)', () => {
  // Authored E-y before E-x, but M-a (left) needs E-x and M-b (right) needs E-y.
  assert.ok(at('E-x').x < at('E-y').x);
  assert.ok(at('R-p').x < at('R-q').x);
});

void test('dependency edges point DOWN in build order: predecessor → dependent', () => {
  const ids = new Set(L.edges.filter((e) => e.kind === 'call').map((e) => e.id));
  for (const id of ['X-db>R-p', 'X-db>R-q', 'R-q>E-y', 'R-p>E-x', 'E-x>M-a', 'R-p>M-a'])
    assert.ok(ids.has(id), id);
  // Managers → their clients, never the reverse.
  assert.ok(ids.has('M-a>U-web') && ids.has('M-b>U-web'));
  assert.ok(!ids.has('U-web>M-a') && !ids.has('U-web>M-b'));
});

void test('N-STP stands in its own side lane, clear of the widest row, full height', () => {
  const stp = at('N-STP');
  const widest = Math.max(...TILES.filter((t) => t.row !== 'sideLane').map((t) => at(t.id).x));
  assert.ok(stp.x >= widest + PLAN_TILE.w + 90, 'clear of the widest row');
  assert.equal(
    L.rows.some((r) => r.row === 'sideLane'),
    false,
    'no gutter band for the lane'
  );
  // Straight down from M0 to N-IT: the lane tile sits centered between them.
  assert.equal(stp.y + PLAN_TILE.h / 2, L.height / 2);
});

void test('N-STP’s chain: M0 gates it directly, and N-IT depends on it', () => {
  const ids = L.edges.map((e) => e.id);
  assert.ok(ids.includes('M0>N-STP'));
  assert.ok(ids.includes('N-STP>N-IT'));
});

void test('a call to an unknown activity is dropped, not drawn to nowhere', () => {
  assert.equal(L.edges.filter((e) => e.to === 'ghost' || e.from === 'ghost').length, 0);
  assert.ok(L.edges.every((e) => L.pos.has(e.from) && L.pos.has(e.to)));
});

void test('no two tiles share a cell', () => {
  const seen = new Set<string>();
  for (const [id, p] of L.pos) {
    const key = `${String(p.x)},${String(p.y)}`;
    assert.ok(!seen.has(key), `${id} lands on an occupied cell`);
    seen.add(key);
  }
});

void test('hovering the middle of a 3-chain lights the whole chain, not its neighbours', () => {
  const tiles = [
    { id: 'A', row: 'resource' as const, calls: [] },
    { id: 'B', row: 'resourceAccess' as const, calls: ['A'] },
    { id: 'C', row: 'engine' as const, calls: ['B'] },
    { id: 'D', row: 'manager' as const, calls: ['C'] },
  ];
  const { edges } = layoutPlanGraph(tiles, undefined);
  const focus = planFocusFor('C', tiles, edges, undefined);
  assert.deepEqual([...focus.tiles].sort(), ['A', 'B', 'C', 'D']);
});

void test('a fork lights both downstream branches and the shared upstream', () => {
  const tiles = [
    { id: 'root', row: 'resource' as const, calls: [] },
    { id: 'left', row: 'engine' as const, calls: ['root'] },
    { id: 'right', row: 'engine' as const, calls: ['root'] },
    { id: 'other', row: 'engine' as const, calls: [] },
  ];
  const { edges } = layoutPlanGraph(tiles, undefined);
  const focus = planFocusFor('root', tiles, edges, undefined);
  assert.deepEqual([...focus.tiles].sort(), ['left', 'right', 'root']);
  assert.ok(!focus.tiles.has('other'), 'an unrelated tile stays dimmed');
});

void test('hovering the milestone lights everything it gates, not just the roots', () => {
  // The REWRITE of the landed :137 test. M0 lights itself and every
  // non-frontEnd tile — the side lane and system testing included, which its
  // own edges never reach — because that is what a forced dependency means.
  // The front-end chain is NOT lit: M0 does not gate what precedes it, and '3'
  // (its direct predecessor) was lit before only as a plain neighbour.
  const f = planFocusFor('M0', TILES, L.edges, 'M0');
  for (const id of ['M0', 'X-db', 'R-lonely', 'N-STP', 'N-IT', 'U-web']) {
    assert.ok(f.tiles.has(id), `${id} should be lit`);
  }
  for (const id of ['1', '2', '3']) {
    assert.ok(!f.tiles.has(id), `${id} is front end and stays dim`);
  }
});

void test('incident now means BOTH ends are lit, not "touches the hovered tile"', () => {
  const tiles = [
    { id: 'A', row: 'resource' as const, calls: [] },
    { id: 'B', row: 'engine' as const, calls: ['A'] },
    { id: 'C', row: 'manager' as const, calls: ['B'] },
    { id: 'Z', row: 'engine' as const, calls: [] },
  ];
  const { edges } = layoutPlanGraph(tiles, undefined);
  const f = planFocusFor('B', tiles, edges, undefined);
  const drawn = edges
    .filter((e) => f.incident(e))
    .map((e) => e.id)
    .sort();
  assert.deepEqual(
    drawn,
    ['A>B', 'B>C'].sort(),
    'the whole lit chain stays drawn, not only B’s own edges'
  );
});

void test('a cycle in the dependency data dims the graph rather than hanging it', () => {
  const tiles = [
    { id: 'X', row: 'engine' as const, calls: ['Y'] },
    { id: 'Y', row: 'engine' as const, calls: ['X'] },
  ];
  const { edges } = layoutPlanGraph(tiles, undefined);
  const focus = planFocusFor('X', tiles, edges, undefined);
  assert.deepEqual([...focus.tiles].sort(), ['X', 'Y']);
});

void test('no front end is just the build-order stack', () => {
  const l = layoutPlanGraph(
    TILES.filter((t) => t.row !== 'frontEnd'),
    undefined
  );
  assert.equal(l.rows[0]?.row, 'resource');
  assert.equal(l.edges.filter((e) => e.kind !== 'call').length, 0);
});
