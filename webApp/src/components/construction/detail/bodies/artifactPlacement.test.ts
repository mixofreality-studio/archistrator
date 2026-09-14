/**
 * The designer's placement table (renderers-placement.md §2.2–§2.4) and the
 * frame's honesty rules (§1, §4, §5), pinned without a renderer.
 */
/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import type { C4Component } from '../../../../contracts/adapters.ts';
import type { ConstructionRow, ServiceContract } from '../../../../contracts/types.ts';
import type { ContractJoin } from '../../../../contracts/serviceContracts.ts';
import type { LensSelection } from '../../lens/useLensSelection.ts';
import {
  artifactRoleFor,
  byDesignSentence,
  contractSourceLine,
  isPrimaryPlacement,
  missingContractSentence,
  noTestPlanSentence,
  placementFor,
  reconstructedArtifactNote,
  REFERENCE_LINE_CONSTRUCTION,
  REFERENCE_LINE_DESIGN,
  type PlacementKind,
} from './artifactPlacement.ts';
import { detailBodyFor } from './bodyDispatch.ts';

function row(kind: ConstructionRow['kind']): ConstructionRow {
  return {
    activityId: 'X',
    classified: true,
    hasBuildEvidence: false,
    recorded: false,
    ...(kind !== undefined ? { kind } : {}),
    phases: [],
    attempts: [],
  };
}

const component: C4Component = {
  id: 'construction-manager',
  name: 'ConstructionManager',
  kind: 'manager',
  layer: 'manager',
  encapsulates: '',
  encapsulatesVolatilities: [],
  contractKey: 'constructionManager',
};
const contract = { component: 'constructionManager', layer: 'Manager' } as ServiceContract;
const CONTRACT: ContractJoin = {
  kind: 'contract',
  componentId: component.id,
  component,
  contractKey: 'constructionManager',
  contract,
};
const MISSING: ContractJoin = { kind: 'missing', componentId: 'design-health-engine', component };
const BY_DESIGN: ContractJoin = {
  kind: 'byDesign',
  componentId: 'github',
  component: { ...component, id: 'github', kind: 'resource', contractKey: '' },
};

function at(
  kind: ConstructionRow['kind'],
  join: ContractJoin,
  selection: Omit<LensSelection, 'activityId'>
): PlacementKind {
  return placementFor(row(kind), { activityId: 'X', ...selection }, join).kind;
}

void test('service: the per-phase/task table', () => {
  assert.equal(at('service', CONTRACT, {}), 'contractSummary');
  assert.equal(at('service', CONTRACT, { lifecyclePhase: 'requirements' }), 'none');
  assert.equal(at('service', CONTRACT, { task: 'srs' }), 'none');
  assert.equal(at('service', CONTRACT, { task: 'srsReview' }), 'srsUnreadable');
  assert.equal(at('service', CONTRACT, { lifecyclePhase: 'detailed_design' }), 'contractFull');
  assert.equal(at('service', CONTRACT, { task: 'detailedDesign' }), 'contractFull');
  assert.equal(at('service', CONTRACT, { task: 'designReview' }), 'contractFull');
  assert.equal(at('service', CONTRACT, { task: 'someConstruction' }), 'contractReference');
  assert.equal(at('service', CONTRACT, { lifecyclePhase: 'test_plan' }), 'componentTestPlan');
  assert.equal(at('service', CONTRACT, { task: 'stp' }), 'componentTestPlan');
  assert.equal(at('service', CONTRACT, { task: 'stpReview' }), 'componentTestPlan');
  assert.equal(at('service', CONTRACT, { task: 'construction' }), 'contractReference');
  assert.equal(at('service', CONTRACT, { task: 'testClient' }), 'contractReference');
  assert.equal(at('service', CONTRACT, { task: 'codeReview' }), 'codeReview');
  assert.equal(at('service', CONTRACT, { task: 'integration' }), 'none');
  assert.equal(at('service', CONTRACT, { task: 'testing' }), 'none');
});

