import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  applyOperatorExpansion,
  deepLinkReveal,
  NO_EXPANSION,
  openByOperator,
  revealForQuery,
} from './searchExpansion.ts';

// Designer re-check N1: a link to a task opens its activity and phase and targets
// the task row; a link to a phase opens its activity; an activity link opens nothing.
void test('a deep link opens exactly its ancestors and targets the selected row', () => {
  assert.deepEqual(deepLinkReveal({ activityId: 'N-STP', lifecyclePhase: 'construction', task: 'codeReview' }), {
    expand: ['N-STP', 'N-STP::construction'],
    target: 'N-STP::construction::codeReview',
  });
  assert.deepEqual(deepLinkReveal({ activityId: 'N-STP', lifecyclePhase: 'construction' }), {
    expand: ['N-STP'],
    target: 'N-STP::construction',
  });
  assert.deepEqual(deepLinkReveal({ activityId: 'N-STP' }), { expand: [], target: null });
  assert.deepEqual(deepLinkReveal({}), { expand: [], target: null });
});

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
