/**
 * The taskSpec read: `mapTaskSpec` (contracts/wire.ts) and `useTaskSpec`
 * (hooks/useTaskSpec.ts), exercised without a DOM.
 *
 * The webApp has no component-test runner, so the real sources are bundled with
 * esbuild (a vite dependency) and imported. The hook is read through
 * `taskSpecQueryOptions`, the exact options object `useTaskSpec` hands useQuery,
 * and run on a QueryClient — key shape, enabled gate and the empty_body refusal
 * are the hook's whole behaviour.
 */
import { test, before } from 'node:test';
import assert from 'node:assert/strict';
import { mkdirSync } from 'node:fs';
import { join, dirname } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { build } from 'esbuild';
import { QueryClient } from '@tanstack/react-query';

const root = join(dirname(fileURLToPath(import.meta.url)), '..');
let wire;
let hook;

async function bundle(entry, out) {
  await build({
    entryPoints: [join(root, entry)],
    outfile: out,
    bundle: true,
    format: 'esm',
    platform: 'node',
    packages: 'external',
    jsx: 'automatic',
    logLevel: 'error',
  });
  return import(pathToFileURL(out).href);
}

before(async () => {
  // Inside the package so the bundles resolve react-query from node_modules.
  const dir = join(root, 'node_modules', '.cache', 'task-spec-test');
  mkdirSync(dir, { recursive: true });
  wire = await bundle('src/contracts/wire.ts', join(dir, 'wire.mjs'));
  hook = await bundle('src/hooks/useTaskSpec.ts', join(dir, 'hook.mjs'));
});

/** A wire TaskSpecView the way the Go server emits it: nil slices are null. */
function wireSpec(over = {}) {
  return {
    activity: 'C-billing',
    task: 'construction',
    identity: {
      kind: 'construction',
      title: 'Construct',
      phase: 'construction',
      command: 'service-construction',
      agent: 'junior-developer',
      workerClass: 'junior',
      artifactKind: 'code',
      exitCriterion: 'checks green',
      mode: 'construction',
    },
    model: { id: 'sonnet', source: 'charter', estimate: { tokensIn: 10, tokensOut: 5, usd: 0.5 } },
    prompt: { text: '/service-construction C-billing', tokens: 7, charter: 'body', skills: null },
    tools: { mcp: null, builtin: ['Read'] },
    editScope: { components: null, extra: null, stateOnly: false },
    checks: null,
    hash: 'h1',
    specChanged: false,
    ...over,
  };
}

const wireRun = {
  id: 'cr1',
  activity: 'C-billing',
  task: 'construction',
  revision: 'a2',
  specHash: 'h0',
  artifact: 'art',
  startedAt: null,
  results: [{ checkId: 'rule:gloss', kind: 'rule', status: 'pass', durationMs: 4 }],
};

void test('mapTaskSpec: an absent checkRun stays absent (not run, not an error)', () => {
  const out = wire.mapTaskSpec(wireSpec());
  assert.equal('checkRun' in out, false);
  assert.equal(out.specChanged, false);
});

void test('mapTaskSpec: nullable wire lists become plain arrays', () => {
  const out = wire.mapTaskSpec(wireSpec());
  assert.deepEqual(out.prompt.skills, []);
  assert.deepEqual(out.tools, { mcp: [], builtin: ['Read'] });
  assert.deepEqual(out.checks, []);
});

void test('mapTaskSpec: an empty editScope maps to empty lists, stateOnly kept', () => {
  const out = wire.mapTaskSpec(
    wireSpec({ editScope: { components: null, extra: null, stateOnly: true } })
  );
  assert.deepEqual(out.editScope, { components: [], extra: [], stateOnly: true });
  const withPaths = wire.mapTaskSpec(
    wireSpec({
      editScope: {
        components: [{ id: 'billing', paths: null }],
        extra: ['go.mod'],
        stateOnly: false,
      },
    })
  );
  assert.deepEqual(withPaths.editScope.components, [{ id: 'billing', paths: [] }]);
  assert.deepEqual(withPaths.editScope.extra, ['go.mod']);
});

