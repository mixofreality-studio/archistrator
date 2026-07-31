/// <reference types="node" />
/**
 * Unit tests for the pure per-use-case realization join (src/contracts/realization.ts):
 *
 *  - realizationByNode indexes one use case's realized DynamicView steps by
 *    activityNodeId (no graph walk — see linearizeSteps below for that).
 *  - personParticipants filters a use case's actors down to the ones that show
 *    up as a call endpoint in its realized view, in ACTOR-list order.
 *  - linearizeSteps is the DFS linearization adapters.ts' toDynamicView consumes
 *    verbatim (Tasks 9-11) — the most complex logic in this codec, so it gets
 *    its own dedicated fixtures below (fork/join/back-edge, entry ordering,
 *    dangling steps, empty input).
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import type {
  ActivityDiagram,
  ActivityEdge,
  ActivityNode,
  ActivityNodeKind,
  System,
  UseCase,
} from './types';
import { realizationByNode, personParticipants, linearizeSteps } from './realization.ts';

const SYSTEM: System = {
  components: null,
  relationships: null,
  dynamicViews: [
    {
      key: 'dv-order',
      title: 'Place Order',
      useCaseId: 'uc-1',
      steps: [
        {
          activityNodeId: 'n-validate',
          calls: [{ from: 'webClient', to: 'orderManager', mode: 'sync', label: 'submit order' }],
        },
        {
          activityNodeId: 'n-charge',
          calls: [
            { from: 'orderManager', to: 'billingEngine', mode: 'sync', label: 'charge card' },
            {
              from: 'billingEngine',
              to: 'customer',
              mode: 'eventPubSub',
              label: 'notify customer',
            },
          ],
        },
        { activityNodeId: 'n-empty', calls: [] },
      ],
    },
    // A view with a blank key and null steps — realizationByNode must tolerate
    // null steps, and useCaseId 'uc-2' has no calls to derive persons from.
    { key: '', title: 'Broken', useCaseId: 'uc-2', steps: null },
  ],
};

function useCase(id: string, actors: { id: string; role: string }[]): UseCase {
  return {
    id,
    name: 'Place Order',
    actors,
    activity: null,
    classification: 'core',
    trigger: 'clientAction',
    variationOf: null,
  };
}

// ── realizationByNode ────────────────────────────────────────────────────────

void test('indexes each step by its activityNodeId, mapping Relationship -> RealizedCall', () => {
  const map = realizationByNode(SYSTEM, 'uc-1');
  assert.deepEqual([...map.keys()], ['n-validate', 'n-charge', 'n-empty']);
  assert.deepEqual(map.get('n-validate')?.calls, [
    { from: 'webClient', to: 'orderManager', mode: 'sync', label: 'submit order' },
  ]);
  assert.equal(map.get('n-charge')?.calls.length, 2);
  assert.equal(map.get('n-charge')?.nodeId, 'n-charge');
  assert.deepEqual(map.get('n-empty')?.calls, []);
});

void test('a use case with a null-steps view yields an empty map', () => {
  assert.equal(realizationByNode(SYSTEM, 'uc-2').size, 0);
});

void test('an unlinked use case id yields an empty map', () => {
  assert.equal(realizationByNode(SYSTEM, 'uc-none').size, 0);
});

void test('an absent system or a blank use case id yields an empty map', () => {
  assert.equal(realizationByNode(undefined, 'uc-1').size, 0);
  assert.equal(realizationByNode(SYSTEM, '  ').size, 0);
});

// ── personParticipants ───────────────────────────────────────────────────────

void test('keeps only actors that appear as a call endpoint, in actor-list order', () => {
  const uc = useCase('uc-1', [
    { id: 'auditor', role: 'Auditor' },
    { id: 'customer', role: 'Customer' },
  ]);
  assert.deepEqual(personParticipants(SYSTEM, uc), [{ id: 'customer', role: 'Customer' }]);
});

void test('an actor appearing as a `from` endpoint also counts', () => {
  const uc = useCase('uc-1', [{ id: 'webClient', role: 'Shopper' }]);
  assert.deepEqual(personParticipants(SYSTEM, uc), [{ id: 'webClient', role: 'Shopper' }]);
});

void test('no actors reach an endpoint -> empty', () => {
  const uc = useCase('uc-1', [{ id: 'auditor', role: 'Auditor' }]);
  assert.deepEqual(personParticipants(SYSTEM, uc), []);
});

void test('a use case with no linked dynamic view -> empty', () => {
  const uc = useCase('uc-none', [{ id: 'webClient', role: 'Shopper' }]);
  assert.deepEqual(personParticipants(SYSTEM, uc), []);
});

void test('an absent system or use case -> empty', () => {
  assert.deepEqual(personParticipants(undefined, useCase('uc-1', [])), []);
  assert.deepEqual(personParticipants(SYSTEM, undefined), []);
});

// ── linearizeSteps ───────────────────────────────────────────────────────────

/** Node-shape shorthand: id + kind + label, with the boilerplate fields filled. */
function node(id: string, kind: ActivityNodeKind, label: string): ActivityNode {
  return { id, kind, label, linkedActorId: null, roleName: '' };
}

