/// <reference types="node" />
/**
 * Unit tests for the activity lifecycle graph's pure grid layout
 * (src/components/activity/lifecycleGraphLayout.ts): longest-path columns, the
 * git-style lane rules (first-authored chain keeps the trunk, a branch returns to
 * its join's lane, a freed lane is reused) and the rail waypoints, over the four
 * shapes the Activity Experience has to draw — a linear chain, Figure A-1's
 * fork/join, a 3-way fork/join, and a branch that skips columns before joining.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  layoutLifecycleGraph,
  type LifecycleGridPoint,
  type LifecycleLayout,
  type LifecycleLayoutInput,
} from './lifecycleGraphLayout.ts';

/** `a>b>c` chains, comma-separated, into authored-order nodes (first mention wins). */
function graph(spec: string): LifecycleLayoutInput[] {
  const nodes = new Map<string, string[]>();
  for (const chain of spec.split(',')) {
    const ids = chain.trim().split('>');
    ids.forEach((id, i) => {
      const deps = nodes.get(id) ?? [];
      const prev = ids[i - 1];
      if (prev !== undefined && !deps.includes(prev)) deps.push(prev);
      nodes.set(id, deps);
    });
  }
  return [...nodes].map(([id, dependsOn]) => ({ id, dependsOn }));
}

function at(layout: LifecycleLayout, id: string): LifecycleGridPoint {
  const p = layout.positions.get(id);
  assert.ok(p !== undefined, `no position for ${id}`);
  return p;
}

function rail(layout: LifecycleLayout, from: string, to: string): [number, number][] {
  const e = layout.edges.find((x) => x.from === from && x.to === to);
  assert.ok(e !== undefined, `no edge ${from}→${to}`);
  return e.points.map((p) => [p.col, p.lane]);
}

/** Figure A-1: the trunk through construction, the STP branch, the Testing join. */
const FIGURE_A1 = graph(
  'srs>srsReview>someConstruction>detailedDesign>designReview>construction>codeReview>integration>testing,' +
    'srsReview>stp>stpReview>testing'
);

void test('a linear chain is one lane, one column per node, straight rails', () => {
  const l = layoutLifecycleGraph(graph('a>b>c>d'));
  assert.deepEqual([l.cols, l.lanes], [4, 1]);
  assert.deepEqual(
    ['a', 'b', 'c', 'd'].map((id) => at(l, id)),
    [0, 1, 2, 3].map((col) => ({ col, lane: 0 }))
  );
  assert.deepEqual(rail(l, 'b', 'c'), [
    [1, 0],
    [2, 0],
  ]);
});

void test('Figure A-1: the first-authored chain keeps lane 0, the STP branch takes lane 1', () => {
  const l = layoutLifecycleGraph(FIGURE_A1);
  assert.deepEqual([l.cols, l.lanes], [9, 2]);
  for (const id of ['srs', 'srsReview', 'someConstruction', 'integration', 'testing']) {
    assert.equal(at(l, id).lane, 0, `${id} is on the trunk`);
  }
  assert.deepEqual(at(l, 'stp'), { col: 2, lane: 1 });
  assert.deepEqual(at(l, 'stpReview'), { col: 3, lane: 1 });
  // The join sits after the SLOWEST parent (Integration, col 7), not after STP Review.
  assert.deepEqual(at(l, 'testing'), { col: 8, lane: 0 });
});

void test('Figure A-1: the fork bends out in its first gap', () => {
  const l = layoutLifecycleGraph(FIGURE_A1);
  assert.deepEqual(rail(l, 'srsReview', 'stp'), [
    [1, 0],
    [2, 1],
  ]);
});

void test('a branch that skips columns rides ITS lane and bends in at the join', () => {
  const l = layoutLifecycleGraph(FIGURE_A1);
  // STP Review (col 3) → Testing (col 8): straight along lane 1 to col 7, then in.
  assert.deepEqual(rail(l, 'stpReview', 'testing'), [
    [3, 1],
    [7, 1],
    [8, 0],
  ]);
});