void test('the two reference cards say different things', () => {
  const design = placementFor(
    row('service'),
    { activityId: 'X', task: 'someConstruction' },
    CONTRACT
  );
  const build = placementFor(row('service'), { activityId: 'X', task: 'construction' }, CONTRACT);
  assert.deepEqual(design, { kind: 'contractReference', line: REFERENCE_LINE_DESIGN });
  assert.deepEqual(build, { kind: 'contractReference', line: REFERENCE_LINE_CONSTRUCTION });
});

void test('a stale phase param cannot move a task out of its own phase', () => {
  assert.equal(
    at('service', CONTRACT, { lifecyclePhase: 'construction', task: 'detailedDesign' }),
    'contractFull'
  );
});

void test('a missing contract: an overview gap card, then the gap as the Detailed Design body', () => {
  assert.equal(at('service', MISSING, {}), 'gapOverview');
  assert.equal(at('service', MISSING, { task: 'detailedDesign' }), 'contractGap');
  assert.equal(at('service', MISSING, { task: 'designReview' }), 'contractGap');
  // No contract to reference, so no reference card claims one.
  assert.equal(at('service', MISSING, { task: 'construction' }), 'none');
  assert.equal(at('service', MISSING, { task: 'someConstruction' }), 'none');
});

void test('resources: by design at bare and Provisioning Spec depth, cut everywhere else', () => {
  assert.equal(at('deployment', BY_DESIGN, {}), 'byDesignOverview');
  assert.equal(at('deployment', BY_DESIGN, { lifecyclePhase: 'detailed_design' }), 'byDesignSpec');
  assert.equal(at('deployment', BY_DESIGN, { task: 'detailedDesign' }), 'byDesignSpec');
  assert.equal(at('deployment', BY_DESIGN, { task: 'designReview' }), 'none');
  assert.equal(at('deployment', BY_DESIGN, { task: 'codeReview' }), 'none');
});

void test('frontend: the client contract in Detailed Design; the SPA slice keeps the rest', () => {
  assert.equal(at('frontend', CONTRACT, {}), 'contractSummary');
  assert.equal(at('frontend', CONTRACT, { task: 'detailedDesign' }), 'contractFull');
  assert.equal(at('frontend', CONTRACT, { task: 'designReview' }), 'contractFull');
  assert.equal(at('frontend', CONTRACT, { task: 'srsReview' }), 'none');
  assert.equal(at('frontend', CONTRACT, { task: 'construction' }), 'none');
  assert.equal(at('frontend', CONTRACT, { task: 'codeReview' }), 'none');
  assert.equal(at('frontend', CONTRACT, { task: 'stp' }), 'componentTestPlan');
});

void test('no component, or no join: nothing is placed', () => {
  assert.equal(at('testing', { kind: 'noComponent' }, {}), 'none');
  assert.equal(at('service', { kind: 'noComponent' }, { task: 'detailedDesign' }), 'none');
  assert.equal(
    at('service', { kind: 'unresolved', reason: 'noSuchComponent' }, { task: 'detailedDesign' }),
    'contractUnresolved'
  );
  assert.equal(at('service', { kind: 'unresolved', reason: 'noSuchComponent' }, {}), 'none');
  assert.equal(placementFor(undefined, { activityId: 'X' }, CONTRACT).kind, 'none');
});

void test('primaries outrank "no record"; companions do not', () => {
  const r = row('service');
  const dd = { activityId: 'X', task: 'detailedDesign' };
  assert.equal(detailBodyFor(r, dd, 'notStarted', undefined, true), 'artifact');
  assert.equal(detailBodyFor(r, dd, 'unknown', undefined, true), 'artifact');
  assert.equal(detailBodyFor(r, dd, 'notStarted', undefined, false), 'unknown');
  assert.equal(
    detailBodyFor(r, { activityId: 'X', task: 'designReview' }, 'notStarted', undefined, true),
    'review'
  );
  assert.equal(isPrimaryPlacement({ kind: 'contractFull' }), true);
  assert.equal(isPrimaryPlacement({ kind: 'contractSummary' }), false);
  assert.equal(isPrimaryPlacement({ kind: 'contractReference', line: '' }), false);
});

