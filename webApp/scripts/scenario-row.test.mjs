/** summarize and statusTone (testRunPanelLogic.ts), bundled with esbuild, no DOM. */
import { test, before } from 'node:test';
import assert from 'node:assert/strict';
import { mkdirSync } from 'node:fs';
import { join, dirname } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { build } from 'esbuild';

const root = join(dirname(fileURLToPath(import.meta.url)), '..');
let logic;

before(async () => {
  const dir = join(root, 'node_modules', '.cache', 'scenario-row-test');
  mkdirSync(dir, { recursive: true });
  const out = join(dir, 'logic.mjs');
  await build({
    entryPoints: [join(root, 'src/components/construction/renderers/testRunPanelLogic.ts')],
    outfile: out,
    bundle: true,
    format: 'esm',
    platform: 'node',
    packages: 'external',
    logLevel: 'error',
  });
  logic = await import(pathToFileURL(out).href);
});

test('summarize counts timeout apart from fail and skip', () => {
  const c = logic.summarize([
    { status: 'pass' },
    { status: 'fail' },
    { status: 'skip' },
    { status: 'timeout' },
    { status: 'timeout' },
    { status: 'unknown' },
  ]);
  assert.deepEqual(c, { pass: 1, fail: 1, skip: 1, timeout: 2 });
});

test('statusTone: timeout is bad, unknown and unrecognised are never good', () => {
  assert.equal(logic.statusTone('pass'), 'good');
  assert.equal(logic.statusTone('fail'), 'bad');
  assert.equal(logic.statusTone('timeout'), 'bad');
  assert.equal(logic.statusTone('skip'), 'neutral');
  assert.equal(logic.statusTone('unknown'), 'neutral');
  assert.equal(logic.statusTone('weird'), 'neutral');
});
