/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { orderedNow } from './orderedNow.ts';

void test('orderedNow never repeats: every call is strictly later than the one before', () => {
  let previous = orderedNow();
  // Far more calls than fit in distinct milliseconds: ties are what it exists for.
  for (let i = 0; i < 10_000; i++) {
    const next = orderedNow();
    assert.ok(
      next > previous,
      `call ${String(i)}: ${String(next)} is not after ${String(previous)}`
    );
    previous = next;
  }
});

void test('orderedNow stays on the epoch-millisecond clock, never behind Date.now()', () => {
  const before = Date.now();
  const at = orderedNow();
  const after = Date.now();
  assert.ok(at >= before, 'never behind the wall clock');
  // A burst moves it on by microseconds, not by milliseconds.
  assert.ok(at < after + 20, `${String(at)} has not drifted from ${String(after)}`);
});