void test('mapTaskSpec: specChanged and a present checkRun come through, nulls dropped', () => {
  const out = wire.mapTaskSpec(wireSpec({ specChanged: true, checkRun: wireRun }));
  assert.equal(out.specChanged, true);
  assert.equal(out.checkRun.id, 'cr1');
  assert.equal('startedAt' in out.checkRun, false, 'a null timestamp is omitted');
  assert.deepEqual(out.checkRun.results, [
    { checkId: 'rule:gloss', kind: 'rule', status: 'pass', durationMs: 4 },
  ]);
});

void test('mapTaskSpec: check sources keep only the members that apply', () => {
  const out = wire.mapTaskSpec(
    wireSpec({
      checks: [
        {
          id: 'rule:gloss',
          kind: 'rule',
          title: 'Glossary rules',
          timeoutSec: 30,
          source: { rulePrefixes: ['GLOSS-'], ruleSlot: 'glossary', argv: null },
        },
      ],
    })
  );
  assert.deepEqual(out.checks[0].source, { rulePrefixes: ['GLOSS-'], ruleSlot: 'glossary' });
});

void test('mapTaskSpec: a status or kind outside the contract is refused loudly', () => {
  assert.throws(
    () =>
      wire.mapTaskSpec(
        wireSpec({ checkRun: { ...wireRun, results: [{ ...wireRun.results[0], status: 'ok' }] } })
      ),
    /check status/
  );
  assert.throws(
    () => wire.mapTaskSpec(wireSpec({ model: { ...wireSpec().model, source: 'x' } })),
    /model source/
  );
});

/** An OpsClient whose one view answer is `view`; records the queries it saw. */
function opsAnswering(view) {
  const seen = [];
  const call = (op, params) => {
    seen.push({ op, query: params.body.query });
    return Promise.resolve(structuredClone(view));
  };
  return { ops: { call, callForBody: call }, seen };
}

void test('useTaskSpec: the key is [taskSpec, project, activity, task, revision]', () => {
  const { ops } = opsAnswering({});
  const o = hook.taskSpecQueryOptions(ops, 'p', 'a', 't', 3);
  assert.deepEqual(o.queryKey, ['taskSpec', 'p', 'a', 't', 3]);
  assert.deepEqual(hook.taskSpecKey('p', 'a', undefined, undefined), [
    'taskSpec',
    'p',
    'a',
    undefined,
    undefined,
  ]);
  assert.equal(o.staleTime, 60_000);
});

void test('useTaskSpec: disabled when any id is empty', () => {
  const { ops } = opsAnswering({});
  assert.equal(hook.taskSpecQueryOptions(ops, 'p', 'a', 't', undefined).enabled, true);
  assert.equal(hook.taskSpecQueryOptions(ops, '', 'a', 't', 1).enabled, false);
  assert.equal(hook.taskSpecQueryOptions(ops, 'p', '', 't', 1).enabled, false);
  assert.equal(hook.taskSpecQueryOptions(ops, 'p', 'a', '', 1).enabled, false);
  assert.equal(hook.taskSpecQueryOptions(ops, 'p', 'a', undefined, 1).enabled, false);
});

void test('useTaskSpec: queries kind taskSpec with the task revision number', async () => {
  const { ops, seen } = opsAnswering({ kind: 'taskSpec', taskSpec: wireSpec() });
  const qc = new QueryClient();
  const out = await qc.fetchQuery(hook.taskSpecQueryOptions(ops, 'p', 'a', 't', 2));
  assert.equal(out.hash, 'h1');
  assert.equal(seen[0].op, 'deliveryQueryProjectView');
  assert.deepEqual(seen[0].query, {
    kind: 'taskSpec',
    projectId: 'p',
    activityId: 'a',
    taskId: 't',
    revision: 2,
  });
  const { ops: ops2, seen: seen2 } = opsAnswering({ kind: 'taskSpec', taskSpec: wireSpec() });
  await new QueryClient().fetchQuery(hook.taskSpecQueryOptions(ops2, 'p', 'a', 't', undefined));
  assert.equal('revision' in seen2[0].query, false, 'no revision asks for the latest run');
});

void test('useTaskSpec: a view with no taskSpec is an empty_body error', async () => {
  const { ops } = opsAnswering({ kind: 'taskSpec' });
  await assert.rejects(
    new QueryClient().fetchQuery(hook.taskSpecQueryOptions(ops, 'p', 'a', 't', 1)),
    (e) => e.code === 'empty_body' || /no taskSpec/.test(e.message)
  );
});
