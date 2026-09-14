/// <reference types="node" />
/**
 * Every construction mutation decides success by its STATUS (fix-D review I2).
 *
 * openapi-fetch 0.14.1 returns `error: undefined` for an empty body, which is
 * what a proxy's 502/503/504 looks like. A mutation that tested
 * `error !== undefined` counted those as success. The Begin dispatch is pinned
 * end to end (construction-begin-confirm.spec, I2), and so is Resume
 * (construction-resume.spec). The other four (pause, override, phase decision, the
 * review-policy preset) have no browser flow that reaches them today, so this reads
 * the hook's source.
 *
 * Since preview P1b every one rides the OpsClient (`ops.call`), whose REST
 * transport applies throwUnlessOk to every answer (ops.test.ts pins that). So
 * this requires each of the six construction ops (resume joined in B1.7) to be called through
 * `ops.call` exactly once, and nothing to hold a raw response it could misread.
 * (The per-type review-policy write went with PolicyPanel, its only caller, in
 * the cleanup round.)
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const source = readFileSync(new URL('./useConstructionMutations.ts', import.meta.url), 'utf8');

const CONSTRUCTION_OPS = [
  'constructionExecuteNextActivity',
  'constructionPauseProject',
  'constructionResumeProject',
  'constructionOverrideActivity',
  'constructionSubmitPhaseDecision',
  'constructionSetReviewPolicy',
] as const;

void test('each construction mutation goes through ops.call, whose transport checks the status', () => {
  // A type argument may nest (`ops.call<OpResult<'…'> | undefined>(`), so it is
  // matched lazily up to the `>(` that opens the call.
  const calls = [...source.matchAll(/\bops\.call(?:<[\s\S]*?>)?\(\s*'([A-Za-z]+)'/g)].map(
    (m) => m[1]
  );
  assert.deepEqual([...calls].sort(), [...CONSTRUCTION_OPS].sort(), 'the six construction ops');
  assert.doesNotMatch(source, /\bapiClient\b/, 'no raw client');
  assert.doesNotMatch(source, /\bresponse\b\s*[,}]/, 'no raw response destructured');
  assert.doesNotMatch(source, /error !== undefined/);
});
