/// <reference types="node" />
/**
 * Parse checks for the Wave-2 reshaped surfaces, run against the REAL committed slot
 * JSON (archistrator's own project.json) rather than a hand-built fixture — so a drift
 * between what the views read and the migrated ground truth fails here instead of
 * rendering an empty screen at runtime:
 *
 *   • Required Behaviors (slot 2): B-NN ids + `behavior` (renamed from `statement`) +
 *     nullable `statedAs` provenance + `volatilityHint`, and
 *   • Deployment & Operations Model (slot 6): the per-project selections + trust
 *     summaries + infra blocks projected by toDeploymentOperationsView, plus the
 *     surviving deployment topology.
 *
 * toDeploymentOperationsView is imported from its leaf logic module (adapters.ts is
 * not node-loadable — extensionless transitive imports); the required-behaviors and
 * topology shapes are asserted directly against the typed slot JSON.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import type { ArtifactModelEnvelope, Objective, Requirement } from './types.ts';
import {
  KNOB_LABELS,
  linkedObjectives,
  realizingKnobs,
  toDeploymentOperationsView,
} from './deploymentOpsLogic.ts';

interface Slot {
  kind: number;
  model: unknown;
}
type EnvModel = NonNullable<ArtifactModelEnvelope['model']>;
const state = JSON.parse(
  readFileSync(new URL('../../../.aiarch/state/project.json', import.meta.url), 'utf8')
) as { slots: Record<string, Slot | undefined> };

function slotModel(key: string): EnvModel {
  const slot = state.slots[key];
  if (slot === undefined) throw new Error(`missing slot ${key}`);
  return slot.model as EnvModel;
}

const depOpsEnvelope: ArtifactModelEnvelope = {
  kind: 'operationalConcepts',
  model: slotModel('6'),
};

void test('slot 2 carries the migrated Required-Behaviors shape', () => {
  const items = (slotModel('2') as { items: Requirement[] | null }).items ?? [];
  assert.ok(items.length > 0, 'expected committed behaviors');
  assert.equal(items[0]?.id, 'B-01');
  for (const it of items) {
    assert.match(it.id, /^B-\d{2}$/, `id ${it.id} is not a B-NN`);
    assert.ok(it.statement.length > 0, `behavior ${it.id} is empty`);
    // The interim `behavior` field name reverted to `statement` (arch2 Path B) — the
    // old name must be gone so the view/adapter can't silently read a stale field.
    assert.equal((it as Record<string, unknown>)['behavior'], undefined);
    // statedAs / volatilityHint are nullable provenance — arrays of strings when present.
    if (it.statedAs != null) assert.ok(Array.isArray(it.statedAs));
    if (it.volatilityHint != null) assert.ok(Array.isArray(it.volatilityHint));
  }
  // At least one behavior carries provenance and at least one carries a hint (proves
  // the two new columns actually flow, not just tolerate-null).
  assert.ok(
    items.some((i) => (i.statedAs?.length ?? 0) > 0),
    'no statedAs provenance found'
  );
  assert.ok(
    items.some((i) => (i.volatilityHint?.length ?? 0) > 0),
    'no volatilityHint found'
  );
});

void test('toDeploymentOperationsView reads the per-project selections + trust + infra', () => {
  const v = toDeploymentOperationsView(depOpsEnvelope);
  assert.ok(v !== undefined, 'expected a committed deployment-operations model');
  assert.equal(v.deploymentScenario, 'deployedOperated');
  assert.equal(v.constructionVenue.kind, 'customerCI');
  assert.equal(v.constructionVenue.repositoryHost, 'GitHub');
  assert.equal(v.reviewPolicyRef, 'vibes');
  // scalingPolicy present under deployedOperated, refined off the loose `unknown`.
  assert.ok(v.scalingPolicy !== undefined);
  assert.equal(v.scalingPolicy.scaleToZero, true);
  assert.equal(v.scalingPolicy.targetUtilizationPct, 70);
  assert.ok(v.infraBuildingBlocks.length > 0);
  for (const b of v.infraBuildingBlocks) {
    assert.ok(b.name.length > 0 && b.category.length > 0);
  }
  // All three customer trust summaries are non-empty (the ratifiable trust tier).
  assert.ok(v.trustSummaries.billing.length > 0);
  assert.ok(v.trustSummaries.usageMetering.length > 0);
  assert.ok(v.trustSummaries.dataOwnership.length > 0);
});

void test('toDeploymentOperationsView is safe-empty on an absent model', () => {
  assert.equal(toDeploymentOperationsView(undefined), undefined);
  assert.equal(toDeploymentOperationsView({ kind: 'operationalConcepts' }), undefined);
});

// ── objectiveLinks traceability (Righting Software ch. 5) ────────────────────

/** The committed mission objectives (slot 0) — the join target for the links. */
const missionObjectives: Objective[] =
  (slotModel('0') as { objectives: Objective[] | null }).objectives ?? [];

