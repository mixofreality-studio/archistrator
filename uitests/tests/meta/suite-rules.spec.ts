/**
 * tests/meta/suite-rules.spec.ts: two rules for the whole suite (fix-H rulings).
 *
 *   (a) EVERY spec runs under the dispatch guard. The guarded `test` is the default
 *       export of support/dispatchGuard.ts, and no file under tests/ imports
 *       '@playwright/test''s own `test`, except the guard itself (which wraps it)
 *       and the seed step (the one place a real project may be created).
 *       meta/project-creation-guard.spec only covered specs that open a project.
 *   (b) A probe that gets NO answer FAILS the test, never skips it: a skip reads as
 *       green and can hide a regression (fix-H report, concern 1). Driven against
 *       a port nothing listens on.
 *   (c) The API contexts are guarded too (cleanup round): the `request` fixture,
 *       page.request and context.request pass through no route, so the guard wraps
 *       them — a write REJECTS before it is sent unless the spec allowlisted it,
 *       and no file takes the unguarded `request` factory from '@playwright/test'.
 */
import guardedDefault, {
  LIVE_DRAFTING_WRITES,
  REQUEST_GUARD_REFUSAL,
  test,
  expect,
} from '../support/dispatchGuard.js';
import { readdirSync, readFileSync } from 'node:fs';
import { join, relative } from 'node:path';
import {
  requireServer,
  skipUnlessConstructionArtifacts,
  skipUnlessContent,
} from '../support/gating.js';

const TESTS = join(import.meta.dirname, '..');

/** The unguarded values: `test` itself, a default or namespace import (either
 *  reaches it), and the `request` factory (an APIRequestContext no guard wraps). */
const UNGUARDED_VALUES = ['test', 'default', 'namespace', 'request'];

/** Files that may import '@playwright/test''s own `test`, and why. */
const UNGUARDED_ALLOWED = new Set([
  'support/dispatchGuard.ts', // it wraps it
  'seed/shared-project.setup.ts', // the one place a real project may be created
]);

/** Every .ts under tests/, as a path relative to tests/. */
function sources(dir = TESTS): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((e) => {
    const p = join(dir, e.name);
    return e.isDirectory() ? sources(p) : e.name.endsWith('.ts') ? [relative(TESTS, p)] : [];
  });
}

/**
 * The value bindings an import clause takes from '@playwright/test': a default or
 * namespace import counts as `test` too, since either reaches it.
 */
function playwrightValueImports(src: string): string[] {
  const out: string[] = [];
  // One import STATEMENT at a time: it starts a line, and its clause crosses no
  // quote or `;`, so it can never run on into the next statement's specifier.
  const re = /^import\s+(?!type\s)([^;'"]*?)\s+from\s+['"]([^'"]+)['"]/gm;
  for (const m of src.matchAll(re)) {
    if (m[2] !== '@playwright/test') continue;
    const clause = m[1] ?? '';
    const named = /\{([\s\S]*)\}/.exec(clause)?.[1];
    const outside = clause
      .replace(/\{[\s\S]*\}/, '')
      .replace(/,/g, ' ')
      .trim();
    if (outside.length > 0) out.push(outside.startsWith('*') ? 'namespace' : 'default');
    for (const part of (named ?? '').split(',')) {
      const name = part.trim();
      if (name.length === 0 || name.startsWith('type ')) continue;
      out.push(name.split(/\s+as\s+/)[0] ?? name);
    }
  }
  return out;
}

test('the guarded test is the default export of the support module', () => {
  expect(guardedDefault).toBe(test);
});

test('no file imports the unguarded test from @playwright/test', () => {
  const files = sources();
  // A floor on the SCAN, not on the suite (see the sibling floor in
  // meta/project-creation-guard.spec.ts): it fails if `sources()` ever stops
  // walking tests/ and this check starts passing over an empty list. Stage 5
  // Task 13 retired 38 spec files with the screens they drove, taking the tree
  // from 45 .ts files to 22, so the floor moves with it.
  expect(files.length).toBeGreaterThan(18);
  const offenders = files
    .filter((f) => !UNGUARDED_ALLOWED.has(f))
    .flatMap((f) =>
      playwrightValueImports(readFileSync(join(TESTS, f), 'utf8'))
        .filter((name) => UNGUARDED_VALUES.includes(name))
        .map((name) => `${f}: ${name}`)
    );
  expect(offenders, 'import test from ./support/dispatchGuard.js instead').toEqual([]);
});

test('the import scan sees every way to take the unguarded test', () => {
  const scan = (src: string): string[] =>
    playwrightValueImports(src).filter((n) => UNGUARDED_VALUES.includes(n));
  expect(scan("import { test, expect } from '@playwright/test';")).toEqual(['test']);
  expect(scan("import { request, expect } from '@playwright/test';")).toEqual(['request']);
  expect(scan("import type { APIRequestContext } from '@playwright/test';")).toEqual([]);
  expect(scan("import { expect, test as it } from '@playwright/test';")).toEqual(['test']);
  expect(scan("import {\n  test,\n  type Page,\n} from '@playwright/test';")).toEqual(['test']);
  expect(scan("import pw from '@playwright/test';")).toEqual(['default']);
  expect(scan("import * as pw from '@playwright/test';")).toEqual(['namespace']);
  expect(scan("import type { Page } from '@playwright/test';")).toEqual([]);
  expect(scan("import { type Page, expect } from '@playwright/test';")).toEqual([]);
  // A real file: the offending import is on any line, never only the first.
  expect(
    scan(
      "/** header */\nimport { expect } from './support/x.js';\nimport { test } from '@playwright/test';\n"
    )
  ).toEqual(['test']);
  // One statement at a time: a guarded import followed by a type-only one is clean.
  expect(
    scan(
      "import { test, expect } from './support/dispatchGuard.js';\nimport type { Page, Route } from '@playwright/test';\n"
    )
  ).toEqual([]);
  // Import text inside a string is not an import.
  expect(scan('const s = "import { test } from \'@playwright/test\'";\n')).toEqual([]);
});

test('every spec takes its test from the dispatch guard', () => {
  const guarded = /import\s+[^;]*\btest\b[^;]*from\s+'\.\.?\/support\/dispatchGuard\.js'/;
  const guardedDefaultImport =
    /import\s+\w+(\s*,\s*\{[^}]*\})?\s+from\s+'\.\.?\/support\/dispatchGuard\.js'/;
  const specs = sources().filter((f) => f.endsWith('.spec.ts'));
  for (const f of specs) {
    const src = readFileSync(join(TESTS, f), 'utf8');
    expect(guarded.test(src) || guardedDefaultImport.test(src), `${f} does not use the guard`).toBe(
      true
    );
  }
});