void test('a 3-way fork takes lanes 0/1/2 in authored order and joins back on lane 0', () => {
  const l = layoutLifecycleGraph(
    graph('normal>decD>decR>risk>sdp, normal>subD>subR>risk, normal>cmpD>cmpR>risk')
  );
  assert.equal(l.lanes, 3);
  assert.deepEqual(
    ['decD', 'subD', 'cmpD'].map((id) => at(l, id)),
    [0, 1, 2].map((lane) => ({ col: 1, lane }))
  );
  assert.deepEqual(at(l, 'risk'), { col: 3, lane: 0 });
  assert.deepEqual(at(l, 'sdp'), { col: 4, lane: 0 });
  assert.deepEqual(rail(l, 'cmpR', 'risk'), [
    [2, 2],
    [3, 0],
  ]);
});

void test('a lane freed by a join is reused by the next fork', () => {
  const l = layoutLifecycleGraph(graph('a>b>c>d>e, a>x>c, c>y>e'));
  assert.equal(l.lanes, 2);
  assert.equal(at(l, 'x').lane, 1);
  assert.equal(at(l, 'y').lane, 1);
});

void test('a nested fork returns to the BRANCH lane, not the trunk', () => {
  const l = layoutLifecycleGraph(graph('a>b>c>d>z, a>p>q>r>z, p>s>r'));
  assert.deepEqual(at(l, 'p'), { col: 1, lane: 1 });
  assert.deepEqual(at(l, 's'), { col: 2, lane: 2 });
  assert.deepEqual(at(l, 'r'), { col: 3, lane: 1 });
  assert.deepEqual(at(l, 'z'), { col: 4, lane: 0 });
});

void test('a redundant transitive edge detours on its own lane, never through a pip', () => {
  const l = layoutLifecycleGraph(graph('a>b>c, a>c'));
  assert.deepEqual(
    ['a', 'b', 'c'].map((id) => at(l, id).lane),
    [0, 0, 0]
  );
  assert.deepEqual(rail(l, 'a', 'c'), [
    [0, 0],
    [1, 1],
    [2, 0],
  ]);
});

void test('no rail runs through a pip it does not belong to', () => {
  for (const nodes of [FIGURE_A1, graph('a>b>c>d>e, a>x>c, c>y>e, a>e'), graph('a>b>c, a>c')]) {
    const l = layoutLifecycleGraph(nodes);
    for (const e of l.edges) {
      e.points.slice(1, -1).forEach((p) => {
        for (const [id, pos] of l.positions) {
          assert.ok(
            pos.col !== p.col || pos.lane !== p.lane,
            `${e.from}→${e.to} passes through ${id}`
          );
        }
      });
      // …and a straight run between two waypoints crosses no occupied cell either.
      e.points.slice(1).forEach((p, i) => {
        const prev = e.points[i];
        if (prev?.lane !== p.lane) return;
        for (const [id, pos] of l.positions) {
          const inside = pos.lane === p.lane && pos.col > prev.col && pos.col < p.col;
          assert.ok(!inside, `${e.from}→${e.to} runs over ${id}`);
        }
      });
    }
  }
});

void test('unknown, self and duplicate dependencies are ignored; a cycle does not hang', () => {
  const l = layoutLifecycleGraph([
    { id: 'a', dependsOn: ['ghost', 'a'] },
    { id: 'b', dependsOn: ['a', 'a', 'c'] },
    { id: 'c', dependsOn: ['b'] },
  ]);
  assert.deepEqual(at(l, 'a'), { col: 0, lane: 0 });
  assert.equal(l.edges.filter((e) => e.from === 'a' && e.to === 'b').length, 1);
  assert.equal(l.positions.size, 3);
});

void test('an empty graph lays out to nothing', () => {
  const l = layoutLifecycleGraph([]);
  assert.deepEqual([l.cols, l.lanes, l.edges.length], [0, 0, 0]);
});
