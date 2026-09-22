/**
 * The generated lifecycles (lifecycles.gen.ts, from server/cmd/gen-lifecycles, which
 * reads the method-assets release pinned in server/go.mod): what every consumer may
 * assume about them.
 *
 * Structural validity is proven where the data is authored (method-assets'
 * ValidateLifecycle) and again by the generator; these pins are about what reaches
 * the webApp — the shapes the graph draws and the key rule callers use.
 * lifecycleTemplates.gen.ts, the generated table this file used to agree against, is
 * gone (stage 2): the construction console reads this file's own data directly through
 * `construction/lifecycleProfiles.ts` now, so there is nothing left to compare.
 */
/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import { LIFECYCLES, lifecycleFor, type LifecycleDef } from './lifecycles.gen.ts';

function mustLifecycle(typeKey: string): LifecycleDef {
  const found = lifecycleFor(typeKey);
  assert.ok(found !== undefined, `no lifecycle for ${typeKey}`);
  return found;
}

const dependsOnOf = (l: LifecycleDef, taskId: string): readonly string[] | undefined =>
  l.tasks.find((t) => t.id === taskId)?.dependsOn;

void test('the type keys are the closed, ordered set the platform ships', () => {
  assert.deepEqual(
    LIFECYCLES.map((l) => l.type),
    [
      'requirements',
      'architecture',
      'projectDesign',
      'service',
      'frontend',
      'testing:plan',
      'testing:harness',
      'testing:perf',
      'testing:systemTest',
      'testing:qaProcess',
      'deployment',
      'documentation',
      'uiDesign',
      'integration',
    ]
  );
  assert.equal(lifecycleFor('nope'), undefined);
});

void test('every lifecycle is drawable: weights total 100, gates review, edges point back', () => {
  for (const l of LIFECYCLES) {
    const total = l.phases.reduce((sum, p) => sum + p.weight, 0);
    assert.equal(total, 100, `${l.type}: phase weights`);
    const earlier = new Set<string>();
    for (const t of l.tasks) {
      for (const dep of t.dependsOn) {
        assert.ok(earlier.has(dep), `${l.type}: ${t.id} depends on ${dep}, not an earlier task`);
      }
      earlier.add(t.id);
    }
    for (const p of l.phases) {
      const gate = l.tasks.find((t) => t.id === p.gate);
      assert.ok(gate !== undefined, `${l.type}: phase ${p.id} has no gate task`);
      assert.equal(gate.kind, 'review', `${l.type}: gate ${gate.id}`);
      assert.equal(gate.phase, p.id, `${l.type}: gate ${gate.id}`);
    }
  }
});

void test('service and frontend fork after the first gate and rejoin at testing (Figure A-1)', () => {
  for (const key of ['service', 'frontend']) {
    const l = mustLifecycle(key);
    assert.deepEqual(dependsOnOf(l, 'detailedDesign'), ['srsReview'], key);
    assert.deepEqual(dependsOnOf(l, 'stp'), ['srsReview'], key);
    assert.deepEqual(dependsOnOf(l, 'testing'), ['integration', 'stpReview'], key);
    // Authored order decides the trunk: the design→construction chain stays on lane 0.
    const ids = l.tasks.map((t) => t.id);
    assert.ok(
      ids.indexOf('detailedDesign') < ids.indexOf('stp'),
      `${key}: trunk is authored first`
    );
  }
});

void test('a conditional sub-attempt is never a node', () => {
  for (const l of LIFECYCLES) {
    for (const t of l.tasks) {
      assert.ok(t.id !== 'someConstruction' && t.id !== 'testClient', `${l.type}: ${t.id}`);
    }
  }
});

void test('project design is one review and nothing to dispatch', () => {
  const l = mustLifecycle('projectDesign');
  assert.equal(l.tasks.length, 1);
  const [gate] = l.tasks;
  assert.ok(gate !== undefined);
  assert.equal(gate.kind, 'review');
  assert.equal(gate.reviews, undefined);
  assert.equal(gate.command, undefined);
  assert.equal(gate.artifactKind, 'SdpReview');
  assert.deepEqual(gate.dependsOn, []);
});

void test('the requirements weights are 15/20/35/30 and architecture is one pair', () => {
  assert.deepEqual(
    mustLifecycle('requirements').phases.map((p) => [p.id, p.weight]),
    [
      ['mission', 15],
      ['glossary', 20],
      ['volatilities', 35],
      ['coreUseCases', 30],
    ]
  );
  const arch = mustLifecycle('architecture');
  assert.deepEqual(
    arch.tasks.map((t) => [t.id, t.kind]),
    [
      ['architectureDraft', 'dispatch'],
      ['architectureReview', 'review'],
    ]
  );
});

void test('a review shares its revision group with the dispatch it judges', () => {
  for (const l of LIFECYCLES) {
    for (const t of l.tasks) {
      assert.equal(t.revisionGroup, t.reviews ?? t.id, `${l.type}: ${t.id}`);
      if (t.reviews === undefined) continue;
      const judged = l.tasks.find((d) => d.id === t.reviews);
      assert.ok(judged !== undefined, `${l.type}: ${t.id} reviews a task that does not exist`);
      assert.equal(judged.kind, 'dispatch', `${l.type}: ${t.id}`);
      assert.equal(judged.revisionGroup, t.revisionGroup, `${l.type}: ${t.id}`);
    }
  }
});
