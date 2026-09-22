import { test } from 'node:test';
import assert from 'node:assert/strict';
import { stackCards } from './commentMarginLayout.ts';

void test('cards that do not collide keep their desired position', () => {
  const got = stackCards(
    [
      { id: 'a', desiredTop: 0, height: 40 },
      { id: 'b', desiredTop: 200, height: 40 },
    ],
    8
  );
  assert.deepEqual(got, [
    { id: 'a', top: 0 },
    { id: 'b', top: 200 },
  ]);
});

void test('a colliding card is pushed down by the gap', () => {
  const got = stackCards(
    [
      { id: 'a', desiredTop: 0, height: 40 },
      { id: 'b', desiredTop: 10, height: 40 },
    ],
    8
  );
  assert.deepEqual(got, [
    { id: 'a', top: 0 },
    { id: 'b', top: 48 },
  ]);
});

void test('a cascade pushes every subsequent card', () => {
  const got = stackCards(
    [
      { id: 'a', desiredTop: 0, height: 40 },
      { id: 'b', desiredTop: 0, height: 40 },
      { id: 'c', desiredTop: 0, height: 40 },
    ],
    8
  );
  assert.deepEqual(
    got.map((c) => c.top),
    [0, 48, 96]
  );
});

void test('input is sorted by desiredTop regardless of argument order', () => {
  const got = stackCards(
    [
      { id: 'late', desiredTop: 300, height: 40 },
      { id: 'early', desiredTop: 0, height: 40 },
    ],
    8
  );
  assert.deepEqual(
    got.map((c) => c.id),
    ['early', 'late']
  );
});

void test('an empty list places nothing', () => {
  assert.deepEqual(stackCards([], 8), []);
});
