/// <reference types="node" />
/**
 * Unit tests for the operations route's enforcement point (operationsGuard.ts)
 * — the actual thing that decides whether OperationsConsoleScreen mounts.
 * Pins the D9 distinction the coordinator's review flagged: "operations is
 * disabled" (a successful {operations:false} read — the real local case,
 * since local always answers this endpoint) and "the capabilities check
 * failed" (unreachable after retries — a cloud operator's transient network
 * blip) must NOT collapse into the same silent redirect.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { isRedirect } from '@tanstack/react-router';
import { operationsBeforeLoad, probeCapabilities } from './operationsGuard.ts';
import type { Capabilities } from '../utilities/capabilities.ts';

void test('server reports operations enabled: the route mounts, no redirect', async () => {
  const result = await operationsBeforeLoad(() => Promise.resolve({ operations: true }), 3, 0);
  assert.deepEqual(result, { capabilitiesUnreachable: false });
});

void test('server reports operations disabled: redirects home rather than mounting', async () => {
  await assert.rejects(
    async () => operationsBeforeLoad(() => Promise.resolve({ operations: false }), 3, 0),
    (err: unknown): boolean => {
      assert.ok(isRedirect(err), 'expected a Redirect to be thrown, not an arbitrary error');
      assert.equal((err as { options: { to?: string } }).options.to, '/');
      return true;
    }
  );
});

void test('server unreachable after every retry: mounts with an explicit error, never redirects', async () => {
  let calls = 0;
  const alwaysFails = (): Promise<Capabilities> => {
    calls += 1;
    return Promise.reject(new Error('network down'));
  };
  const result = await operationsBeforeLoad(alwaysFails, 3, 0);
  assert.deepEqual(result, { capabilitiesUnreachable: true });
  assert.equal(calls, 3, 'expected every retry attempt to run before giving up');
});

void test('probeCapabilities short-circuits on the first success and does not treat it as unreachable', async () => {
  let calls = 0;
  const succeedsOnSecondTry = (): Promise<Capabilities> => {
    calls += 1;
    return calls < 2
      ? Promise.reject(new Error('transient'))
      : Promise.resolve({ operations: true });
  };
  const result = await probeCapabilities(succeedsOnSecondTry, 3, 0);
  assert.deepEqual(result, { operations: true });
  assert.equal(calls, 2, 'expected the retry to stop as soon as a call succeeds');
});