void test('toDeploymentOperationsView carries objectiveLinks through (real slot 6)', () => {
  const v = toDeploymentOperationsView(depOpsEnvelope);
  assert.ok(v !== undefined);
  assert.ok(v.objectiveLinks !== undefined, 'expected committed objectiveLinks');
  assert.deepEqual(v.objectiveLinks['deploymentScenario'], [3, 10]);
  assert.deepEqual(v.objectiveLinks['infraBuildingBlocks'], [2, 6, 9, 10]);
});

void test('linkedObjectives joins knob link numbers onto the mission objectives', () => {
  const v = toDeploymentOperationsView(depOpsEnvelope);
  assert.ok(v !== undefined);
  const chips = linkedObjectives(v.objectiveLinks, 'deploymentScenario', missionObjectives);
  assert.deepEqual(
    chips.map((c) => c.number),
    [3, 10]
  );
  for (const c of chips) {
    assert.ok(c.statement.length > 0, `objective ${String(c.number)} statement missing`);
  }
});

void test('linkedObjectives degrades gracefully (absent links / unknown objective)', () => {
  // Older committed states carry no objectiveLinks at all → render nothing.
  assert.deepEqual(linkedObjectives(undefined, 'deploymentScenario', missionObjectives), []);
  // A knob with no entry (or a null entry) → nothing.
  assert.deepEqual(linkedObjectives({}, 'scalingPolicy', missionObjectives), []);
  assert.deepEqual(
    linkedObjectives({ scalingPolicy: null }, 'scalingPolicy', missionObjectives),
    []
  );
  // Duplicates collapse; a number with no matching objective keeps its chip with an
  // empty statement (the link is still rendered, just tooltip-less).
  const chips = linkedObjectives(
    { reviewPolicyRef: [7, 7, 99] },
    'reviewPolicyRef',
    missionObjectives
  );
  assert.deepEqual(
    chips.map((c) => c.number),
    [7, 99]
  );
  assert.ok((chips[0]?.statement.length ?? 0) > 0);
  assert.equal(chips[1]?.statement, '');
});

void test('realizingKnobs computes the reverse join in canonical knob order', () => {
  const v = toDeploymentOperationsView(depOpsEnvelope);
  assert.ok(v !== undefined);
  // Objective 10 is cited by scenario + scaling + infra in the real committed state.
  assert.deepEqual(realizingKnobs(v.objectiveLinks, 10), [
    'deploymentScenario',
    'scalingPolicy',
    'infraBuildingBlocks',
  ]);
  // Objective 3 → scenario + venue.
  assert.deepEqual(realizingKnobs(v.objectiveLinks, 3), [
    'deploymentScenario',
    'constructionVenue',
  ]);
  // An objective no knob cites → nothing (no warning — coverage is the server's job).
  assert.deepEqual(realizingKnobs(v.objectiveLinks, 1), []);
  // Absent links (older states) → nothing.
  assert.deepEqual(realizingKnobs(undefined, 3), []);
});

void test('every knob has a human label for the realized-by chips', () => {
  assert.equal(KNOB_LABELS.deploymentScenario, 'Deployment scenario');
  assert.equal(KNOB_LABELS.constructionVenue, 'Construction venue');
  assert.equal(KNOB_LABELS.reviewPolicyRef, 'Review policy');
  assert.equal(KNOB_LABELS.scalingPolicy, 'Scaling');
  assert.equal(KNOB_LABELS.infraBuildingBlocks, 'Infrastructure building blocks');
});

void test('the deployment topology survives the reshape (cloud + local profiles)', () => {
  const deployment = (
    slotModel('6') as {
      deployment: { environments: { profile: string; nodes: unknown[] | null }[] | null };
    }
  ).deployment;
  const profiles = (deployment.environments ?? []).map((e) => e.profile);
  assert.ok(profiles.includes('cloud'), 'cloud profile missing');
  assert.ok(profiles.includes('local'), 'local profile missing');
  const cloud = (deployment.environments ?? []).find((e) => e.profile === 'cloud');
  assert.ok((cloud?.nodes ?? []).length > 0, 'cloud topology should have nodes');
});
