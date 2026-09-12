import { test } from 'node:test';
import assert from 'node:assert/strict';
import type { ConstructionRow } from '../../../../contracts/types.ts';
import { briefingFor, unknownTitleFor } from './taskBriefing.ts';

const row: ConstructionRow = {
  activityId: 'U-SPA-web-client',
  kind: 'frontend',
  classified: true,
  hasBuildEvidence: false,
  recorded: false,
  phases: [],
  attempts: [],
};

void test('an activity selection is headed "This activity", never "This task"', () => {
  const sel = { activityId: 'U-SPA-web-client' };
  assert.equal(unknownTitleFor(briefingFor(row, sel), sel), 'This activity');
});

void test('a phase selection is headed by the phase name, a task selection by the task label', () => {
  const phaseSel = { activityId: 'U-SPA-web-client', lifecyclePhase: 'requirements' };
  const phaseBriefing = briefingFor(row, phaseSel);
  assert.ok(phaseBriefing !== undefined);
  assert.equal(unknownTitleFor(phaseBriefing, phaseSel), phaseBriefing.title);
  assert.equal(phaseBriefing.scope, 'lifecyclePhase');

  const taskSel = { ...phaseSel, task: 'srsReview' };
  const taskBriefing = briefingFor(row, taskSel);
  assert.ok(taskBriefing !== undefined);
  assert.equal(unknownTitleFor(taskBriefing, taskSel), taskBriefing.title);
  assert.equal(taskBriefing.scope, 'task');
});

void test('with no profile to resolve against, the raw task or phase key is the heading', () => {
  assert.equal(
    unknownTitleFor(undefined, { activityId: 'X', lifecyclePhase: 'integration', task: 'testing' }),
    'testing'
  );
  assert.equal(
    unknownTitleFor(undefined, { activityId: 'X', lifecyclePhase: 'integration' }),
    'integration'
  );
});
