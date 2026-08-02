/// <reference types="node" />
/**
 * Unit tests for parallelEdges: the per-pair ordinal that lets LayeredStepEdge
 * spread the strands a call chain stacks between the same two participants
 * (founder QA round 4 — seven SystemDesignManager→AgenticJobAccess calls drawn
 * as one line, so "Next does nothing").
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { parallelIndex, parallelLane } from './parallelEdges.ts';

void test('a lone edge is slot 0 of 1', () => {
  const slots = parallelIndex([{ id: 'e1', from: 'a', to: 'b' }]);
  assert.deepEqual(slots.get('e1'), { index: 0, count: 1 });
});

void test('edges sharing a directed pair are numbered in supplied order', () => {
  const slots = parallelIndex([
    { id: '6', from: 'sdm', to: 'aja' },
    { id: '7', from: 'sdm', to: 'aja' },
    { id: '9', from: 'sdm', to: 'aja' },
  ]);
  assert.deepEqual(slots.get('6'), { index: 0, count: 3 });
  assert.deepEqual(slots.get('7'), { index: 1, count: 3 });
  assert.deepEqual(slots.get('9'), { index: 2, count: 3 });
});

void test('different pairs are bucketed independently, interleaving and all', () => {
  const slots = parallelIndex([
    { id: 'a1', from: 'x', to: 'y' },
    { id: 'b1', from: 'p', to: 'q' },
    { id: 'a2', from: 'x', to: 'y' },
  ]);
  assert.deepEqual(slots.get('a1'), { index: 0, count: 2 });
  assert.deepEqual(slots.get('a2'), { index: 1, count: 2 });
  assert.deepEqual(slots.get('b1'), { index: 0, count: 1 });
});

void test('direction is part of the bucket key — a→b and b→a do not stack', () => {
  const slots = parallelIndex([
    { id: 'f', from: 'a', to: 'b' },
    { id: 'r', from: 'b', to: 'a' },
  ]);
  assert.deepEqual(slots.get('f'), { index: 0, count: 1 });
  assert.deepEqual(slots.get('r'), { index: 0, count: 1 });
});

void test('every supplied edge gets an entry and no others are invented', () => {
  const slots = parallelIndex([
    { id: 'e1', from: 'a', to: 'b' },
    { id: 'e2', from: 'a', to: 'b' },
  ]);
  assert.equal(slots.size, 2);
  assert.equal(slots.get('missing'), undefined);
});

void test('an empty chain yields no slots', () => {
  assert.equal(parallelIndex([]).size, 0);
});

void test('lanes centre on zero so the fan is symmetric about the original path', () => {
  assert.equal(parallelLane(undefined), 0);
  assert.equal(parallelLane({ index: 0, count: 1 }), 0);
  assert.deepEqual(
    [0, 1, 2].map((index) => parallelLane({ index, count: 3 })),
    [-1, 0, 1]
  );
  assert.deepEqual(
    [0, 1, 2, 3].map((index) => parallelLane({ index, count: 4 })),
    [-1.5, -0.5, 0.5, 1.5]
  );
});

void test('the seven-strand case spreads across seven distinct lanes', () => {
  const lanes = [0, 1, 2, 3, 4, 5, 6].map((index) => parallelLane({ index, count: 7 }));
  assert.deepEqual(lanes, [-3, -2, -1, 0, 1, 2, 3]);
  assert.equal(new Set(lanes).size, 7);
});
