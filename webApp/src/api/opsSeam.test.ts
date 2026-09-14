/// <reference types="node" />
/**
 * ONE seam: nothing reaches the server except through the OpsClient (preview P1b).
 *
 * The preview P1 report found 23 raw `apiClient` calls in 10 hooks. Each skipped
 * the OpsClient, so it also skipped the MCP transport (an MCP-hosted app has no
 * network to the API origin) and the preview's fixture transport (the network
 * guard refused it). They all ride `useOpsClient().ops` now, and this scan keeps
 * it that way, by reading every source file under src/:
 *
 *  1. `apiClient`, the raw openapi-fetch client, appears in code only in the ops
 *     client implementation itself: api/client.ts (which makes it and wraps it in
 *     the REST OpsClient) and the generated api/ops.gen.ts. Comments are
 *     stripped first, so prose may name it.
 *  2. openapi-fetch is imported only by that implementation.
 *  3. The api/client module (the REST OpsClient's home) is imported only by
 *     main.tsx, the browser SPA's entry, which hands it to <OpsClientProvider>.
 *     Anything else importing it would pin itself to REST and bypass the seam.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';

const SRC = fileURLToPath(new URL('../', import.meta.url));

/** The ops client implementation: the only files that may touch the raw client. */
const IMPLEMENTATION = new Set(['api/client.ts', 'api/ops.gen.ts']);
/** The only importer of the api/client module: the REST shell's entry. */
const CLIENT_MODULE_IMPORTERS = new Set(['main.tsx']);

function sources(dir: string): string[] {
  const out: string[] = [];
  for (const name of readdirSync(dir)) {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) out.push(...sources(path));
    else if (/\.(ts|tsx)$/.test(name) && !/\.test\.tsx?$/.test(name)) out.push(path);
  }
  return out;
}

/** The code without its comments (block, then line; a `//` in a string is rare
 *  enough here, and would only hide code, never invent it). */
function code(source: string): string {
  return source.replace(/\/\*[\s\S]*?\*\//g, '').replace(/(^|[^:'"])\/\/.*$/gm, '$1');
}

const files = sources(SRC).map((path) => {
  const rel = relative(SRC, path).split('\\').join('/');
  return { rel, code: code(readFileSync(path, 'utf8')) };
});

void test('the scan sees the app (it cannot pass blind)', () => {
  assert.ok(files.length > 200, `scanned ${String(files.length)} files`);
  for (const allowed of [...IMPLEMENTATION, ...CLIENT_MODULE_IMPORTERS]) {
    assert.ok(
      files.some((f) => f.rel === allowed),
      `${allowed} is on an allowlist but does not exist`
    );
  }
  // The allowlist is not stale: the implementation really is where apiClient lives.
  const client = files.find((f) => f.rel === 'api/client.ts');
  assert.match(client?.code ?? '', /export const apiClient = createClient/);
});

void test('apiClient appears in code only in the ops client implementation', () => {
  const offenders = files
    .filter((f) => !IMPLEMENTATION.has(f.rel) && /\bapiClient\b/.test(f.code))
    .map((f) => f.rel);
  assert.deepEqual(offenders, [], 'call useOpsClient().ops instead');
});

void test('openapi-fetch is imported only by the ops client implementation', () => {
  const offenders = files
    .filter((f) => !IMPLEMENTATION.has(f.rel) && /from\s+['"]openapi-fetch['"]/.test(f.code))
    .map((f) => f.rel);
  assert.deepEqual(offenders, []);
});

/** Whether an import specifier in `rel` resolves to src/api/client(.ts). */
function importsClientModule(rel: string, specifier: string): boolean {
  if (!specifier.startsWith('.')) return false;
  const from = rel.split('/').slice(0, -1);
  for (const part of specifier.replace(/\.ts$/, '').split('/')) {
    if (part === '..') from.pop();
    else if (part !== '.') from.push(part);
  }
  return from.join('/') === 'api/client';
}

void test('only the browser entry imports the api/client module', () => {
  const offenders = files
    .filter((f) => !IMPLEMENTATION.has(f.rel) && !CLIENT_MODULE_IMPORTERS.has(f.rel))
    .filter((f) =>
      [...f.code.matchAll(/(?:from|import)\s*\(?\s*['"]([^'"]+)['"]/g)].some((m) =>
        importsClientModule(f.rel, m[1] ?? '')
      )
    )
    .map((f) => f.rel);
  assert.deepEqual(offenders, [], 'take the OpsClient from useOpsClient()');
  // The resolver itself, so this rule cannot pass blind.
  assert.ok(importsClientModule('hooks/useX.ts', '../api/client'));
  assert.ok(importsClientModule('main.tsx', './api/client.ts'));
  assert.ok(!importsClientModule('hooks/useX.ts', '../api/opsContext'));
});