/** Edge-shape shorthand: plain control-flow unless a guard is given. */
function edge(from: string, to: string, guard = ''): ActivityEdge {
  return { from, to, kind: guard.length > 0 ? 'guardedFlow' : 'controlFlow', guard };
}

// A branchy diagram: start -> fork -> {a1, b1} -> join -> decision -> {back to
// a1 (loop), or end}. `orphan` sits in the diagram with no incoming edge from
// any entry (off-path, never visited); `ghost` is a CallStep whose
// activityNodeId isn't in the diagram's nodes at all (dangling reference).
const BRANCHY_ACTIVITY: ActivityDiagram = {
  nodes: [
    node('s0', 'start', 'Start'),
    node('f1', 'fork', 'Fork'),
    node('a1', 'action', 'Branch A'),
    node('b1', 'action', 'Branch B'),
    node('j1', 'join', 'Join'),
    node('d1', 'decision', 'Check'),
    node('e1', 'end', 'End'),
    node('orphan', 'action', 'Orphan'),
  ],
  edges: [
    edge('s0', 'f1'),
    edge('f1', 'a1'),
    edge('f1', 'b1'),
    edge('a1', 'j1'),
    edge('b1', 'j1'),
    edge('j1', 'd1'),
    edge('d1', 'a1', 'retry'), // back-edge (loop)
    edge('d1', 'e1', 'done'),
  ],
};

// Authored step order deliberately does NOT match graph/visit order, so the
// "never-visited steps appended in AUTHORED order" behavior is distinguishable
// from "appended in graph order". `orphan` and `ghost` are never visited.
const BRANCHY_STEPS: NonNullable<System['dynamicViews']>[number]['steps'] = [
  {
    activityNodeId: 'a1',
    calls: [{ from: 'webClient', to: 'serviceA', mode: 'sync', label: 'callA' }],
  },
  {
    activityNodeId: 'b1',
    calls: [{ from: 'webClient', to: 'serviceB', mode: 'sync', label: 'callB' }],
  },
  {
    activityNodeId: 'j1',
    calls: [{ from: 'serviceA', to: 'joinSvc', mode: 'sync', label: 'joinCall' }],
  },
  {
    activityNodeId: 'd1',
    calls: [
      { from: 'joinSvc', to: 'checkSvc', mode: 'sync', label: 'check1' },
      { from: 'checkSvc', to: 'checkSvc2', mode: 'queued', label: 'check2' },
    ],
  },
  {
    activityNodeId: 'orphan',
    calls: [{ from: 'webClient', to: 'orphanSvc', mode: 'sync', label: 'orphanCall' }],
  },
  {
    activityNodeId: 'ghost',
    calls: [{ from: 'webClient', to: 'ghostSvc', mode: 'sync', label: 'ghostCall' }],
  },
  {
    activityNodeId: 'e1',
    calls: [{ from: 'checkSvc2', to: 'doneSvc', mode: 'eventPubSub', label: 'finish' }],
  },
];

