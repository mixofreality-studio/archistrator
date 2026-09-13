/// <reference types="node" />
/**
 * Every hook decides a response's outcome by its STATUS (fix-E review, the same
 * bug outside construction).
 *
 * openapi-fetch 0.14.1 returns `error: undefined` for an empty body, which is
 * what a proxy's 502/503/504 looks like. A hook that tested `error !== undefined`
 * counted those as success: create-project then POSTed to
 * set-operating-model/undefined and reported success, and a GET handed
 * `undefined` to its mapper, which threw a TypeError instead of "status 502".
 *
 * Behaviour is pinned where a browser reaches it (status-decides-outcome.spec,
 * construction-begin-confirm.spec) and on the shared helpers (errors.test.ts,
 * ops.test.ts). This pins the rest by reading the source: every apiClient call
 * in a hook is followed by throwUnlessOk or bodyUnlessError, and no hook decides
 * from the parsed body.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readdirSync, readFileSync } from 'node:fs';

const dir = new URL('./', import.meta.url);
const hooks = readdirSync(dir)
  .filter((f) => f.endsWith('.ts') && !f.endsWith('.test.ts'))
  .map((f) => ({ file: f, source: readFileSync(new URL(f, dir), 'utf8') }));

void test('no hook decides success from the parsed error body', () => {
  for (const { file, source } of hooks) {
    assert.doesNotMatch(source, /error !== undefined/, file);
    assert.doesNotMatch(source, /toApiError\(/, `${file} builds its own ApiError`);
  }
});

void test('every apiClient call in a hook is checked by its status', () => {
  let total = 0;
  for (const { file, source } of hooks) {
    const calls = source.match(/apiClient\.(GET|POST|PUT|PATCH|DELETE)\(/g)?.length ?? 0;
    const checked = source.match(/\b(throwUnlessOk|bodyUnlessError)\(/g)?.length ?? 0;
    assert.equal(checked, calls, `${file}: ${String(calls)} calls, ${String(checked)} checked`);
    total += calls;
  }
  // The sites the fix-E review listed, plus construction's six, plus the GETs.
  assert.ok(total >= 25, `found ${String(total)} apiClient calls`);
});
