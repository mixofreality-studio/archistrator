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
  readBeginFailure,
  subscribeBeginFailures,
  writeBeginFailure,
  type BeginFailure,
} from './beginFailureMemory.ts';
import { dispatchOutcomeFor } from './beginControl.ts';

const unknown = (at: number): BeginFailure => ({
  outcome: dispatchOutcomeFor(500, 'boom'),
  at,
  dismissed: false,
  holdExpired: false,
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
