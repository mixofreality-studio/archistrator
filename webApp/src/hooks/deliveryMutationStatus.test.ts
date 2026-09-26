/// <reference types="node" />
/**
 * Every delivery mutation decides success by its STATUS (fix-D review I2).
 *
 * openapi-fetch 0.14.1 returns `error: undefined` for an empty body, which is what
 * a proxy's 502/503/504 looks like. A mutation that tested `error !== undefined`
 * counted those as success. The Begin dispatch is pinned end to end
 * (construction-begin-confirm.spec, I2) and so is Resume (construction-resume.spec);
 * the rest have no browser flow that reaches them today, so this reads the source.
 *
 * Since preview P1b every one rides the OpsClient — `ops.call`, whose REST transport
 * applies throwUnlessOk, or `ops.callForBody`, which also refuses a 2xx that owes a
 * body and carries none (ops.test.ts pins both). So this requires each of the TEN
 * delivery writes to be called exactly once, and nothing to hold a raw response it
 * could misread.
 *
 * It was `constructionMutationStatus.test.ts` and pinned six `construction*` ops.
 * Stage 4a's one Manager publishes ten writes for all three rails, so the subject
 * of the test is the write surface, not one rail of it.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const source = readFileSync(new URL('./useDeliveryMutations.ts', import.meta.url), 'utf8');

/** The ten writes of the twelve-op contract, in contract order. */
const DELIVERY_WRITES = [
  'deliveryStartProject',
  'deliveryExecuteNextActivity',
  'deliveryDispatchActivityTask',
  'deliverySubmitReviewDecision',
  'deliveryAskQuestions',
  'deliveryAcknowledgeStaleBasis',
  'deliverySetProjectRunState',
  'deliveryOverrideActivity',
  'deliveryReplanProject',
  'deliverySetProjectExecutionPolicy',
] as const;

void test('each delivery mutation goes through the OpsClient, whose transport checks the status', () => {
  // A type argument may nest (`ops.call<OpResult<'…'> | undefined>(`), so it is
  // matched lazily up to the `>(` that opens the call. Both entry points count:
  // a write that returns a body uses callForBody, and its refusal of an
  // empty-but-owed body is part of the same guarantee.
  const calls = [
    ...source.matchAll(/\bops\.(?:call|callForBody)(?:<[\s\S]*?>)?\(\s*'([A-Za-z]+)'/g),
  ].map((m) => m[1]);
  assert.deepEqual([...calls].sort(), [...DELIVERY_WRITES].sort(), 'the ten delivery writes');
  assert.doesNotMatch(source, /\bapiClient\b/, 'no raw client');
  assert.doesNotMatch(source, /\bresponse\b\s*[,}]/, 'no raw response destructured');
  assert.doesNotMatch(source, /error !== undefined/);
});

void test('the two read ops are NOT written from the mutations module', () => {
  // A mutation that read through QueryProjectView would be inventing a cache entry
  // outside the query layer's keys, which is how a screen goes stale after a write.
  assert.doesNotMatch(source, /'deliveryQueryProjectView'/);
  assert.doesNotMatch(source, /'deliveryQueryActivityView'/);
});
