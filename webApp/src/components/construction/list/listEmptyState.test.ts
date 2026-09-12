import { test } from 'node:test';
import assert from 'node:assert/strict';
import { emptyListCopyFor, NO_ACTIVITIES_MESSAGE } from './listEmptyState.ts';

void test('rows showing: no empty state at all', () => {
  assert.equal(emptyListCopyFor(3, 29, 'billing'), undefined);
});

void test('a search that matches nothing names the query and offers Clear filters — never "nothing recorded"', () => {
  const copy = emptyListCopyFor(0, 29, '  zzz-no-such ');
  assert.deepEqual(copy, { message: 'No activity matches “zzz-no-such”.', offerClear: true });
  assert.notEqual(copy.message, NO_ACTIVITIES_MESSAGE);
});

void test('a filter (no query) that hides everything says so and offers Clear filters', () => {
  assert.deepEqual(emptyListCopyFor(0, 29, ''), {
    message: 'No activity matches the current filters.',
    offerClear: true,
  });
});

void test('a genuinely empty project says nothing is recorded, with nothing to clear', () => {
  assert.deepEqual(emptyListCopyFor(0, 0, 'x'), {
    message: NO_ACTIVITIES_MESSAGE,
    offerClear: false,
  });
});
