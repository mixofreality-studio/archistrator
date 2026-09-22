/**
 * The construction console's profile view over the method-assets lifecycles.
 *
 * This pins the invariants the console's rendering depends on — sub-attempt rows,
 * book labels, canonical keys — read through the exported adapter, not a generated
 * comparison table (lifecycleTemplates.gen.ts is gone; the console reads
 * lifecycles.gen.ts, method-assets' own data, directly).
 */
/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import { profileFor, SERVICE_PROFILE } from './lifecycleProfiles.ts';
import type { ActivityKind } from './KindBadge';

/** Every ActivityKind, the three design kinds included — the adapter must be total. */
const KINDS: readonly ActivityKind[] = [
  'service',
  'frontend',
  'testing',
  'deployment',
  'documentation',
  'uiDesign',
  'integration',
  'requirements',
  'architecture',
  'projectDesign',
];

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
      // A gate-only phase (projectDesign's SDP · M0 review) dispatches nothing, so it
      // has no command cell to name; every phase that DOES carry work has one.
      const dispatches = p.tasks.some((t) => !t.gate && !t.conditional);
      assert.equal(p.id.length > 0, dispatches, `${kind}/${p.phase}`);
    }
  }
});