void test('(a)/(c)/(d) fork+join+back-edge: DFS order, seq-tagging fields, one emission per node', () => {
  const out = linearizeSteps(BRANCHY_STEPS, BRANCHY_ACTIVITY);

  // Full expected linearization: DFS visits s0,f1,a1,j1,d1,e1,b1 (f1's first
  // branch — a1 — is fully explored, including reaching e1 via j1/d1, before
  // f1's second branch b1 is tried at all); s0/f1 carry no step. Then the two
  // never-visited steps (orphan, ghost) append in their AUTHORED order.
  assert.deepEqual(
    out.map((c) => c.label),
    ['callA', 'joinCall', 'check1', 'check2', 'finish', 'callB', 'orphanCall', 'ghostCall']
  );

  // (c) the d1->a1 back-edge does not re-traverse into a1 a second time: callA
  // appears exactly once, not twice.
  assert.equal(out.filter((c) => c.label === 'callA').length, 1);

  // (d) j1 is reachable via TWO edges (a1->j1 and b1->j1) but its step is
  // authored once and emitted exactly once, on first visit (via the a1 branch).
  assert.equal(out.filter((c) => c.label === 'joinCall').length, 1);

  // Per-call tagging: d1's step carries two calls -> callInStep/callsInStep tag
  // both, sharing stepNodeId/stepLabel.
  const check1 = out.find((c) => c.label === 'check1');
  const check2 = out.find((c) => c.label === 'check2');
  assert.deepEqual(check1, {
    from: 'joinSvc',
    to: 'checkSvc',
    mode: 'sync',
    label: 'check1',
    stepNodeId: 'd1',
    stepLabel: 'Check',
    callInStep: 1,
    callsInStep: 2,
  });
  assert.deepEqual(check2, {
    from: 'checkSvc',
    to: 'checkSvc2',
    mode: 'queued',
    label: 'check2',
    stepNodeId: 'd1',
    stepLabel: 'Check',
    callInStep: 2,
    callsInStep: 2,
  });

  // A single-call step tags callInStep=1/callsInStep=1, stepLabel from the node.
  assert.deepEqual(
    out.find((c) => c.label === 'callB'),
    {
      from: 'webClient',
      to: 'serviceB',
      mode: 'sync',
      label: 'callB',
      stepNodeId: 'b1',
      stepLabel: 'Branch B',
      callInStep: 1,
      callsInStep: 1,
    }
  );
});

void test('(e) never-visited steps (off-path + dangling) append last, in AUTHORED order', () => {
  const out = linearizeSteps(BRANCHY_STEPS, BRANCHY_ACTIVITY);
  const tail = out.slice(-2);
  assert.deepEqual(
    tail.map((c) => ({ label: c.label, stepNodeId: c.stepNodeId, stepLabel: c.stepLabel })),
    [
      // orphan sits IN the diagram (has a label) but no entry reaches it.
      { label: 'orphanCall', stepNodeId: 'orphan', stepLabel: 'Orphan' },
      // ghost's activityNodeId isn't in the diagram's nodes at all — stepLabel
      // degrades to the node id rather than crashing or dropping the call.
      { label: 'ghostCall', stepNodeId: 'ghost', stepLabel: 'ghost' },
    ]
  );
});

void test('(b) entries walk in diagram-declared order: a start node and an edge-less event node', () => {
  // 'event1' is declared BEFORE 'start1' in `nodes` and has NO outgoing edges —
  // it must still be walked as its own entry (start ∪ event nodes), and in
  // nodes-array order relative to the other entry.
  const activity: ActivityDiagram = {
    nodes: [
      node('event1', 'acceptEvent', 'On Signal'),
      node('start1', 'start', 'Start'),
      node('mid', 'action', 'Mid'),
    ],
    edges: [edge('start1', 'mid')],
  };
  const steps: NonNullable<System['dynamicViews']>[number]['steps'] = [
    {
      activityNodeId: 'mid',
      calls: [{ from: 'webClient', to: 'midSvc', mode: 'sync', label: 'midCall' }],
    },
    {
      activityNodeId: 'event1',
      calls: [{ from: 'scheduler', to: 'eventSvc', mode: 'eventPubSub', label: 'eventCall' }],
    },
  ];
  const out = linearizeSteps(steps, activity);
  // event1 (first entry, no edges) walks to completion before start1 (second
  // entry) is tried, so event1's call is emitted before start1's descendant mid.
  assert.deepEqual(
    out.map((c) => c.label),
    ['eventCall', 'midCall']
  );
});

void test('(f) empty/absent steps or activity: no crash, no output', () => {
  assert.deepEqual(linearizeSteps(null, null), []);
  assert.deepEqual(linearizeSteps([], null), []);
  assert.deepEqual(linearizeSteps(null, BRANCHY_ACTIVITY), []);
});

void test('(f) an absent/null activity diagram still linearizes every step, in authored order', () => {
  const steps: NonNullable<System['dynamicViews']>[number]['steps'] = [
    { activityNodeId: 'x1', calls: [{ from: 'a', to: 'b', mode: 'sync', label: 'one' }] },
    { activityNodeId: 'x2', calls: [{ from: 'b', to: 'c', mode: 'sync', label: 'two' }] },
  ];
  const out = linearizeSteps(steps, undefined);
  assert.deepEqual(
    out.map((c) => ({ label: c.label, stepNodeId: c.stepNodeId, stepLabel: c.stepLabel })),
    [
      // stepLabel falls back to the node id — there is no diagram to resolve it.
      { label: 'one', stepNodeId: 'x1', stepLabel: 'x1' },
      { label: 'two', stepNodeId: 'x2', stepLabel: 'x2' },
    ]
  );
});
