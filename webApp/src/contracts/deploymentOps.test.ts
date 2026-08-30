/// <reference types="node" />
/**
 * Parse checks run against the REAL committed slot JSON (archistrator's own
 * project.json) rather than a hand-built fixture — so a drift between what the
 * surviving views read and the migrated ground truth fails here instead of
 * rendering an empty screen at runtime:
 *
 *   • Required Behaviors (slot 2): B-NN ids + `statement` + nullable `statedAs`
 *     provenance + `volatilityHint`. The Required-Behaviors STEP is retired, but the
 *     committed slot is still read — GlossaryView's cross-artifact term-usage join
 *     builds its "Behaviors" corpus from `items[].statement` (glossaryLogic
 *     buildUsageCorpus), and toMarkdown still projects the kind — so the shape check
 *     stays.
 *   • Deployment & Operations Model (slot 6): the surviving deployment topology,
 *     which the Architecture step's Deployment lens renders through
 *     listDeploymentProfiles / toDeploymentView / DeploymentFlow.
 *
 * The per-project SELECTIONS (knobs / trust summaries / infra blocks / objectiveLinks)
 * are no longer projected anywhere: the Deployment & Operations page is gone and
 * deploymentOpsLogic.ts went with it, so the toDeploymentOperationsView / KNOB_LABELS
 * / linkedObjectives cases that used to live here were removed with their subjects.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import type { ArtifactModelEnvelope, Requirement } from './types.ts';

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
