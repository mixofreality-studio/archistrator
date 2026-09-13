import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  applyOperatorExpansion,
  deepLinkKey,
  deepLinkReveal,
  forgetShownLink,
  linkAlreadyShown,
  NO_EXPANSION,
  openByOperator,
  rememberShownLink,
  revealForQuery,
} from './searchExpansion.ts';

// Designer re-check N1: a link to a task opens its activity and phase and targets
// the task row; a link to a phase opens its activity; an activity link opens nothing.
void test('a deep link opens exactly its ancestors and targets the selected row', () => {
  assert.deepEqual(
    deepLinkReveal({ activityId: 'N-STP', lifecyclePhase: 'construction', task: 'codeReview' }),
    {
      expand: ['N-STP', 'N-STP::construction'],
      target: 'N-STP::construction::codeReview',
    }
  );
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

// Fix-C review N1: the reveal runs only when the URL selection changed — a lens
// switch remounts the tree and must not re-open what the operator collapsed.
void test('a deep link is shown once per selection, however often the tree remounts', () => {
  forgetShownLink();
  const sel = { activityId: 'N-STP', lifecyclePhase: 'construction', task: 'codeReview' };
  const link = deepLinkKey('archistrator', sel);
  assert.equal(link, '["archistrator","N-STP","construction","codeReview"]');
  assert.equal(
    deepLinkKey('archistrator', { activityId: 'N-STP' }),
    '["archistrator","N-STP","",""]'
  );
  assert.equal(linkAlreadyShown(link), false, 'a fresh console reveals it');
  rememberShownLink(link);
  assert.equal(linkAlreadyShown(link), true, 'a remount with the same selection does not');
  const other = deepLinkKey('archistrator', {
    activityId: 'N-STP',
    lifecyclePhase: 'construction',
  });
  assert.equal(linkAlreadyShown(other), false, 'a changed selection does');
  forgetShownLink();
});

// Fix-D review M1: the memory is per PROJECT. The same a/p/k in another project
// is a different link, and an in-app switch to it must reveal it.
void test('the same deep link in a second project is a new link', () => {
  forgetShownLink();
  const sel = { activityId: 'N-STP', lifecyclePhase: 'construction', task: 'codeReview' };
  rememberShownLink(deepLinkKey('archistrator', sel));
  assert.equal(linkAlreadyShown(deepLinkKey('gtdapp', sel)), false);
  assert.notEqual(deepLinkKey('a', { activityId: 'b|c' }), deepLinkKey('a|b', { activityId: 'c' }));
  forgetShownLink();
});
