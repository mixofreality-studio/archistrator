/**
 * phaseStateFor / progressPct — the SPA's ONLY per-phase done/active derivation.
 *
 * This file exists because the helper had no tests at all while it was inferring
 * `done` by ordinal position ("everything before currentPhase is done", plus
 * "integrated means everything is done") and `ConstructionRow.phases` — the server's
 * real per-phase completion — had zero consumers in the SPA. The inference was inert
 * only by luck: completion can be NON-MONOTONIC (a committed row has carried that
 * shape), and there ordinal inference reports the opposite of the truth for the
 * first phase. These tests pin that `done` is READ and never derived.
 */
/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import type { PhaseRow } from '../../contracts/types.ts';
import { phaseStateFor, progressPct } from './lifecycleTemplates.ts';
import { GENERATED_TEMPLATES, GENERATED_TESTING_VARIANTS } from './lifecycleTemplates.gen.ts';

function phaseRow(phase: string, completed: boolean, weight = 20): PhaseRow {
  return { phase, weight, label: phase, completed };
}

void test('phaseStateFor reads the server per-phase completions', () => {
  const phases = [
    phaseRow('requirements', true, 15),
    phaseRow('detailed_design', true, 25),
    phaseRow('test_plan', false, 10),
    phaseRow('construction', false, 35),
    phaseRow('integration', false, 15),
  ];
  const got = phaseStateFor('frontend', 'construction', phases);

  assert.deepEqual(
    got.map((p) => [p.phase, p.done]),
    [
      ['requirements', true],
      ['detailed_design', true],
      ['test_plan', false],
      ['construction', false],
      ['integration', false],
    ]
  );
  // `active` is the REAL current phase, and only that one.
  assert.deepEqual(
    got.filter((p) => p.active).map((p) => p.phase),
    ['construction']
  );
});

void test('phaseStateFor marks nothing done when the server reports no phases', () => {
  for (const phases of [undefined, []]) {
    const got = phaseStateFor('service', 'construction', phases);
    assert.ok(got.length > 0, 'the template still renders its rows');
    assert.equal(
      got.some((p) => p.done),
      false,
      'absence of phase data must render as absence, never as a plausible guess'
    );
    assert.equal(progressPct(got), 0);
  }
});

// The regression this whole finding is about. The non-monotonic shape: `requirements`
// INCOMPLETE with every later phase complete. Ordinal inference ("everything before
// the current phase is done") gets the first row exactly backwards, and the old
// `integrated` short-circuit marked all five done regardless of the data.
void test('phaseStateFor preserves NON-MONOTONIC completion', () => {
  const phases = [
    phaseRow('requirements', false, 15),
    phaseRow('detailed_design', true, 25),
    phaseRow('test_plan', true, 10),
    phaseRow('construction', true, 35),
    phaseRow('integration', true, 15),
  ];
  const got = phaseStateFor('frontend', 'integration', phases);

  const requirements = got.find((p) => p.phase === 'requirements');
  assert.ok(requirements !== undefined, 'the frontend template carries requirements');
  assert.equal(
    requirements.done,
    false,
    'requirements is INCOMPLETE on the server; no amount of later progress makes it done'
  );
  assert.deepEqual(
    got.filter((p) => !p.done).map((p) => p.phase),
    ['requirements']
  );
  // Σ weights of done phases — 100 minus the incomplete requirements phase.
  assert.equal(progressPct(got), 85);
});

// A phase the server reported that this kind's template does not carry contributes
// nothing, and a template phase the server did not report stays not-done: the join is
// by canonical phase id, never positional.
void test('phaseStateFor joins by phase id, not by position', () => {
  // uiDesign carries only requirements + detailed_design.
  const got = phaseStateFor('uiDesign', undefined, [
    phaseRow('construction', true, 40),
    phaseRow('detailed_design', true, 60),
  ]);

  assert.deepEqual(
    got.map((p) => [p.phase, p.done]),
    [
      ['requirements', false],
      ['detailed_design', true],
    ]
  );
  assert.equal(
    got.some((p) => p.active),
    false,
    'no current phase reported → nothing active'
  );
});

// Fix round B (designer P1-7): the exit criterion and the task labels are the
// PROFILE's, generated from the server — never one generic sentence per canonical
// phase shared by every kind.
void test('the same canonical phase states a different exit for a different profile', () => {
  const exitOf = (phases: typeof GENERATED_TEMPLATES.service, phase: string): string =>
    phases.find((p) => p.phase === phase)?.exitCriterion ?? '';
  const svc = GENERATED_TEMPLATES.service;
  assert.notEqual(
    exitOf(GENERATED_TESTING_VARIANTS.plan, 'construction'),
    exitOf(svc, 'construction')
  );
  assert.notEqual(
    exitOf(GENERATED_TESTING_VARIANTS.systemTest, 'requirements'),
    exitOf(svc, 'requirements')
  );
  for (const phases of [
    ...Object.values(GENERATED_TEMPLATES),
    ...Object.values(GENERATED_TESTING_VARIANTS),
  ]) {
    for (const p of phases) assert.ok(p.exitCriterion.length > 0, `${p.id} has no exit criterion`);
  }
});

void test('task KEYS never vary by profile — only their labels do', () => {
  const keysOf = (phases: typeof GENERATED_TEMPLATES.service): string[] =>
    phases.flatMap((p) => p.tasks.map((tk) => `${p.phase}:${tk.task}`));
  const svcKeys = new Set(keysOf(GENERATED_TEMPLATES.service));
  for (const phases of [
    ...Object.values(GENERATED_TEMPLATES),
    ...Object.values(GENERATED_TESTING_VARIANTS),
  ]) {
    for (const key of keysOf(phases)) assert.ok(svcKeys.has(key), `${key} is not a Figure A-1 key`);
  }
  // The frontend's Flows phase is not an STP, though its keys are stp/stpReview.
  const flows = GENERATED_TEMPLATES.frontend.find((p) => p.phase === 'test_plan');
  assert.ok(flows !== undefined);
  assert.deepEqual(
    flows.tasks.map((tk) => tk.task),
    ['stp', 'stpReview']
  );
  assert.ok(flows.tasks.every((tk) => tk.label !== tk.bookLabel));
});
