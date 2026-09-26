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
 * ops.test.ts). Since preview P1b no hook holds a response at all: every call
 * rides the OpsClient, whose REST transport applies throwUnlessOk (`call`) or
 * bodyUnlessError (`callForBody`) to every answer, and api/opsSeam.test.ts keeps
 * apiClient out of the hooks. This pins the rest by reading the source.
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
    // The status helpers belong to the transport now: a hook calling one is
    // holding a raw response it should not have.
    assert.doesNotMatch(
      source,
      /\b(throwUnlessOk|bodyUnlessError)\(/,
      `${file} checks a raw response`
    );
  }
});

void test('every hook reaches the server through the OpsClient', () => {
  let total = 0;
  for (const { source } of hooks) {
    total += source.match(/\bops\.(call|callForBody)\b/g)?.length ?? 0;
  }
  // The floor only guards against this scan going blind. It was 39 when three
  // Managers published forty ops across fourteen hook modules. Stage 4a's twelve
  // ops are reached from two modules, and the nine readers share ONE
  // `queryProjectView` helper rather than each naming its own op — so the honest
  // count is 20 (10 delivery writes + 2 delivery reads + 8 operations/composition
  // calls in the hooks that did not move), and a lower number means the scan or the
  // OpsClient discipline broke.
  assert.ok(total >= 20, `found ${String(total)} OpsClient calls`);
});
