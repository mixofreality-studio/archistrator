/// <reference types="node" />
/**
 * The failed-Begin memory is per project and survives a remount (fix-E review I2).
 * The remount itself is pinned in the browser (construction-begin-confirm.spec,
 * I2); this pins the store's rules.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  failureAwaitsPump,
  readBeginDispatched,
  readBeginFailure,
  subscribeBeginDispatches,
  subscribeBeginFailures,
  writeBeginDispatched,
  writeBeginFailure,
  type BeginFailure,
} from './beginFailureMemory.ts';
import { dispatchOutcomeFor } from './beginControl.ts';

const unknown = (at: number): BeginFailure => ({
  outcome: dispatchOutcomeFor(500, 'boom'),
  at,
  dismissed: false,
  holdExpired: false,
  startedAtFailure: false,
});

void test('one failure per project: another project neither sees nor clears it', () => {
  writeBeginFailure('p-one', unknown(1));
  assert.equal(readBeginFailure('p-one')?.at, 1);
  assert.equal(readBeginFailure('p-two'), null);
  writeBeginFailure('p-two', null);
  assert.equal(readBeginFailure('p-one')?.at, 1, 'clearing p-two leaves p-one');
  writeBeginFailure('p-one', null);
  assert.equal(readBeginFailure('p-one'), null);
});

void test('an update sees the current value, and every write notifies', () => {
  let calls = 0;
  const off = subscribeBeginFailures(() => {
    calls += 1;
  });
  writeBeginFailure('p-upd', unknown(5));
  writeBeginFailure('p-upd', (f) => (f !== null && f.at === 5 ? { ...f, holdExpired: true } : f));
  assert.equal(readBeginFailure('p-upd')?.holdExpired, true);
  // An update that keeps the same value is not a change.
  writeBeginFailure('p-upd', (f) => f);
  assert.equal(calls, 2);
  off();
  writeBeginFailure('p-upd', null);
  assert.equal(calls, 2, 'no notification after unsubscribing');
});

void test('the snapshot is stable between writes (useSyncExternalStore needs it)', () => {
  writeBeginFailure('p-snap', unknown(9));
  assert.equal(readBeginFailure('p-snap'), readBeginFailure('p-snap'));
  writeBeginFailure('p-snap', null);
});

void test('an awaited pickup is kept per project, apart from the failures, and notifies only its own listeners', () => {
  let dispatchCalls = 0;
  let failureCalls = 0;
  const offD = subscribeBeginDispatches(() => {
    dispatchCalls += 1;
  });
  const offF = subscribeBeginFailures(() => {
    failureCalls += 1;
  });
  writeBeginDispatched('p-d', { at: 7 });
  assert.equal(readBeginDispatched('p-d')?.at, 7);
  assert.equal(readBeginDispatched('p-other'), null, 'another project sees nothing');
  assert.equal(readBeginFailure('p-d'), null, 'a success is not a failure');
  assert.equal(readBeginDispatched('p-d'), readBeginDispatched('p-d'), 'a stable snapshot');
  // Cleared only when it is the SAME dispatch: a newer one is never the one removed.
  writeBeginDispatched('p-d', (d) => (d !== null && d.at === 6 ? null : d));
  assert.equal(readBeginDispatched('p-d')?.at, 7);
  writeBeginDispatched('p-d', (d) => (d !== null && d.at === 7 ? null : d));
  assert.equal(readBeginDispatched('p-d'), null);
  assert.equal(dispatchCalls, 2, 'the write and the clear; the no-op update is not a change');
  assert.equal(failureCalls, 0, 'the failure listeners never heard of it');
  offD();
  offF();
});

void test('only an unknown outcome whose hold has not run out awaits the pump', () => {
  assert.equal(failureAwaitsPump(null), false);
  assert.equal(failureAwaitsPump(unknown(1)), true);
  assert.equal(failureAwaitsPump({ ...unknown(1), holdExpired: true }), false);
  assert.equal(
    failureAwaitsPump({ ...unknown(1), outcome: dispatchOutcomeFor(400, 'no') }),
    false,
    'a rejection started nothing'
  );
});
