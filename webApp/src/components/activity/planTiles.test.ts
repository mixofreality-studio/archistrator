/**
 * The plan's activities and tiles (planTiles.ts), exercised against the LIVE
 * captured plan fixture — the repo's own 32-activity plan, decoded exactly as
 * `useProject` decodes it — so these are assertions about real data, not about
 * a hand-written shape that agrees with the code by construction.
 *
 * The fixture pre-dates deterministic component testing (spec §7): it still
 * carries N-STP and N-IT, whose variants (plan, systemTest) no longer exist. They
 * decode as testing rows WITHOUT a variant now, which is exactly the "no band"
 * case — so the fixture's two retired activities are what these tests use to pin
 * that an unplaceable activity is listed last and named, never dropped.
 */
/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import {
  LIST_ROWS,
  MILESTONE_ID,
  PLAN_STATE_LABEL,
  planActivitiesFrom,
  planStateFor,
  planTilesFrom,
} from './planTiles.ts';
import { mapConstructionRow } from '../../contracts/wire.ts';
import type { LifecycleNode, LifecycleNodeState } from './lifecycleGraphTypes.ts';

// The fixture is the wire payload; decode it exactly as useProject does, so the
// test exercises the same ConstructionRow the screen gets.
function live(): ReturnType<typeof planActivitiesFrom> {
  const url = new URL(
    '../../../../uitests/preview-fixtures/web-client/plan/list.json',
    import.meta.url
  );
  // The head-state read is one kind of the merged project view (stage 4a), so the
  // fixture keys it by that kind and the body is the view's `summary` member —
  // exactly what useProject unwraps.
  const doc = JSON.parse(readFileSync(url, 'utf8')) as {
    ops: { deliveryQueryProjectView: { summary: { result: { summary: Record<string, never> } } } };
  };
  const project = doc.ops.deliveryQueryProjectView.summary.result.summary as unknown as {
    activityExecution: Record<string, unknown>;
    Slots: { kind: string; model: unknown }[];
  };
  const rows = Object.fromEntries(
    Object.entries(project.activityExecution).map(([id, raw]) => [
      id,
      mapConstructionRow(raw as never),
    ])
  );
  const slot = (kind: string): unknown => project.Slots.find((s) => s.kind === kind)?.model;
  return planActivitiesFrom({
    activityList: slot('activityList') as never,
    network: slot('network') as never,
    rows,
  });
}

void test('the list opens with the three design activities in Table 11-1 order, then M0', () => {
  const ids = live().map((a) => a.id);
  assert.deepEqual(ids.slice(0, 3), ['requirements', 'architecture', 'projectDesign']);
  assert.ok(
    ids.indexOf('R-github') > ids.indexOf('projectDesign'),
    'the build stack follows the front end'
  );
  // The two retired activities have no band, so they are listed LAST (planTiles
  // lists what it cannot place after every row), in the committed list's order.
  assert.deepEqual(ids.slice(-2), ['N-IT', 'N-STP'], 'unplaceable activities close the list');
});

void test('every activity of the committed list appears, placed or not', () => {
  const activities = live();
  assert.equal(activities.length, 32, 'the repo’s own plan has 32 activities');
  const { tiles, unplaced } = planTilesFrom(activities);
  assert.equal(
    tiles.length + unplaced.length,
    activities.length,
    'nothing is dropped on the floor'
  );
});

void test('M0 gates the build roots and nothing in the front end', () => {
  const byId = new Map(live().map((a) => [a.id, a]));
  assert.deepEqual(byId.get('R-github')?.calls, [MILESTONE_ID]);
  assert.deepEqual(byId.get('N-STP')?.calls, [MILESTONE_ID]);
  assert.deepEqual(byId.get('requirements')?.calls, [], 'the first activity depends on nothing');
  assert.deepEqual(byId.get('architecture')?.calls, ['requirements']);
  assert.deepEqual(byId.get('projectDesign')?.calls, ['architecture']);
});

