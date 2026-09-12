import { test } from 'node:test';
import assert from 'node:assert/strict';
import type { ConstructionRow } from '../../../contracts/types';
import { beginControlFor, notStartedActivities, type StartedAnswer } from './beginControl.ts';

const COMMITTED_WORDS = /Begin construction|Resume construction/;

void test('while the project or a session probe is loading, the button is disabled and names neither Begin nor Resume', () => {
  const answers: StartedAnswer[] = ['loading', 'started', 'notStarted', 'unknown'];
  for (const started of answers) {
    const c = beginControlFor({ started, projectLoading: true, running: false });
    assert.equal(c.disabled, true, `projectLoading + ${started}`);
    assert.doesNotMatch(c.label, COMMITTED_WORDS, `projectLoading + ${started}`);
  }
  const probing = beginControlFor({ started: 'loading', projectLoading: false, running: false });
  assert.equal(probing.disabled, true);
  assert.doesNotMatch(probing.label, COMMITTED_WORDS);
});

void test('the label is the session answer: started is Resume, notStarted is Begin', () => {
  const resume = beginControlFor({ started: 'started', projectLoading: false, running: false });
  assert.equal(resume.label, 'Resume construction');
  assert.equal(resume.disabled, false);
  const begin = beginControlFor({ started: 'notStarted', projectLoading: false, running: false });
  assert.equal(begin.label, 'Begin construction');
  assert.equal(begin.disabled, false);
});

void test('an unknown answer claims neither word alone', () => {
  const c = beginControlFor({ started: 'unknown', projectLoading: false, running: false });
  assert.equal(c.label, 'Begin or resume construction');
  assert.equal(c.disabled, false);
});

void test('a run in flight disables the button whatever the session says', () => {
  const c = beginControlFor({ started: 'notStarted', projectLoading: false, running: true });
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
