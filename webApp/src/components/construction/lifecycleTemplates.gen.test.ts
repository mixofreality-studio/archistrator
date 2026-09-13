/**
 * The generated life-cycle templates (lifecycleTemplates.gen.ts, from
 * server/cmd/gen-uiprofiles): what every consumer may assume about them.
 *
 * These two pins used to live in lifecycleTemplates.test.ts beside the tests of
 * `phaseStateFor`. That helper and its module had no production importer left, so
 * both went; these pins are about the generated data, not the helper, and stay.
 */
/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import { GENERATED_TEMPLATES, GENERATED_TESTING_VARIANTS } from './lifecycleTemplates.gen.ts';

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