void test('an activity with no build-order row is reported as unplaced, not dropped', () => {
  const activities = [
    ...live(),
    {
      id: 'X-orphan',
      title: 'Orphan',
      row: undefined,
      calls: [],
      lifecycle: [],
      onCriticalPath: false,
      effortDays: 5,
    },
  ];
  const { tiles, unplaced } = planTilesFrom(activities);
  assert.ok(!tiles.some((t) => t.id === 'X-orphan'));
  // The fixture's retired N-STP / N-IT are unplaced the same way (see the header).
  assert.deepEqual(unplaced, ['N-IT', 'N-STP', 'X-orphan']);
});

void test('every row carries a mini lifecycle, and none of them claims a revision', () => {
  for (const a of live()) {
    for (const n of a.lifecycle) assert.equal(n.revisions.length, 0, `${a.id}/${n.id}`);
  }
});

void test('the build stack runs in BUILD order, resources first and clients last', () => {
  const activities = live();
  const rowAt = (id: string): number =>
    LIST_ROWS.indexOf(activities.find((a) => a.id === id)?.row ?? 'frontEnd');
  // Resources are built first, clients last. (N-STP's side lane and N-IT's
  // system-testing row are retired with those activities, spec §7.)
  assert.ok(
    rowAt('R-github') < rowAt('C-project-state-access'),
    'resources precede resource access'
  );
  assert.ok(
    rowAt('C-project-state-access') < rowAt('C-review-engine'),
    'resource access precedes engines'
  );
  assert.ok(rowAt('C-review-engine') < rowAt('C-operations-manager'), 'engines precede managers');
  assert.ok(rowAt('C-operations-manager') < rowAt('U-SPA-web-client'), 'managers precede clients');
  assert.equal(activities.find((a) => a.id === 'N-STP')?.row, undefined, 'N-STP has no band');
  assert.equal(activities.find((a) => a.id === 'N-IT')?.row, undefined, 'N-IT has no band');
});

void test('the critical path comes from the committed network, not from a guess', () => {
  const activities = live();
  assert.equal(
    activities.filter((a) => a.onCriticalPath).length,
    15,
    'the committed network names 15 critical activities'
  );
  assert.equal(activities.find((a) => a.id === 'requirements')?.onCriticalPath, true);
  assert.equal(activities.find((a) => a.id === 'C-billing-engine')?.onCriticalPath, false);
});

void test('a title and an effort come from the committed activity list', () => {
  const a = live().find((x) => x.id === 'requirements');
  assert.ok(a !== undefined, 'the committed list carries the Requirements activity');
  assert.equal(a.title, 'Requirements');
  assert.equal(a.effortDays, 5);
});

void test('no committed list means no activities, and no invented ones', () => {
  assert.deepEqual(
    planActivitiesFrom({ activityList: undefined, network: undefined, rows: {} }),
    []
  );
});

const node = (state: LifecycleNodeState): LifecycleNode => ({
  id: `n-${state}`,
  kind: 'dispatch',
  title: state,
  phase: 'construction',
  state,
  dependsOn: [],
  revisions: [],
});

void test('the plan state is the worst thing the lifecycle says', () => {
  assert.equal(planStateFor([]), 'unknown');
  assert.equal(planStateFor([node('pending'), node('pending')]), 'notStarted');
  assert.equal(planStateFor([node('done'), node('pending')]), 'running');
  assert.equal(planStateFor([node('done'), node('running')]), 'running');
  assert.equal(planStateFor([node('done'), node('awaitingHuman'), node('running')]), 'awaitingYou');
  assert.equal(planStateFor([node('sentBack'), node('pending')]), 'awaitingYou');
  assert.equal(planStateFor([node('failed'), node('awaitingHuman')]), 'failed');
  assert.equal(planStateFor([node('done'), node('done')]), 'done');
});

void test('every plan state says a word, so colour is never the only carrier', () => {
  for (const [state, label] of Object.entries(PLAN_STATE_LABEL)) {
    assert.ok(label.length > 0, state);
  }
});

void test('the live plan reports the states the captured read really carries', () => {
  const states = new Map(live().map((a) => [a.id, planStateFor(a.lifecycle)]));
  assert.equal(states.get('requirements'), 'done', 'an integrated activity is done');
  assert.equal(states.get('C-billing-manager'), 'awaitingYou', 'a row in review awaits a human');
  assert.equal(
    states.get('R-merchant-gateway'),
    'notStarted',
    'a row with no build evidence has not started'
  );
});
