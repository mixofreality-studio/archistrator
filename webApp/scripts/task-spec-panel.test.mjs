/**
 * The task spec panel's pure rules (src/components/activity/taskSpecPanelLogic.ts),
 * over the real resolver's fixtures (preview/fixtures/web-client/task-spec/ and the
 * review answer in activity-experience/service-parallel-testplan.json), mapped
 * through the app's own `mapTaskSpec` — the panel never sees a raw wire value.
 *
 * Bundled with esbuild like task-spec.test.mjs: the webApp has no component-test
 * runner, so the real sources are bundled and imported.
 */
import { test, before } from 'node:test';
import assert from 'node:assert/strict';
import { mkdirSync, readdirSync, readFileSync } from 'node:fs';
import { join, dirname } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { build } from 'esbuild';

const root = join(dirname(fileURLToPath(import.meta.url)), '..');
let logic;
let wire;
let specHook;
let runHook;
let graph;
let rowKeys;
let anchors;

async function bundle(entry, out) {
  await build({
    entryPoints: [join(root, entry)],
    outfile: out,
    bundle: true,
    format: 'esm',
    platform: 'node',
    packages: 'external',
    jsx: 'automatic',
    // The hooks reach the file endpoint's URL builder, which reads Vite's env.
    define: { 'import.meta.env': '{}' },
    logLevel: 'error',
  });
  return import(pathToFileURL(out).href);
}

before(async () => {
  const dir = join(root, 'node_modules', '.cache', 'task-spec-panel-test');
  mkdirSync(dir, { recursive: true });
  logic = await bundle('src/components/activity/taskSpecPanelLogic.ts', join(dir, 'logic.mjs'));
  wire = await bundle('src/contracts/wire.ts', join(dir, 'wire.mjs'));
  specHook = await bundle('src/hooks/useTaskSpec.ts', join(dir, 'spec-hook.mjs'));
  runHook = await bundle('src/hooks/useTestRun.ts', join(dir, 'run-hook.mjs'));
  graph = await bundle('src/components/activity/activityViewToGraph.ts', join(dir, 'graph.mjs'));
  rowKeys = await bundle('src/components/comments/rowKeys.ts', join(dir, 'row-keys.mjs'));
  anchors = await bundle('src/components/comments/CommentContext.tsx', join(dir, 'anchors.mjs'));
});

const FIXTURES = join(root, 'preview', 'fixtures', 'web-client');

/** The fixture's taskSpec answer, mapped exactly as useTaskSpec maps it. */
function fixtureSpec(rel) {
  const f = JSON.parse(readFileSync(join(FIXTURES, rel), 'utf8'));
  return wire.mapTaskSpec(f.ops.deliveryQueryProjectView.taskSpec.result.taskSpec);
}

function check(id, kind, source = {}) {
  return { id, kind, title: id, source, timeoutSec: 60 };
}

test('checkRows: a spec with no checkRun is all not-run (R35)', () => {
  const spec = fixtureSpec('task-spec/construction.json');
  assert.equal(spec.checkRun, undefined);
  const rows = logic.checkRows(spec);
  assert.equal(rows.length, spec.checks.length);
  assert.ok(rows.length > 0);
  for (const r of rows) {
    assert.equal(r.state, 'not-run');
    assert.equal(r.result, undefined);
  }
  assert.deepEqual(logic.checkCounts(rows), {
    passed: 0,
    failed: 0,
    timedOut: 0,
    notRun: rows.length,
  });
  assert.equal(logic.countsLine(logic.checkCounts(rows)), '0 passed · 0 failed · 5 not run');
});

test('checkRows: mixed results join by check id; a timeout is counted apart', () => {
  const spec = fixtureSpec('task-spec/construction-run.json');
  const rows = logic.checkRows(spec);
  const byId = Object.fromEntries(rows.map((r) => [r.check.id, r.state]));
  assert.deepEqual(byId, {
    'command:arch': 'pass',
    'command:build': 'pass',
    'command:gofmt': 'fail',
    'command:lint': 'pass',
    'command:vet': 'timeout',
  });
  assert.equal(
    rows.find((r) => r.check.id === 'command:gofmt').result.outputRef,
    'command-gofmt.log'
  );
  const counts = logic.checkCounts(rows);
  assert.deepEqual(counts, { passed: 3, failed: 1, timedOut: 1, notRun: 0 });
  assert.equal(logic.countsLine(counts), '3 passed · 1 failed · 1 timed out · 0 not run');
  assert.equal(spec.specChanged, true);
});

