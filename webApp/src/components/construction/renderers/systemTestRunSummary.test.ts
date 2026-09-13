/**
 * N-IT's run summary (systemTestRunSummary.ts, designer P1-12): never-run is "not
 * run · N scenarios planned" with muted chips and no tile; red only for a step that
 * recorded red.
 */
/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import type { ConstructionRow, TestScenarioView, TestStepView } from '../../../contracts/types.ts';
import {
  scenarioChipFor,
  scenarioRunStatus,
  systemTestRunSummaryFor,
} from './systemTestRunSummary.ts';

function step(status?: string): TestStepView {
  return {
    seq: 1,
    component: 'systemDesignManager',
    operation: 'op',
    ...(status !== undefined ? { status } : {}),
    inputs: null,
    expect: { errorExpected: false },
  };
}

function scenario(id: string, statuses: (string | undefined)[][]): TestScenarioView {
  return {
    id,
    useCase: 'uc',
    title: id,
    cases: statuses.map((steps, i) => ({
      id: `${id}-c${String(i)}`,
      kind: 'happy',
      title: 'case',
      steps: steps.map(step),
    })),
  };
}

function row(attempts: number): ConstructionRow {
  return {
    activityId: 'N-IT',
    classified: true,
    hasBuildEvidence: false,
    recorded: attempts > 0,
    phases: [],
    attempts: Array.from({ length: attempts }, (_, i) => ({
      attemptId: `N-IT:srs:${String(i + 1)}`,
      task: 'srs',
      phase: 'requirements',
      attempt: i + 1,
      outcome: '' as const,
      evidence: { kind: '', ref: '' },
      provenance: { origin: 'observed' as const },
    })),
  };
}

void test('an unrun scenario is "not run", never "failing"', () => {
  assert.equal(scenarioRunStatus(scenario('S1', [[undefined, undefined], [undefined]])), 'notRun');
  assert.equal(scenarioRunStatus(scenario('S1', [['', '']])), 'notRun');
  assert.deepEqual(scenarioChipFor('notRun'), { label: 'not run', tone: 'muted' });
});

void test('red is reserved for a step that recorded red', () => {
  assert.equal(scenarioRunStatus(scenario('S1', [['green', 'red']])), 'failing');
  assert.equal(scenarioRunStatus(scenario('S1', [['green', 'green'], ['green']])), 'green');
  assert.equal(scenarioRunStatus(scenario('S1', [['green', undefined]])), 'partial');
  assert.equal(scenarioChipFor('failing').tone, 'bad');
  assert.equal(scenarioChipFor('partial').tone, 'muted');
});

void test('never attempted: "not run · N scenarios planned", and no green/total tile', () => {
  const scenarios = [scenario('S1', [[undefined]]), scenario('S2', [[undefined]])];
  const s = systemTestRunSummaryFor(scenarios, undefined);
  assert.equal(s.attempted, false);
  assert.equal(s.headline, 'not run · 2 scenarios planned');
  assert.equal(s.tile, undefined);
  assert.equal(
    systemTestRunSummaryFor([scenario('S1', [[]])], row(0)).headline,
    'not run · 1 scenario planned'
  );
});

// Fix-B review M4: a reconstructed attempt is not a run. It must not turn a never-
// run system test into "attempted" and bring back the red green/total tile.
void test('a reconstructed attempt alone is not a run: still "not run", still no tile', () => {
  const r = row(2);
  const reconstructed: ConstructionRow = {
    ...r,
    attempts: r.attempts.map((a) => ({ ...a, provenance: { origin: 'backfilled' as const } })),
  };
  const s = systemTestRunSummaryFor([scenario('S1', [[undefined]])], reconstructed);
  assert.equal(s.attempted, false);
  assert.equal(s.tile, undefined);
  assert.equal(s.headline, 'not run · 1 scenario planned');
});

void test('once anything was attempted, the tile reports green/total', () => {
  const scenarios = [scenario('S1', [['green']]), scenario('S2', [[undefined]])];
  const s = systemTestRunSummaryFor(scenarios, undefined);
  assert.equal(s.attempted, true);
  assert.deepEqual(s.tile, { label: 'scenarios green', value: '1/2', tone: 'bad' });
  // An attempt on the N-IT row counts even before any step records a status.
  assert.equal(systemTestRunSummaryFor([scenario('S1', [[undefined]])], row(1)).attempted, true);
});
