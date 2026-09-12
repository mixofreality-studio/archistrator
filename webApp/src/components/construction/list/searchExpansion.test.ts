import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  applyOperatorExpansion,
  NO_EXPANSION,
  openByOperator,
  revealForQuery,
} from './searchExpansion.ts';

void test('clearing the query closes what the search opened, and only that', () => {
  const opened = revealForQuery(NO_EXPANSION, ['C-a', 'C-a::requirements', 'C-b']);
  assert.deepEqual(opened.expanded, ['C-a', 'C-a::requirements', 'C-b']);
  const cleared = revealForQuery(opened, []);
  assert.deepEqual(cleared.expanded, []);
  assert.deepEqual(cleared.searchOpened, []);
});

void test('a row open before the search is never closed by clearing it', () => {
  const mine = applyOperatorExpansion(NO_EXPANSION, ['C-a']);
  const searched = revealForQuery(mine, ['C-a', 'C-b']);
  assert.deepEqual(searched.searchOpened, ['C-b'], 'C-a was already the operator’s');
  assert.deepEqual(revealForQuery(searched, []).expanded, ['C-a']);
});

void test('a search-opened row the operator closed stays closed; one they reopened is theirs', () => {
  const searched = revealForQuery(NO_EXPANSION, ['C-a', 'C-b']);
  const closedA = applyOperatorExpansion(searched, ['C-b']);
  assert.deepEqual(closedA.searchOpened, ['C-b']);
  const reopenedA = applyOperatorExpansion(closedA, ['C-b', 'C-a']);
  assert.deepEqual(reopenedA.searchOpened, ['C-b']);
  assert.deepEqual(revealForQuery(reopenedA, []).expanded, ['C-a']);
});

void test('a new query replaces the previous reveal rather than piling onto it', () => {
  const first = revealForQuery(NO_EXPANSION, ['C-a', 'C-b']);
  const second = revealForQuery(first, ['C-b', 'C-c']);
  assert.deepEqual(second.expanded, ['C-b', 'C-c']);
  assert.deepEqual(second.searchOpened, ['C-b', 'C-c']);
});

void test('"Expand to current phase" claims rows for the operator, so a clear keeps them', () => {
  const searched = revealForQuery(NO_EXPANSION, ['C-a', 'C-b']);
  const claimed = openByOperator(searched, ['C-b', 'C-c']);
  assert.deepEqual(claimed.searchOpened, ['C-a']);
  assert.deepEqual(revealForQuery(claimed, []).expanded, ['C-b', 'C-c']);
});
