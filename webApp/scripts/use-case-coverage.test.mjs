/**
 * The PM coverage table's pure rules (src/components/activity/useCaseCoverageLogic.ts) over the
 * wire mapping the hook applies (`mapUseCaseCoverage`): Go nil slices arrive as JSON null and the
 * app shapes are never-null lists, and a path with no door, no outcome or a derivation defect is
 * the finding the reviewer is there to catch (FPC Task 2.14).
 *
 * Bundled with esbuild like task-spec-panel.test.mjs: the webApp has no component-test runner,
 * so the real sources are bundled and imported.
 */
import { test, before } from 'node:test';
import assert from 'node:assert/strict';
import { mkdirSync } from 'node:fs';
import { join, dirname } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { build } from 'esbuild';

const root = join(dirname(fileURLToPath(import.meta.url)), '..');
let logic;
let wire;

async function bundle(entry, out) {
  await build({
    entryPoints: [join(root, entry)],
    outfile: out,
    bundle: true,
    format: 'esm',
    platform: 'node',
    packages: 'external',
    jsx: 'automatic',
    define: { 'import.meta.env': '{}' },
    logLevel: 'error',
  });
  return import(pathToFileURL(out).href);
}

before(async () => {
  const dir = join(root, 'node_modules', '.cache', 'use-case-coverage-test');
  mkdirSync(dir, { recursive: true });
  logic = await bundle('src/components/activity/useCaseCoverageLogic.ts', join(dir, 'logic.mjs'));
  wire = await bundle('src/contracts/wire.ts', join(dir, 'wire.mjs'));
});

const wireView = {
  useCases: [
    {
      useCase: 'reword-a-todo',
      paths: [
        {
          scenario: 'reword-a-todo-P1',
          guards: ['[abandoned]'],
          client: '',
          door: '',
          outcomeNodes: null,
          problems: ['SCN-ACTOR-PATH-CLIENT', 'SCN-UNADDRESSED'],
        },
        {
          scenario: 'reword-a-todo-P2',
          guards: null,
          client: 'todo-web-app',
          door: 'web',
          outcomeNodes: ['Todo shows the new wording'],
          problems: null,
        },
      ],
    },
  ],
};

test('mapUseCaseCoverage: every list is never-null', () => {
  const v = wire.mapUseCaseCoverage(wireView);
  const [p1, p2] = v.useCases[0].paths;
  assert.deepEqual(p1.outcomeNodes, []);
  assert.deepEqual(p2.guards, []);
  assert.deepEqual(p2.problems, []);
  assert.deepEqual(wire.mapUseCaseCoverage({ useCases: null }).useCases, []);
});

test('door and list labels say NONE where the absence is the finding', () => {
  const [p1, p2] = wire.mapUseCaseCoverage(wireView).useCases[0].paths;
  assert.equal(logic.doorLabel(p1), 'NONE');
  assert.equal(logic.doorLabel(p2), 'todo-web-app (web)');
  assert.equal(logic.doorLabel({ ...p2, door: '' }), 'todo-web-app');
  assert.equal(logic.listLabel(p1.outcomeNodes, 'NONE'), 'NONE');
  assert.equal(logic.listLabel(['a', 'b'], 'NONE'), 'a; b');
});

test('pathIsFinding: no door, no outcome, or a defect', () => {
  const [p1, p2] = wire.mapUseCaseCoverage(wireView).useCases[0].paths;
  assert.equal(logic.pathIsFinding(p1), true);
  assert.equal(logic.pathIsFinding(p2), false);
  assert.equal(logic.pathIsFinding({ ...p2, outcomeNodes: [] }), true);
  assert.equal(logic.pathIsFinding({ ...p2, problems: ['SCN-UNADDRESSED'] }), true);
  assert.equal(logic.pathIsFinding({ ...p2, client: '' }), true);
});

test('the override note says the floor and the reason', () => {
  assert.match(logic.COVERAGE_OVERRIDE_NOTE, /every review preset/);
  assert.match(logic.COVERAGE_OVERRIDE_NOTE, /override/);
  assert.match(logic.COVERAGE_OVERRIDE_NOTE, /reason/);
});
