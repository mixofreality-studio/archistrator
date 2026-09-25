/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  miniLifecycleFromRow,
  miniLifecycleLabel,
  type MiniLifecycleInput,
} from './miniLifecycleFromRow.ts';
import type { ActivityBuildStatusRow, PhaseRow } from '../../contracts/types.ts';

const SERVICE_PHASES = [
  'requirements',
  'detailed_design',
  'test_plan',
  'construction',
  'integration',
];
const phases = (done: readonly string[]): PhaseRow[] =>
  SERVICE_PHASES.map((phase) => ({
    phase,
    weight: 20,
    label: phase,
    completed: done.includes(phase),
  }));

const row = (over: Partial<MiniLifecycleInput>): MiniLifecycleInput => ({
  kind: 'service',
  phases: phases([]),
  ...over,
});

void test('a finished service activity is all done, and its label counts its ten tasks', () => {
  const nodes = miniLifecycleFromRow(row({ phases: phases(SERVICE_PHASES), status: 'integrated' }));
  assert.equal(nodes.length, 10, 'the service lifecycle has ten tasks');
  assert.ok(nodes.every((n) => n.state === 'done'));
  assert.equal(miniLifecycleLabel(nodes), 'Lifecycle: 10 of 10 tasks done');
});

void test('the current phase runs, everything before it is done, everything after is pending', () => {
  const nodes = miniLifecycleFromRow(
    row({
      phases: phases(['requirements', 'detailed_design', 'test_plan']),
      currentLifecyclePhase: 'construction',
      status: 'in-construction',
    })
  );
  const state = (id: string): string => nodes.find((n) => n.id === id)?.state ?? 'MISSING';
  assert.equal(state('srs'), 'done');
  assert.equal(state('designReview'), 'done');
  assert.equal(state('construction'), 'running');
  assert.equal(state('codeReview'), 'running');
  assert.equal(state('integration'), 'pending');
  assert.equal(state('testing'), 'pending');
});

void test('a genuinely not-started row (classified, no status, no current phase, no phases at all) renders every task pending', () => {
  // The realistic shape of a PLANNED-NO-RECORD activity (ConstructionRow's own
  // `recorded === false`): classified — `kind` is present, so a real lifecycle
  // resolves — but nothing else has ever been recorded: no phases, no current
  // phase, no status. Every one of its ten tasks must read `pending`, not fall
  // through to `running` by accident (only the CURRENT phase's tasks run, and
  // there is no current phase here to match).
  const nodes = miniLifecycleFromRow({ kind: 'service', phases: [] });
  assert.equal(nodes.length, 10, 'the service lifecycle still has ten tasks');
  for (const n of nodes) {
    assert.equal(n.state, 'pending', n.id);
  }
});

void test('in-review is the awaiting-a-human state; failed is failed; both leave finished phases done', () => {
  // `status` is typed as the plain union, not `MiniLifecycleInput['status']`:
  // indexed access on an optional member always widens to include `undefined`
  // (independent of, and not caught by, `exactOptionalPropertyTypes`), and
  // this helper is never called with one.
  const at = (status: ActivityBuildStatusRow): string =>
    miniLifecycleFromRow(
      row({ phases: phases(['requirements']), currentLifecyclePhase: 'detailed_design', status })
    ).find((n) => n.id === 'designReview')?.state ?? 'MISSING';
  assert.equal(at('in-review'), 'awaitingHuman');
  assert.equal(at('failed'), 'failed');
  assert.equal(at('in-construction'), 'running');
  const done = miniLifecycleFromRow(
    row({
      phases: phases(['requirements']),
      currentLifecyclePhase: 'detailed_design',
      status: 'failed',
    })
  ).find((n) => n.id === 'srs');
  assert.equal(done?.state, 'done', 'a failure does not un-finish what finished');
});

void test('a testing row takes its VARIANT lifecycle, not the bare type', () => {
  assert.equal(miniLifecycleFromRow(row({ kind: 'testing', variant: 'plan' })).length, 6);
  assert.equal(miniLifecycleFromRow(row({ kind: 'testing', variant: 'qaProcess' })).length, 4);
});

void test('an unclassified row gets no spine at all rather than a guessed one', () => {
  // `kind` is OMITTED, not set to `undefined`: `exactOptionalPropertyTypes`
  // means a `MiniLifecycleInput` value can never carry an explicit `kind:
  // undefined` (the member's own type has no `| undefined`) — omitting the
  // key is the only way to express "unclassified", and it is what the real
  // wire decoder does too (mapConstructionRow never writes the key at all).
  assert.deepEqual(miniLifecycleFromRow({ phases: phases([]) }), []);
});

void test('EVERY produced node carries zero revisions — the invariant this module exists to hold', () => {
  // The project read carries no per-task revision history. A fabricated
  // revision would let a mini graph claim a round that never happened;
  // opening the activity is what fetches real ones (QueryActivityView).
  for (const kind of [
    'service',
    'frontend',
    'deployment',
    'documentation',
    'uiDesign',
    'integration',
  ] as const) {
    for (const n of miniLifecycleFromRow(row({ kind, phases: phases(SERVICE_PHASES) }))) {
      assert.equal(n.revisions.length, 0, `${kind}/${n.id}`);
    }
  }
});
