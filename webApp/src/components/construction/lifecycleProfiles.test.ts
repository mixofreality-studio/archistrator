/**
 * The construction console's profile view over the method-assets lifecycles.
 *
 * While BOTH generated tables exist, this pins the adapter against the one the
 * console renders from today: every phase and every task row, field for field.
 * Task 4 deletes lifecycleTemplates.gen.ts and with it this comparison; the
 * invariants below it (sub-attempt rows, book labels, canonical keys) stay.
 */
/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import { GENERATED_TEMPLATES, GENERATED_TESTING_VARIANTS } from './lifecycleTemplates.gen.ts';
import { profileFor, SERVICE_PROFILE } from './lifecycleProfiles.ts';
import type { ActivityKind } from './KindBadge';
import type { TestingVariantName } from '../../contracts/types';

const KINDS: readonly ActivityKind[] = [
  'service',
  'frontend',
  'testing',
  'deployment',
  'documentation',
  'uiDesign',
  'integration',
];
const VARIANTS: readonly TestingVariantName[] = [
  'plan',
  'harness',
  'perf',
  'systemTest',
  'qaProcess',
];

void test('every kind profile equals the table the console renders today', () => {
  for (const kind of KINDS) {
    assert.deepEqual(profileFor(kind, undefined), GENERATED_TEMPLATES[kind], kind);
  }
});

void test('every testing variant profile equals the table it renders today', () => {
  for (const variant of VARIANTS) {
    assert.deepEqual(profileFor('testing', variant), GENERATED_TESTING_VARIANTS[variant], variant);
  }
});

void test('an unclassified row has no profile', () => {
  assert.equal(profileFor(undefined, undefined), undefined);
  assert.equal(profileFor(undefined, 'perf'), undefined);
});

void test('SERVICE_PROFILE is the canonical five in Method order', () => {
  assert.deepEqual(
    SERVICE_PROFILE.map((p) => p.phase),
    ['requirements', 'detailed_design', 'test_plan', 'construction', 'integration']
  );
});

void test('the two sub-attempt rows are carried, in place, and flagged', () => {
  const design = SERVICE_PROFILE.find((p) => p.phase === 'detailed_design');
  const construction = SERVICE_PROFILE.find((p) => p.phase === 'construction');
  assert.ok(design !== undefined && construction !== undefined);
  assert.deepEqual(
    design.tasks.map((t) => t.task),
    ['someConstruction', 'detailedDesign', 'designReview']
  );
  assert.deepEqual(
    construction.tasks.map((t) => t.task),
    ['construction', 'testClient', 'codeReview']
  );
  assert.deepEqual(
    design.tasks.map((t) => t.conditional),
    [true, false, false]
  );
  // A sub-attempt is no lifecycle node, so it keeps the book's own name.
  const someConstruction = design.tasks[0];
  assert.ok(someConstruction !== undefined);
  assert.equal(someConstruction.label, 'Some Construction');
  assert.equal(someConstruction.bookLabel, 'Some Construction');
});

void test('exactly one gate per phase, and the labels are the profile’s own', () => {
  const flows = profileFor('frontend', undefined)?.find((p) => p.phase === 'test_plan');
  assert.ok(flows !== undefined);
  // The frontend's Flows phase is not an STP, though its keys are stp/stpReview.
  assert.deepEqual(
    flows.tasks.map((t) => t.task),
    ['stp', 'stpReview']
  );
  assert.ok(flows.tasks.every((t) => t.label !== t.bookLabel));
  for (const kind of KINDS) {
    for (const p of profileFor(kind, undefined) ?? []) {
      assert.equal(p.tasks.filter((t) => t.gate).length, 1, `${kind}/${p.phase}`);
      assert.ok(p.exitCriterion.length > 0, `${kind}/${p.phase}`);
      assert.ok(p.id.length > 0, `${kind}/${p.phase}`);
    }
  }
});
