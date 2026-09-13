import { test } from 'node:test';
import assert from 'node:assert/strict';
import { caseKindInk, stepStatusFor } from './scenarioInk.ts';

// Designer final items: a never-run N-IT names its targets in NEUTRAL ink.
void test('never run: negative and boundary chips are neutral, and every call is a neutral target', () => {
  assert.equal(caseKindInk('negative', 'notRun'), 'neutral');
  assert.equal(caseKindInk('boundary', 'notRun'), 'neutral');
  assert.equal(caseKindInk('happy', 'notRun'), 'good');
  assert.equal(stepStatusFor('notRun', undefined), 'planned');
  assert.equal(stepStatusFor('notRun', 'green'), 'planned');
});

void test('the plan (N-STP) keeps its red targets; a run colours by what was recorded', () => {
  assert.equal(caseKindInk('negative', 'plan'), 'danger');
  assert.equal(caseKindInk('boundary', 'plan'), 'caution');
  assert.equal(caseKindInk('negative', 'run'), 'danger');
  assert.equal(stepStatusFor('plan', 'green'), 'red');
  assert.equal(stepStatusFor('run', 'green'), 'green');
  assert.equal(stepStatusFor('run', 'red'), 'red');
  assert.equal(stepStatusFor('run', undefined), 'red');
});