test('checkRows: a check the run never reported stays not-run beside reported ones', () => {
  const spec = {
    ...fixtureSpec('task-spec/construction-run.json'),
    checks: [check('command:build', 'command'), check('rule:glossary', 'rule')],
  };
  const rows = logic.checkRows(spec);
  assert.deepEqual(
    rows.map((r) => r.state),
    ['pass', 'not-run']
  );
});

test('checkRows: an unknown kind and an unknown status survive, never as a pass (R45)', () => {
  const spec = wire.mapTaskSpec({
    ...JSON.parse(readFileSync(join(FIXTURES, 'task-spec/construction-run.json'), 'utf8')).ops
      .deliveryQueryProjectView.taskSpec.result.taskSpec,
    checks: [{ id: 'probe:x', kind: 'probe', title: 'Probe', source: {}, timeoutSec: 5 }],
    checkRun: {
      id: 'cr',
      activity: 'a',
      task: 't',
      revision: 'r',
      specHash: 'h',
      artifact: 'x',
      results: [{ checkId: 'probe:x', kind: 'probe', status: 'flaky', durationMs: 1 }],
    },
  });
  const [row] = logic.checkRows(spec);
  assert.equal(row.check.kind, 'unknown');
  assert.equal(row.state, 'unknown');
  assert.deepEqual(logic.checkCounts([row]), { passed: 0, failed: 0, timedOut: 0, notRun: 0 });
  assert.equal(logic.sourceLabel(row.check), 'probe:x');
});

test('groupByKind: rules, commands, scenarios, then Other; empty groups dropped', () => {
  const rows = [
    { check: check('x', 'unknown'), state: 'not-run' },
    { check: check('scenario:c:s1', 'scenario'), state: 'pass' },
    { check: check('rule:all', 'rule'), state: 'fail' },
    { check: check('scenario:c:s2', 'scenario'), state: 'not-run' },
  ];
  const groups = logic.groupByKind(rows);
  assert.deepEqual(
    groups.map((g) => [g.kind, logic.KIND_LABEL[g.kind], g.rows.map((r) => r.check.id)]),
    [
      ['rule', 'Rules', ['rule:all']],
      ['scenario', 'Scenarios', ['scenario:c:s1', 'scenario:c:s2']],
      ['unknown', 'Other', ['x']],
    ]
  );
  assert.deepEqual([...logic.KIND_ORDER], ['rule', 'command', 'scenario', 'unknown']);
  assert.deepEqual(logic.groupByKind([]), []);
});

test('formatUsd: below a cent, at a cent, and above', () => {
  assert.equal(logic.formatUsd(0), '<$0.01');
  assert.equal(logic.formatUsd(0.009), '<$0.01');
  assert.equal(logic.formatUsd(0.01), '$0.01');
  assert.equal(logic.formatUsd(81), '$81.00');
  assert.equal(logic.formatUsd(1.236), '$1.24');
});

test('formatTokens: plain below 1k, k from 1k, M from 1M', () => {
  assert.equal(logic.formatTokens(0), '0');
  assert.equal(logic.formatTokens(999), '999');
  assert.equal(logic.formatTokens(1000), '1k');
  assert.equal(logic.formatTokens(911), '911');
  assert.equal(logic.formatTokens(12_345), '12k');
  // The unit follows the ROUNDED value: nothing reads "1000k".
  assert.equal(logic.formatTokens(999_499), '999k');
  assert.equal(logic.formatTokens(999_500), '1.0M');
  assert.equal(logic.formatTokens(999_999), '1.0M');
  assert.equal(logic.formatTokens(1499), '1k');
  assert.equal(logic.formatTokens(1500), '2k');
  assert.equal(logic.formatTokens(1_000_000), '1.0M');
  assert.equal(logic.formatTokens(12_000_000), '12.0M');
});