void test('UNDER REVIEW needs a gate owed now on an observed attempt — nothing less', () => {
  const owed = {
    gateSelected: true,
    gateOwedNow: true,
    attemptOrigin: 'observed' as const,
    hiddenCount: 0,
  };
  assert.equal(artifactRoleFor(owed), 'underReview');
  assert.equal(artifactRoleFor({ ...owed, attemptOrigin: undefined }), 'underReview');
  assert.equal(artifactRoleFor({ ...owed, gateOwedNow: false }), 'committedNow');
  assert.equal(artifactRoleFor({ ...owed, gateSelected: false }), 'committedNow');
  assert.equal(artifactRoleFor({ ...owed, attemptOrigin: 'backfilled' }), 'committedNow');
  assert.equal(artifactRoleFor({ ...owed, attemptOrigin: 'synthesized' }), 'committedNow');
  assert.equal(artifactRoleFor({ ...owed, hiddenCount: 1 }), 'committedNow');
});

void test('the source line claims the current contract and nothing about authorship', () => {
  assert.equal(
    contractSourceLine('constructionManager', 0, false),
    'serviceContracts.constructionManager · current · no revision history recorded'
  );
  assert.equal(contractSourceLine('x', 3, false), 'serviceContracts.x · current · revision 3 of 3');
  assert.match(
    contractSourceLine('x', 0, true),
    / · shown under Observed only: project state, not evidence$/
  );
  assert.doesNotMatch(contractSourceLine('x', 3, true), /written by/);
});

void test('the reconstructed sentence names its scope and the revision history', () => {
  assert.equal(
    reconstructedArtifactNote('task', 0),
    'The contract below is the one committed today. Nothing recorded links it to this attempt — the attempt was reconstructed, and the contract has no revision history.'
  );
  assert.match(
    reconstructedArtifactNote('wider', 0),
    /the attempts here — they were reconstructed/
  );
  assert.match(
    reconstructedArtifactNote('task', 2),
    /no revision records the attempt that wrote it/
  );
});

void test('the gap sentences are distinct: a real gap versus nothing missing', () => {
  const missing = missingContractSentence({
    componentId: 'design-health-engine',
    contractKey: undefined,
    othersMissing: 0,
    operations: [
      {
        label: 'EvaluateDesignHealth(project, systemModel) → findings',
        calledBy: 'system-design-manager',
      },
    ],
  });
  assert.equal(
    missing,
    'design-health-engine has no entry in the committed service contracts. Every other component built by an activity has one, so this is missing data, not a design choice. Detailed Design cannot honestly pass without it. The architecture names one operation on it: EvaluateDesignHealth(project, systemModel) → findings, called by system-design-manager.'
  );
  const resource = byDesignSentence('resource');
  assert.match(resource, /Nothing is missing/);
  assert.doesNotMatch(missing, /Nothing is missing/);
  assert.doesNotMatch(resource, /missing data/);
  // "Every other component has one" is computed, never asserted blind.
  assert.match(
    missingContractSentence({
      componentId: 'a',
      contractKey: 'aKey',
      othersMissing: 2,
      operations: [],
    }),
    /^a names the contract key aKey, .* one of 3 components .* names no operation on it\.$/
  );
});

void test('the backfill clause appears only where the stp attempt was reconstructed', () => {
  assert.match(noTestPlanSentence(true), /backfilled with no plan behind them/);
  assert.doesNotMatch(noTestPlanSentence(false), /backfilled/);
  assert.match(noTestPlanSentence(false), /it is not a substitute\.$/);
});
