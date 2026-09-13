/// <reference types="node" />
/**
 * Every construction mutation decides success by its STATUS (fix-D review I2).
 *
 * openapi-fetch 0.14.1 returns `error: undefined` for an empty body, which is
 * what a proxy's 502/503/504 looks like. A mutation that tested
 * `error !== undefined` counted those as success. The Begin dispatch is pinned
 * end to end (construction-begin-confirm.spec, I2). The other five (pause,
 * override, phase decision, both review-policy writes) have no browser flow that
 * reaches them today, so this reads the hook's source and requires one
 * throwUnlessOk per POST.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const source = readFileSync(new URL('./useConstructionMutations.ts', import.meta.url), 'utf8');

void test('one throwUnlessOk per construction POST, and no success decided from the parsed body', () => {
  const posts = source.match(/apiClient\.POST\(/g)?.length ?? 0;
  const guarded = source.match(/throwUnlessOk\(response, error\);/g)?.length ?? 0;
  assert.equal(posts, 6, 'the six construction mutations');
  assert.equal(guarded, posts, 'every POST checks its status');
  assert.doesNotMatch(source, /error !== undefined/);
});