// A port nothing listens on: the connection is refused at once, so no answer.
const NOWHERE = 'http://127.0.0.1:9';

test('the server probe FAILS when nothing answers; it never skips', async ({ request }) => {
  await expect(requireServer(request, NOWHERE)).rejects.toThrow(/did not answer/);
  // Had the probe skipped, this test would be marked skipped, and read as green.
  expect(test.info().expectedStatus).toBe('passed');
  expect(test.info().annotations.filter((a) => a.type === 'skip')).toEqual([]);
});

test('the construction-artifacts probe FAILS when nothing answers; it never skips', async ({
  request,
}) => {
  await expect(skipUnlessConstructionArtifacts(request, NOWHERE)).rejects.toThrow(/did not answer/);
  expect(test.info().expectedStatus).toBe('passed');
  expect(test.info().annotations.filter((a) => a.type === 'skip')).toEqual([]);
});

// (c) Against the same dead port: an UNGUARDED write would reject too, but with the
// connection error, so each assertion names which of the two happened.
const WRITE = `${NOWHERE}/api/v1/delivery/execute-next-activity/p`;
const DRAFT = `${NOWHERE}/api/v1/delivery/dispatch-activity-task/p/a`;
const REFUSED = new RegExp(REQUEST_GUARD_REFUSAL);
const SENT = /ECONNREFUSED/;

test('the request fixture refuses every write verb before sending it', async ({ request }) => {
  await expect(request.post(WRITE)).rejects.toThrow(REFUSED);
  await expect(request.put(WRITE)).rejects.toThrow(REFUSED);
  await expect(request.patch(WRITE)).rejects.toThrow(REFUSED);
  await expect(request.delete(WRITE)).rejects.toThrow(REFUSED);
  await expect(request.fetch(WRITE, { method: 'POST' })).rejects.toThrow(REFUSED);
  await expect(request.fetch(WRITE, { method: 'delete' })).rejects.toThrow(REFUSED);
  // Relative, the way a spec writes it: resolved against baseURL, still refused.
  await expect(request.post('/api/v1/delivery/execute-next-activity/p')).rejects.toThrow(
    REFUSED
  );
});

test('the request fixture lets GET and HEAD through', async ({ request }) => {
  await expect(request.get(`${NOWHERE}/x`)).rejects.toThrow(SENT);
  await expect(request.head(`${NOWHERE}/x`)).rejects.toThrow(SENT);
  await expect(request.fetch(`${NOWHERE}/x`)).rejects.toThrow(SENT);
});

test('page.request and context.request are guarded the same way', async ({ page, context }) => {
  await expect(page.request.post(WRITE)).rejects.toThrow(REFUSED);
  await expect(context.request.delete(WRITE)).rejects.toThrow(REFUSED);
  await expect(page.request.get(`${NOWHERE}/x`)).rejects.toThrow(SENT);
});

test.describe('with the live-drafting writes allowed', () => {
  test.use({ dispatchGuardAllows: LIVE_DRAFTING_WRITES });

  test('an allowlisted write is sent; a pump write still is not', async ({
    request,
    page,
  }) => {
    await expect(request.post(DRAFT)).rejects.toThrow(SENT);
    await expect(page.request.post(DRAFT)).rejects.toThrow(SENT);
    await expect(request.post(WRITE)).rejects.toThrow(REFUSED);
  });
});

// The seeded CI job (uitests-construction) sets REQUIRE_CONSTRUCTION_ARTIFACTS=1:
// there, missing content is a broken seed, and a skip would read as green.
test('under REQUIRE_CONSTRUCTION_ARTIFACTS=1 missing content FAILS; it never skips', () => {
  const saved = process.env.REQUIRE_CONSTRUCTION_ARTIFACTS;
  process.env.REQUIRE_CONSTRUCTION_ARTIFACTS = '1';
  try {
    expect(() => {
      skipUnlessContent(false, 'no content.');
    }).toThrow(/REQUIRE_CONSTRUCTION_ARTIFACTS=1/);
    // Present content is not a reason to fail.
    skipUnlessContent(true, 'unused');
  } finally {
    if (saved === undefined) delete process.env.REQUIRE_CONSTRUCTION_ARTIFACTS;
    else process.env.REQUIRE_CONSTRUCTION_ARTIFACTS = saved;
  }
  expect(test.info().expectedStatus).toBe('passed');
  expect(test.info().annotations.filter((a) => a.type === 'skip')).toEqual([]);
});
