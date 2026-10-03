/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import { taskArtifactFor } from './taskArtifactFor.ts';

void test('a service design review is the contract, through the JOIN', () => {
  assert.deepEqual(
    taskArtifactFor({
      type: 'service',
      taskId: 'designReview',
      componentId: 'billing-state-access',
      artifactKind: 'DetailedDesign',
    }),
    { kind: 'serviceContract', componentId: 'billing-state-access' }
  );
});

void test('a service SRS review is NOT the contract — the ARTIFACT_PHASES rule', () => {
  const a = taskArtifactFor({
    type: 'service',
    taskId: 'srsReview',
    componentId: 'billing-state-access',
    artifactKind: 'SRS',
  });
  assert.equal(a.kind, 'unavailable');
  assert.match((a as { reason: string }).reason, /another phase/);
});

void test('a service activity with no component has no contract to draw', () => {
  const a = taskArtifactFor({ type: 'service', taskId: 'designReview' });
  assert.equal(a.kind, 'unavailable');
});

void test('a frontend code review is the built surface', () => {
  assert.deepEqual(taskArtifactFor({ type: 'frontend', taskId: 'codeReview' }), {
    kind: 'classified',
    classification: 'frontend',
    phase: 'construction',
  });
});

void test('a uiDesign concept review is the design concept', () => {
  assert.deepEqual(taskArtifactFor({ type: 'uiDesign', taskId: 'designReview' }), {
    kind: 'classified',
    classification: 'uiDesign',
    phase: 'detailed_design',
  });
});

// Deterministic component testing (spec §6.4): a component's own lifecycle
// carries its scenario plan (`stp` / `stpReview`) and its run (`testing`).
void test('a service test-plan review is the scenario plan, on the test_plan phase', () => {
  for (const taskId of ['stp', 'stpReview']) {
    assert.deepEqual(
      taskArtifactFor({ type: 'service', taskId, componentId: 'billing-state-access' }),
      { kind: 'classified', classification: 'service', phase: 'test_plan' },
      taskId
    );
  }
});

void test('a service testing review is the test run, on the integration phase', () => {
  assert.deepEqual(
    taskArtifactFor({ type: 'service', taskId: 'testing', componentId: 'billing-state-access' }),
    { kind: 'classified', classification: 'service', phase: 'integration' }
  );
  // The integration DISPATCH task sits on the same phase and shows the same run.
  assert.deepEqual(taskArtifactFor({ type: 'service', taskId: 'integration', componentId: 'x' }), {
    kind: 'classified',
    classification: 'service',
    phase: 'integration',
  });
});

void test('a frontend test-plan and testing review take the same two renderers', () => {
  assert.deepEqual(taskArtifactFor({ type: 'frontend', taskId: 'stpReview' }), {
    kind: 'classified',
    classification: 'frontend',
    phase: 'test_plan',
  });
  assert.deepEqual(taskArtifactFor({ type: 'frontend', taskId: 'testing' }), {
    kind: 'classified',
    classification: 'frontend',
    phase: 'integration',
  });
});

void test('a service code review has no artifact view: the code is on the PR, not here', () => {
  const a = taskArtifactFor({ type: 'service', taskId: 'codeReview', componentId: 'x' });
  assert.equal(a.kind, 'unavailable');
  assert.match((a as { reason: string }).reason, /another phase/);
});

void test('a requirements review names its committed slot, through the RESOLVED kind', () => {
  assert.deepEqual(
    taskArtifactFor({ type: 'requirements', taskId: 'glossaryReview', artifactKind: 'Glossary' }),
    { kind: 'slot', artifactKind: 'glossary' }
  );
});

void test('an architecture review names the system slot', () => {
  assert.deepEqual(
    taskArtifactFor({ type: 'architecture', taskId: 'architectureReview', artifactKind: 'System' }),
    { kind: 'slot', artifactKind: 'system' }
  );
});

void test('the M0 review names the sdpReview slot', () => {
  assert.deepEqual(
    taskArtifactFor({ type: 'projectDesign', taskId: 'sdpReview', artifactKind: 'SdpReview' }),
    { kind: 'slot', artifactKind: 'sdpReview' }
  );
});

void test('a design review with no resolvable kind refuses rather than picking a slot', () => {
  const a = taskArtifactFor({ type: 'requirements', taskId: 'glossaryReview' });
  assert.equal(a.kind, 'unavailable');
  assert.match((a as { reason: string }).reason, /artifact kind/);
});

// ---------------------------------------------------------------------------
// The seven R17 earmarks. Each is asserted ONCE here so the earmark list in the
// report cannot drift from what the code actually refuses to draw.
// ---------------------------------------------------------------------------

void test('deployment has no artifact view, and the reason names the kind', () => {
  const a = taskArtifactFor({ type: 'deployment', taskId: 'construction' });
  assert.equal(a.kind, 'unavailable');
  assert.match((a as { reason: string }).reason, /deployment/);
});

void test('documentation has no artifact view', () => {
  const a = taskArtifactFor({ type: 'documentation', taskId: 'codeReview' });
  assert.equal(a.kind, 'unavailable');
  assert.match((a as { reason: string }).reason, /documentation/);
});

void test('integration has no artifact view', () => {
  const a = taskArtifactFor({ type: 'integration', taskId: 'testing' });
  assert.equal(a.kind, 'unavailable');
  assert.match((a as { reason: string }).reason, /integration/);
});

void test('the three testing variants with no authored view have none', () => {
  for (const variant of ['harness', 'perf', 'qaProcess']) {
    const a = taskArtifactFor({ type: 'testing', variant, taskId: 'codeReview' });
    assert.equal(a.kind, 'unavailable', variant);
    assert.match((a as { reason: string }).reason, new RegExp(`testing:${variant}`), variant);
  }
});

void test('a testing row without a variant has no lifecycle, so nothing to draw', () => {
  const a = taskArtifactFor({ type: 'testing', taskId: 'codeReview' });
  assert.equal(a.kind, 'unavailable');
});

void test('an activity type this build does not know is refused, never guessed', () => {
  const a = taskArtifactFor({ type: 'somethingNew', taskId: 'codeReview' });
  assert.equal(a.kind, 'unavailable');
});

void test('a task id the lifecycle does not carry is refused', () => {
  const a = taskArtifactFor({ type: 'frontend', taskId: 'noSuchTask' });
  assert.equal(a.kind, 'unavailable');
});