test('sourceLabel: per kind', () => {
  assert.equal(
    logic.sourceLabel(check('rule:g', 'rule', { rulePrefixes: ['GLOSS-', 'REQ-'] })),
    'GLOSS-* REQ-*'
  );
  assert.equal(logic.sourceLabel(check('rule:all', 'rule')), 'all method rules');
  assert.equal(
    logic.sourceLabel(check('command:build', 'command', { argv: ['go', 'build', './...'] })),
    'go build ./...'
  );
  assert.equal(logic.sourceLabel(check('command:none', 'command')), '');
  assert.equal(
    logic.sourceLabel(
      check('scenario:billing:s1', 'scenario', { contract: 'billing', useCase: 'charge' })
    ),
    'billing · use case charge'
  );
  const spec = fixtureSpec('task-spec/construction.json');
  const gofmt = spec.checks.find((c) => c.id === 'command:gofmt');
  assert.match(logic.sourceLabel(gofmt), /^sh -c test -z/);
});

test('modelSourceTip: charter, project override, unknown', () => {
  assert.equal(logic.modelSourceTip('charter'), 'from charter');
  assert.equal(logic.modelSourceTip('projectOverride'), 'project override');
  assert.equal(logic.modelSourceTip('unknown'), 'source unknown');
  // The real review answer carries an empty source, which maps to 'unknown'.
  const review = fixtureSpec('activity-experience/service-parallel-testplan.json');
  assert.equal(review.model.source, 'unknown');
  assert.equal(review.identity.command, '');
  assert.deepEqual(logic.checkRows(review), []);
});

test('scenarioViewOf: the scenario id from the source, absent refs as empty', () => {
  const c = check('scenario:billing:s1', 'scenario', { contract: 'billing', scenario: 's1' });
  const v = logic.scenarioViewOf(c, {
    checkId: c.id,
    kind: 'scenario',
    status: 'fail',
    durationMs: 12,
    outputRef: 'out.jsonl',
  });
  assert.deepEqual(v, {
    id: 's1',
    title: c.id,
    guards: [],
    status: 'fail',
    durationMs: 12,
    videoRef: '',
    traceRef: '',
    outputRef: 'out.jsonl',
  });
});

test('checkOutputRefsOf: the run logs, distinct, none without a run', () => {
  assert.deepEqual(specHook.checkOutputRefsOf(fixtureSpec('task-spec/construction-run.json')), [
    'command-gofmt.log',
    'command-vet.log',
  ]);
  assert.deepEqual(specHook.checkOutputRefsOf(fixtureSpec('task-spec/construction.json')), []);
  assert.equal(
    specHook.checkRunFileUrl('p', fixtureSpec('task-spec/construction.json'))('a.log'),
    ''
  );
});

test('outputRefsOf: covers every judged contract (R36)', () => {
  const member = (component, refs) => ({
    run: { id: 'r1', component },
    scenarios: refs.map((outputRef, i) => ({ id: `${component}-${String(i)}`, outputRef })),
  });
  const primary = member('billing', ['a.jsonl']);
  const panel = { ...primary, testRuns: [primary, member('ledger', ['b.jsonl', 'a.jsonl', ''])] };
  assert.deepEqual(runHook.outputRefsOf(panel), ['a.jsonl', 'b.jsonl']);
});

test('specSections: a no-command review shows role (if any), exit criterion and checks only', () => {
  const review = fixtureSpec('activity-experience/service-parallel-testplan.json');
  assert.deepEqual(logic.specSections(review), {
    role: false,
    command: false,
    model: false,
    cost: false,
    exitCriterion: true,
    prompt: false,
    tools: false,
    editScope: false,
  });
  const withRole = { ...review, identity: { ...review.identity, workerClass: 'system-architect' } };
  assert.equal(logic.specSections(withRole).role, true);
  assert.equal(logic.specSections(withRole).editScope, false);
  const construction = fixtureSpec('task-spec/construction.json');
  assert.deepEqual(logic.specSections(construction), {
    role: true,
    command: true,
    model: true,
    cost: true,
    exitCriterion: true,
    prompt: true,
    tools: true,
    editScope: true,
  });
});

test('anchors: the persisted comment addresses of a spec check and a run scenario', () => {
  assert.equal(
    anchors.taskSpecCheckAnchor('C-billing-engine', 'construction', 'command:gofmt'),
    '$.testingState.checkRuns[activity=C-billing-engine][task=construction].results[checkId=command:gofmt]'
  );
  assert.equal(
    anchors.testRunScenarioAnchor('cr-1', 'billingStateAccess', 'bill-P1'),
    '$.testingState.checkRuns[id=cr-1].results[checkId=scenario:billingStateAccess:bill-P1]'
  );
});

