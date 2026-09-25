/// <reference types="node" />
/**
 * Unit tests for the activity lifecycle graph's pixel geometry
 * (src/components/activity/lifecycleGraphGeometry.ts): the regular pitch, the
 * elastic columns (active pill, label-wide phases, bends), forward arrowheads,
 * return arcs, which side a phase label lands on, and lane labels.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { layoutLifecycleGraph } from './lifecycleGraphLayout.ts';
import { LIFECYCLE_METRICS as M, lifecycleGeometry } from './lifecycleGraphGeometry.ts';

// Figure A-1, cut down: a trunk a→b→c→d→j with a branch b→x→y→j.
const NODES = [
  { id: 'a', phase: 'req', dependsOn: [] },
  { id: 'b', phase: 'req', dependsOn: ['a'] },
  { id: 'c', phase: 'dd', dependsOn: ['b'] },
  { id: 'd', phase: 'dd', dependsOn: ['c'] },
  { id: 'x', phase: 'tp', dependsOn: ['b'] },
  { id: 'y', phase: 'tp', dependsOn: ['x'] },
  { id: 'j', phase: 'int', dependsOn: ['d', 'y'] },
];
const LAYOUT = layoutLifecycleGraph(NODES);
const PITCH = M.pip + M.gap;

function xOf(g: ReturnType<typeof lifecycleGeometry>, id: string): number {
  const x = g.x.get(id);
  assert.ok(x !== undefined, `no x for ${id}`);
  return x;
}

void test('with nothing active and no labels, a straight chain sits on the regular pitch', () => {
  const chain = ['a', 'b', 'c', 'd'].map((id, i, all) => ({
    id,
    phase: '',
    dependsOn: i === 0 ? [] : [all[i - 1] ?? ''],
  }));
  const g = lifecycleGeometry(layoutLifecycleGraph(chain), chain, [], undefined);
  assert.deepEqual(
    chain.map((n) => xOf(g, n.id)),
    [0, 1, 2, 3].map((col) => M.pip / 2 + col * PITCH)
  );
  assert.equal(g.width, 4 * PITCH - M.gap);
  assert.equal(g.height, M.laneHeight);
});

void test('a branch node shares its column with the trunk, one lane down', () => {
  const g = lifecycleGeometry(LAYOUT, NODES, [], undefined);
  assert.equal(xOf(g, 'x'), xOf(g, 'c'));
  assert.equal(g.height, 2 * M.laneHeight);
});

void test('the active column widens by the pill, pushing every later column right', () => {
  const g = lifecycleGeometry(LAYOUT, NODES, [], { nodeId: 'c', left: 20, right: 150 });
  const plain = lifecycleGeometry(LAYOUT, NODES, [], undefined);
  const lead = 20 - M.pip / 2;
  assert.equal(xOf(g, 'c'), xOf(plain, 'c') + lead);
  assert.equal(xOf(g, 'd') - xOf(g, 'c'), 150 + M.gap + M.pip / 2);
  assert.equal(xOf(g, 'x'), xOf(g, 'c'), 'the branch pip stays under the pill’s pip');
});

void test('a phase narrower than its label stretches the gaps BETWEEN its columns', () => {
  const g = lifecycleGeometry(LAYOUT, NODES, [{ id: 'req', labelWidth: 120 }], undefined);
  const box = g.phases.find((p) => p.id === 'req');
  assert.ok(box !== undefined);
  assert.deepEqual([box.x, box.width], [0, 120]);
  assert.equal(xOf(g, 'b') + M.pip / 2, 120, 'the phase still ends at its last pip');
});

void test('a one-column phase makes room for its whole label', () => {
  const g = lifecycleGeometry(LAYOUT, NODES, [{ id: 'int', labelWidth: 90 }], undefined);
  const box = g.phases.find((p) => p.id === 'int');
  assert.ok(box !== undefined);
  assert.equal(box.x, xOf(g, 'j') - M.pip / 2, 'over the phase’s first column');
  assert.ok(g.width >= box.x + 90);
});

void test('adjacent phase labels never overlap, however long', () => {
  const phases = ['req', 'dd', 'int'].map((id) => ({ id, labelWidth: 170 }));
  const g = lifecycleGeometry(LAYOUT, NODES, phases, { nodeId: 'c', left: 20, right: 150 });
  const top = g.phases.filter((p) => p.side === 'top' && p.row === 0);
  assert.equal(top.length, 3, 'and none had to be pushed to a second row');
  top.forEach((b, i) => {
    const next = top[i + 1];
    if (next !== undefined) assert.ok(b.x + b.width <= next.x, `${b.id} runs into ${next.id}`);
  });
});

void test('a trunk phase is labelled above the rails, a branch-only phase below', () => {
  const g = lifecycleGeometry(
    LAYOUT,
    NODES,
    [
      { id: 'dd', labelWidth: 10 },
      { id: 'tp', labelWidth: 10 },
    ],
    undefined
  );
  assert.deepEqual(
    g.phases.map((p) => [p.id, p.side, p.row]),
    [
      ['dd', 'top', 0],
      ['tp', 'bottom', 0],
    ]
  );
  assert.equal(g.railsTop, M.labelRow);
  assert.equal(g.height, M.labelRow + 2 * M.laneHeight + M.labelRow);
});

void test('the pill stretches only the phase that owns it', () => {
  // `d` (trunk, col 3) is active; `y` (branch, col 3) merely shares its column. The
  // branch phase runs from `x`'s pip to `y`'s pip and is exactly as wide as its
  // label needs — the pill next door adds nothing to it.
  const tp = [{ id: 'tp', labelWidth: 95 }];
  const extent = (g: ReturnType<typeof lifecycleGeometry>): number => {
    const box = g.phases.find((p) => p.id === 'tp');
    assert.ok(box !== undefined);
    return xOf(g, 'y') + M.pip / 2 - box.x;
  };
  assert.equal(extent(lifecycleGeometry(LAYOUT, NODES, tp, undefined)), 95);
  assert.equal(
    extent(lifecycleGeometry(LAYOUT, NODES, tp, { nodeId: 'd', left: 20, right: 150 })),
    95
  );
});

void test('labels that would overlap on one side stack into rows', () => {
  const nodes = [
    { id: 'a', phase: 'p', dependsOn: [] },
    { id: 'b', phase: 'q', dependsOn: ['a'] },
  ];
  // Both phases start within one pitch of each other and `p` is a one-column
  // phase whose label is as wide as the graph: `q` cannot share its row… except
  // that the elastic columns make room first. They must end up clear EITHER way.
  const g = lifecycleGeometry(
    layoutLifecycleGraph(nodes),
    nodes,
    [
      { id: 'p', labelWidth: 200 },
      { id: 'q', labelWidth: 200 },
    ],
    undefined
  );
  const [p, q] = g.phases;
  assert.ok(p !== undefined && q !== undefined);
  assert.ok(p.row !== q.row || p.x + p.width <= q.x);
});

void test('a bend keeps its full run and finishes ahead of the arrowhead', () => {
  const nodes = [
    { id: 'f', phase: '', dependsOn: [] },
    { id: 'p', phase: '', dependsOn: ['f'] },
    { id: 'q', phase: '', dependsOn: ['f'] },
    { id: 'r', phase: '', dependsOn: ['f'] },
  ];
  const g = lifecycleGeometry(layoutLifecycleGraph(nodes), nodes, [], undefined);
  const rail = g.rails.find((e) => e.to === 'r');
  assert.ok(rail !== undefined);
  // M x y L bendStart y C c1x c1y c2x c2y bendEnd y L stop y
  const n = (rail.d.match(/-?\d+(\.\d+)?/g) ?? []).map(Number);
  const [bendStart, bendEnd, stop] = [n[2] ?? 0, n[8] ?? 0, n[10] ?? 0];
  assert.equal(bendEnd - bendStart, M.bend + M.bendPerLane, 'two lanes ⇒ the longer run');
  assert.ok(bendStart >= xOf(g, 'f'), 'starting no earlier than its source');
  assert.equal(stop, rail.arrow.x - M.arrowLen);
  assert.ok(bendEnd < stop, 'the arrowhead sits on straight rail');
});

void test('a lane change ends square-on: the last control point shares the target’s y', () => {
  const g = lifecycleGeometry(LAYOUT, NODES, [], undefined);
  const rail = g.rails.find((e) => e.from === 'y' && e.to === 'j');
  assert.ok(rail !== undefined);
  const n = (rail.d.match(/-?\d+(\.\d+)?/g) ?? []).map(Number);
  // … C c1x c1y c2x c2y endX endY L stopX stopY
  const [c2y, endY, stopY] = [n.at(-5), n.at(-3), n.at(-1)];
  assert.equal(c2y, endY);
  assert.equal(stopY, g.laneY(0));
  assert.deepEqual(rail.arrow, {
    x: xOf(g, 'j') - M.pip / 2 - M.arrowGap,
    y: g.laneY(0),
    dir: 'right',
  });
});

// ── arrowheads ──────────────────────────────────────────────────────────────

void test('every rail ends in a forward arrowhead that touches no pip and no pill', () => {
  for (const active of [
    undefined,
    { nodeId: 'c', left: 20, right: 150 },
    { nodeId: 'j', left: 20, right: 150 },
  ]) {
    const g = lifecycleGeometry(LAYOUT, NODES, [], active);
    assert.equal(g.rails.length, LAYOUT.edges.length);
    for (const rail of g.rails) {
      const { x: tip, y } = rail.arrow;
      assert.equal(rail.arrow.dir, 'right');
      const lane = LAYOUT.positions.get(rail.to)?.lane ?? 0;
      assert.equal(y, g.laneY(lane), 'on its target’s lane');
      for (const n of NODES) {
        if (LAYOUT.positions.get(n.id)?.lane !== lane) continue;
        const [left, right] =
          n.id === active?.nodeId ? [active.left, active.right] : [M.pip / 2, M.pip / 2];
        const clear =
          tip <= xOf(g, n.id) - left - M.arrowGap + 0.01 ||
          tip - M.arrowLen >= xOf(g, n.id) + right;
        assert.ok(clear, `${rail.from}→${rail.to}'s arrowhead overlaps ${n.id}`);
      }
    }
  }
});

// ── return arcs ─────────────────────────────────────────────────────────────

void test('a return arc is drawn for exactly the back edges asked for', () => {
  assert.deepEqual(lifecycleGeometry(LAYOUT, NODES, [], undefined).backArcs, []);
  const g = lifecycleGeometry(LAYOUT, NODES, [], undefined, M, [
    { from: 'd', to: 'c' },
    { from: 'ghost', to: 'c' },
  ]);
  assert.deepEqual(
    g.backArcs.map((a) => [a.from, a.to]),
    [['d', 'c']]
  );
});

void test('a return arc points DOWN at the dispatch task, from above both pips', () => {
  const g = lifecycleGeometry(LAYOUT, NODES, [], undefined, M, [{ from: 'd', to: 'c' }]);
  const [arc] = g.backArcs;
  assert.ok(arc !== undefined);
  assert.deepEqual(arc.arrow, {
    x: xOf(g, 'c'),
    y: g.laneY(0) - M.pip / 2 - M.arcLand,
    dir: 'down',
  });
  assert.ok(arc.box.y + arc.box.height <= g.laneY(0) - M.pip / 2, 'wholly above the pip tops');
  assert.equal(arc.badge.x, (xOf(g, 'c') + xOf(g, 'd')) / 2, 'the count rides the crown');
});

void test('lane 0’s arc gets a band of its own, under the phase labels', () => {
  const phases = [{ id: 'dd', labelWidth: 40 }];
  const without = lifecycleGeometry(LAYOUT, NODES, phases, undefined);
  const g = lifecycleGeometry(LAYOUT, NODES, phases, undefined, M, [{ from: 'd', to: 'c' }]);
  const [arc] = g.backArcs;
  assert.ok(arc !== undefined);
  assert.ok(g.railsTop > without.railsTop);
  // Phase labels occupy [0, topRows·labelRow); the arc, in strip space, starts after.
  assert.ok(g.railsTop + arc.box.y >= g.topRows * M.labelRow);
});

void test('an arc on a lower lane clears the rail and the pips of the lane above', () => {
  const g = lifecycleGeometry(LAYOUT, NODES, [], undefined, M, [{ from: 'y', to: 'x' }]);
  const [arc] = g.backArcs;
  assert.ok(arc !== undefined);
  assert.ok(arc.box.y >= g.laneY(0) + M.pip / 2, 'below lane 0’s pips (and so its rail)');
  assert.ok(arc.box.y + arc.box.height <= g.laneY(1) - M.pip / 2, 'above its own');
  assert.equal(g.railsTop, 0, 'and it needs no band');
});

void test('an arc still clears the pill when its dispatch task is the active one', () => {
  const active = { nodeId: 'c', left: 20, right: 150 };
  const g = lifecycleGeometry(LAYOUT, NODES, [], active, M, [{ from: 'd', to: 'c' }]);
  const [arc] = g.backArcs;
  assert.ok(arc !== undefined);
  // The pill is ~16px tall either side of the lane centre.
  assert.ok(arc.arrow.y <= g.laneY(0) - 16.5);
  assert.equal(arc.arrow.x, xOf(g, 'c'), 'aimed at the pip inside the pill');
});

// ── lane labels ─────────────────────────────────────────────────────────────

// The 3-way fork (the sibling project-design options): normal → {dec, sub, cmp} → risk.
const FORK = [
  { id: 'normal', phase: 'n', dependsOn: [] },
  { id: 'decD', phase: 'o', dependsOn: ['normal'], laneLabelWidth: 74 },
  { id: 'decR', phase: 'o', dependsOn: ['decD'] },
  { id: 'subD', phase: 'o', dependsOn: ['normal'], laneLabelWidth: 68 },
  { id: 'subR', phase: 'o', dependsOn: ['subD'] },
  { id: 'cmpD', phase: 'o', dependsOn: ['normal'], laneLabelWidth: 62 },
  { id: 'cmpR', phase: 'o', dependsOn: ['cmpD'] },
  { id: 'risk', phase: 'r', dependsOn: ['decR', 'subR', 'cmpR'] },
];
const FORK_LAYOUT = layoutLifecycleGraph(FORK);
const FORK_PHASES = [
  { id: 'n', labelWidth: 40 },
  { id: 'o', labelWidth: 50 },
  { id: 'r', labelWidth: 30 },
];

void test('a fork names every branch, the trunk included, as one left-aligned legend', () => {
  const g = lifecycleGeometry(FORK_LAYOUT, FORK, FORK_PHASES, undefined);
  assert.deepEqual(
    g.laneLabels.map((l) => l.nodeId),
    ['decD', 'subD', 'cmpD']
  );
  assert.equal(new Set(g.laneLabels.map((l) => l.x)).size, 1, 'one x for the whole fork');
  g.laneLabels.forEach((l, lane) => {
    assert.equal(l.y + l.height / 2, g.laneY(lane), 'centred on its own rail');
  });
});

void test('a lane label clears every pip, stays inside the rail band, and no bend enters it', () => {
  for (const active of [
    undefined,
    { nodeId: 'normal', left: 20, right: 160 },
    { nodeId: 'subD', left: 20, right: 160 },
  ]) {
    const g = lifecycleGeometry(FORK_LAYOUT, FORK, FORK_PHASES, active);
    const reach = (id: string): [number, number] =>
      id === active?.nodeId ? [active.left, active.right] : [M.pip / 2, M.pip / 2];
    for (const l of g.laneLabels) {
      // pips (and the pill) on the label's own lane
      const lane = FORK_LAYOUT.positions.get(l.nodeId)?.lane;
      for (const n of FORK) {
        if (FORK_LAYOUT.positions.get(n.id)?.lane !== lane) continue;
        const [left, right] = reach(n.id);
        const clear = l.x + l.width <= xOf(g, n.id) - left || l.x >= xOf(g, n.id) + right;
        assert.ok(clear, `${l.nodeId}'s label overlaps ${n.id}`);
      }
      // brackets live outside [0, railsHeight]; a label never leaves its lane's band
      assert.ok(
        l.y >= (lane ?? 0) * M.laneHeight && l.y + l.height <= ((lane ?? 0) + 1) * M.laneHeight
      );
      assert.ok(l.height <= M.laneHeight - M.pip, 'fits between two lanes’ pips');
      // …and the arrowhead into its node sits clear of it, on the pip's side
      const into = g.rails.find((e) => e.to === l.nodeId);
      assert.ok(into !== undefined && into.arrow.x - M.arrowLen >= l.x + l.width);
      // every bend (the C segment) has finished before the label starts
      for (const rail of g.rails) {
        for (const c of rail.d.matchAll(
          /L(-?[\d.]+) -?[\d.]+ C[^L]*? (-?[\d.]+) -?[\d.]+(?= L|$)/g
        )) {
          const [from, to] = [Number(c[1]), Number(c[2])];
          const outside = to <= l.x || from >= l.x + l.width;
          assert.ok(outside, `${rail.from}→${rail.to} bends through ${l.nodeId}'s label`);
        }
      }
    }
    // the bend into a labelled column keeps its full run
    const rail = g.rails.find((e) => e.to === 'cmpD');
    const nums = (rail?.d ?? '').match(/-?\d+(\.\d+)?/g)?.map(Number) ?? [];
    // M x y L bendStart y C c1x c1y c2x c2y bendEnd y L stop y
    assert.ok((nums[8] ?? 0) - (nums[2] ?? 0) >= M.bend + M.bendPerLane - 0.2);
  }
});

void test('a branch already named beneath the rails is not labelled twice', () => {
  // Figure A-1: `tp` lives wholly on the branch, so its phase label goes below and IS the name.
  const nodes = NODES.map((n) => (n.id === 'x' ? { ...n, laneLabelWidth: 50 } : n));
  const g = lifecycleGeometry(LAYOUT, nodes, [{ id: 'tp', labelWidth: 10 }], undefined);
  assert.deepEqual(g.laneLabels, []);
  const plain = lifecycleGeometry(LAYOUT, NODES, [{ id: 'tp', labelWidth: 10 }], undefined);
  assert.equal(g.width, plain.width, 'and it reserves no room');
});
