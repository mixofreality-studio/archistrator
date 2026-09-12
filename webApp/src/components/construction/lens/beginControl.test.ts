import { test } from 'node:test';
import assert from 'node:assert/strict';
import type { ConstructionRow } from '../../../contracts/types';
import { beginControlFor, notStartedActivities } from './beginControl.ts';

const COMMITTED_WORDS = /Begin construction|Resume construction/;

void test('while the project is loading, the button is disabled and names neither Begin nor Resume', () => {
  for (const constructionStarted of [true, false, undefined]) {
    const c = beginControlFor({ constructionStarted, projectLoading: true, running: false });
    assert.equal(c.disabled, true, `loading + ${String(constructionStarted)}`);
    assert.doesNotMatch(c.label, COMMITTED_WORDS, `loading + ${String(constructionStarted)}`);
  }
});

void test('the label is the server’s constructionStarted: true is Resume, false is Begin', () => {
  const resume = beginControlFor({
    constructionStarted: true,
    projectLoading: false,
    running: false,
  });
  assert.equal(resume.label, 'Resume construction');
  assert.equal(resume.verb, 'Resume');
  assert.equal(resume.disabled, false);
  const begin = beginControlFor({
    constructionStarted: false,
    projectLoading: false,
    running: false,
  });
  assert.equal(begin.label, 'Begin construction');
  assert.equal(begin.verb, 'Begin');
  assert.equal(begin.disabled, false);
});

void test('no project read claims neither word, and dispatches nothing', () => {
  const c = beginControlFor({
    constructionStarted: undefined,
    projectLoading: false,
    running: false,
  });
  assert.doesNotMatch(c.label, COMMITTED_WORDS);
  assert.equal(c.disabled, true);
});

void test('a run in flight disables the button whatever the project says', () => {
  const c = beginControlFor({ constructionStarted: false, projectLoading: false, running: true });
  assert.equal(c.disabled, true);
});

function row(overrides: Partial<ConstructionRow>): ConstructionRow {
  return {
    activityId: 'C-x',
    classified: true,
    hasBuildEvidence: false,
    recorded: false,
    phases: [],
    attempts: [],
    ...overrides,
  };
}

void test('a Begin names exactly the unrecorded rows, sorted, with titles where the list has one', () => {
  const rows = {
    'N-IT': row({ activityId: 'N-IT' }),
    'C-done': row({ activityId: 'C-done', recorded: true, hasBuildEvidence: true }),
    // Recorded but with no evidence yet (started, no phase): under way, not a candidate.
    'C-started': row({ activityId: 'C-started', recorded: true, hasBuildEvidence: false }),
    'C-billing-state-access': row({ activityId: 'C-billing-state-access' }),
  };
  const titles: Record<string, string> = { 'N-IT': 'System testing' };
  const got = notStartedActivities(rows, (id) => titles[id]);
  assert.deepEqual(got, [
    { activityId: 'C-billing-state-access' },
    { activityId: 'N-IT', title: 'System testing' },
  ]);
  assert.deepEqual(
    notStartedActivities(undefined, () => undefined),
    [],
    'no rows yet is nothing to name'
  );
});