/**
 * R47: the gate verbs read the spec's artifact kind once loaded and the generated
 * table's while it is not, so the two must agree. Every resolver-generated
 * fixture answer is checked against artifactKindOf for the same lifecycle task.
 */
test('artifactKindOf agrees with the resolver on every fixture (R47)', () => {
  const dir = join(FIXTURES, 'activity-experience');
  let checked = 0;
  for (const f of readdirSync(dir).filter((n) => n.endsWith('.json'))) {
    const fx = JSON.parse(readFileSync(join(dir, f), 'utf8'));
    const answer = fx.ops.deliveryQueryProjectView?.taskSpec;
    assert.ok(answer !== undefined, `${f} has no taskSpec answer`);
    if (answer.result === undefined) continue; // a refused resolve (an activity no longer planned)
    const spec = answer.result.taskSpec;
    const view = fx.ops.deliveryQueryActivityView.result;
    assert.equal(
      graph.lifecycleArtifactKindOf(view.type, view.variant, spec.task),
      spec.identity.artifactKind,
      `${f}: ${view.type}/${spec.task}`
    );
    checked += 1;
  }
  for (const f of ['construction.json', 'construction-run.json']) {
    const spec = fixtureSpec(`task-spec/${f}`);
    assert.equal(
      graph.lifecycleArtifactKindOf('service', undefined, spec.task),
      spec.identity.artifactKind
    );
    checked += 1;
  }
  assert.ok(checked >= 15, `checked ${String(checked)}`);
});

test("keyBelongsToNestedControl: Enter/Space on a control inside the row is the control's (R48)", () => {
  const row = { tagName: 'DIV', parentElement: null, getAttribute: () => null };
  const inner = { tagName: 'DIV', parentElement: row, getAttribute: () => null };
  const button = { tagName: 'BUTTON', parentElement: inner, getAttribute: () => null };
  const span = { tagName: 'SPAN', parentElement: button, getAttribute: () => null };
  const roleButton = {
    tagName: 'DIV',
    parentElement: row,
    getAttribute: (n) => (n === 'role' ? 'button' : null),
  };
  const text = { tagName: 'P', parentElement: inner, getAttribute: () => null };
  assert.equal(rowKeys.keyBelongsToNestedControl('Enter', button, row), true);
  assert.equal(rowKeys.keyBelongsToNestedControl(' ', span, row), true);
  assert.equal(rowKeys.keyBelongsToNestedControl('c', roleButton, row), true);
  assert.equal(rowKeys.keyBelongsToNestedControl('Enter', row, row), false);
  assert.equal(rowKeys.keyBelongsToNestedControl('Enter', text, row), false);
  assert.equal(rowKeys.keyBelongsToNestedControl('ArrowDown', button, row), false);
  // The walk stops at the row: an interactive ancestor OUTSIDE the row does not count.
  const outer = { tagName: 'BUTTON', parentElement: null, getAttribute: () => null };
  const rowInButton = { ...row, parentElement: outer };
  const t2 = { tagName: 'P', parentElement: rowInButton, getAttribute: () => null };
  assert.equal(rowKeys.keyBelongsToNestedControl('Enter', t2, rowInButton), false);
});

test("taskSpecRefreshDue: re-read once when the same selection's episode turns terminal (R48)", () => {
  const sig = (sel, running, rec) => specHook.episodeSignal(sel, running, rec);
  const ep = { episodeId: 'e1', outcome: 'succeeded' };
  assert.equal(specHook.taskSpecRefreshDue(undefined, sig('construction/1', false, ep)), false);
  assert.equal(
    specHook.taskSpecRefreshDue(sig('construction/1', true), sig('construction/1', false, ep)),
    true
  );
  assert.equal(
    specHook.taskSpecRefreshDue(sig('construction/1', true), sig('construction/1', false)),
    false
  );
  assert.equal(
    specHook.taskSpecRefreshDue(sig('construction/1', false), sig('construction/1', false, ep)),
    true
  );
  assert.equal(
    specHook.taskSpecRefreshDue(sig('construction/1', false, ep), sig('construction/1', false, ep)),
    false
  );
  assert.equal(
    specHook.taskSpecRefreshDue(sig('construction/1', false, ep), sig('codeReview/1', false, ep)),
    false
  );
  assert.equal(
    specHook.taskSpecRefreshDue(
      sig('construction/1', false, ep),
      sig('construction/1', false, { episodeId: 'e2', outcome: 'failed' })
    ),
    true
  );
});
