/**
 * The unknown body's pure half (taskBriefing.ts) plus the body dispatch
 * (bodyDispatch.ts) and the pane's provenance/evidence scoping
 * (detailPaneState.ts). Node's type-stripping test runner cannot load a `.tsx`
 * module at all, which is exactly why all three are plain `.ts` siblings of the
 * components that render them.
 */
/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import type {
  ConstructionRow,
  ProjectStateWithGit,
  TaskAttemptRow,
  TestScenarioView,
} from '../../../../contracts/types.ts';
import { worstOriginOf, provenanceBasesOf } from '../../provenanceAxis.ts';
import { evidencePointerFor, provenanceNodeFor, selectedAttemptOf } from '../detailPaneState.ts';
import { absenceFor, briefingFor, unknownStatementFor, UNKNOWN_STATEMENT } from './taskBriefing.ts';
import {
  artifactRendererKeyFor,
  detailBodyFor,
  selectedTaskIsGate,
  testingArtifactRendererKeyFor,
} from './bodyDispatch.ts';

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

function row(overrides: Partial<ConstructionRow> = {}): ConstructionRow {
  return {
    activityId: 'C-x',
    classified: true,
    hasBuildEvidence: true,
    phases: [],
    attempts: [],
    ...overrides,
  };
}

function attempt(overrides: Partial<TaskAttemptRow> = {}): TaskAttemptRow {
  return {
    attemptId: 'C-x:srs:1',
    task: 'srs',
    phase: 'requirements',
    attempt: 1,
    outcome: 'passed',
    evidence: { kind: '', ref: '' },
    provenance: { origin: 'observed' },
    ...overrides,
  };
}

/** The real shape of a ruling-derived attempt: passed, backfilled, no evidence. */
function rulingAttempt(task: string): TaskAttemptRow {
  return attempt({
    attemptId: `C-AA:${task}:1`,
    task,
    outcome: 'passed',
    evidence: { kind: '', ref: '' },
    provenance: {
      origin: 'backfilled',
      generator: 'cmd/backfill-attempts',
      basis:
        'serviceContracts[artifactAccess] + founderRuling[2026-09-09]=assume any component that is fully implemented is done and reviewed and integrated',
    },
  });
}

function scenario(id: string): TestScenarioView {
  return { id, useCase: `uc-${id}`, title: `Scenario ${id}`, cases: [] };
}

/** A project carrying a committed system test plan / a recorded test run. */
function projectWith(
  testingState: Partial<NonNullable<ProjectStateWithGit['testingState']>>
): ProjectStateWithGit {
  return {
    projectId: 'p1',
    name: 'p1',
    owner: 'usr-1',
    phase: 'construction',
    version: 1,
    research: { sources: [] },
    slots: [],
    testingState: { testRuns: [], defects: [], ...testingState },
  };
}

// ---------------------------------------------------------------------------
// The briefing — composed, never authored per task
// ---------------------------------------------------------------------------

void test('composes the gate task briefing from the generated profile alone', () => {
  const r = row({ kind: 'service' });
  const b = briefingFor(r, { lifecyclePhase: 'detailed_design', task: 'designReview' });
  assert.ok(b !== undefined);
  assert.equal(b.title, 'Design Review');
  assert.equal(b.scope, 'task');
  // Gate-ness and the phase's display name are the only inputs to the sentence.
  assert.match(b.whatItIs, /gate task of Detailed Design/);
  assert.equal(b.weight, 'part of Detailed Design · 20% of this activity.');
  assert.equal(b.retryRule, 'A failing Design Review repeats Detailed Design.');
  assert.ok(b.exit.length > 0);
});

void test('a work task names the gate that actually decides its phase', () => {
  const b = briefingFor(row({ kind: 'service' }), {
    lifecyclePhase: 'construction',
    task: 'construction',
  });
  assert.ok(b !== undefined);
  assert.match(b.whatItIs, /work task within Construction/);
  assert.match(b.whatItIs, /exit rests on Code Review/);
  assert.match(b.retryRule, /re-runs Construction alone; Code Review still decides/);
});

void test('reads the per-KIND phase name, not a canonical one', () => {
  // `test_plan` is "Test Plan" for a service and "Flows" for a frontend. A hand
  // table would have one name for both.
  const service = briefingFor(row({ kind: 'service' }), { lifecyclePhase: 'test_plan' });
  const frontend = briefingFor(row({ kind: 'frontend' }), { lifecyclePhase: 'test_plan' });
  assert.equal(service?.title, 'Test Plan');
  assert.equal(frontend?.title, 'Flows');
});

void test('falls back to a phase briefing when no task is selected', () => {
  const b = briefingFor(row({ kind: 'service' }), { lifecyclePhase: 'integration' });
  assert.ok(b !== undefined);
  assert.equal(b.scope, 'lifecyclePhase');
  assert.equal(b.title, 'Integration');
});

void test('an unclassified activity gets no briefing at all, never a borrowed one', () => {
  assert.equal(briefingFor(row({ classified: false }), { task: 'srs' }), undefined);
  assert.equal(briefingFor(undefined, { task: 'srs' }), undefined);
});

void test('the unknown statement carries no error tone and names both real causes', () => {
  assert.equal(unknownStatementFor('task'), UNKNOWN_STATEMENT);
  assert.match(UNKNOWN_STATEMENT, /has not run, or it ran before per-task history was captured/);
  for (const word of ['error', 'failed', 'wrong', 'unable', 'problem']) {
    assert.equal(UNKNOWN_STATEMENT.toLowerCase().includes(word), false, `tone word: ${word}`);
  }
});

// ---------------------------------------------------------------------------
// ABSENT is not UNKNOWN — the whole point of the sibling body
// ---------------------------------------------------------------------------

void test('a phase the profile does not carry is absent BY DESIGN, not missing data', () => {
  const absence = absenceFor(row({ kind: 'deployment' }), { lifecyclePhase: 'test_plan' });
  assert.ok(absence !== undefined);
  assert.equal(absence.scope, 'lifecyclePhase');
  assert.equal(
    absence.statement,
    'Deployment activities carry no Test Plan phase. This is by design, not missing data.'
  );
  // It shows what the profile DOES carry, so the reader's next question is answered.
  assert.deepEqual(
    absence.carried.map((p) => p.name),
    ['Provisioning Spec', 'Construction', 'Convergence Verification']
  );
});

void test('a phase the profile DOES carry is never called absent, however empty', () => {
  assert.equal(
    absenceFor(row({ kind: 'deployment' }), { lifecyclePhase: 'construction' }),
    undefined
  );
  // ...and neither is a junk phase id from the URL: we cannot claim a profile
  // deliberately omits something that is not a Method phase at all.
  assert.equal(absenceFor(row({ kind: 'deployment' }), { lifecyclePhase: 'nonsense' }), undefined);
});

void test('a task the profile does not carry is absent too', () => {
  const absence = absenceFor(row({ kind: 'uiDesign' }), { task: 'codeReview' });
  assert.ok(absence !== undefined);
  assert.equal(absence.scope, 'task');
  assert.match(absence.statement, /by design, not missing data/);
});

// ---------------------------------------------------------------------------
// Dispatch — the order of the questions IS the design
// ---------------------------------------------------------------------------

void test('absence is decided BEFORE "no record", so it can never read as a gap', () => {
  const r = row({ kind: 'deployment' });
  assert.equal(detailBodyFor(r, { lifecyclePhase: 'test_plan' }, 'unknown'), 'absent');
});

void test('no record lands on the unknown body — the majority path', () => {
  const r = row({ kind: 'service' });
  assert.equal(
    detailBodyFor(r, { lifecyclePhase: 'requirements', task: 'srs' }, 'unknown'),
    'unknown'
  );
  assert.equal(detailBodyFor(r, {}, 'notStarted'), 'unknown');
});

void test('a gate task with a record is a review; a work task is its artifact or its episodes', () => {
  const service = row({ kind: 'service', attempts: [rulingAttempt('srs')] });
  assert.equal(detailBodyFor(service, { task: 'designReview' }, 'passed'), 'review');
  assert.equal(detailBodyFor(service, { task: 'detailedDesign' }, 'passed'), 'artifact');
  // Deployment is CUT for this stage: no renderer, so it falls through to the
  // episode body rather than to an empty artifact frame.
  const deployment = row({ kind: 'deployment' });
  assert.equal(detailBodyFor(deployment, { task: 'construction' }, 'passed'), 'episode');
});

void test('an activity-level selection gets the episode body, which is activity-level too', () => {
  assert.equal(detailBodyFor(row({ kind: 'service' }), {}, 'passed'), 'episode');
});

void test('selectedTaskIsGate reads the generated profile, not the task name', () => {
  const r = row({ kind: 'service' });
  assert.equal(selectedTaskIsGate(r, { task: 'codeReview' }), true);
  assert.equal(selectedTaskIsGate(r, { task: 'construction' }), false);
  assert.equal(selectedTaskIsGate(r, {}), false);
});

void test('the cut classifications resolve to no artifact renderer', () => {
  const inPhase = { task: 'detailedDesign' };
  assert.equal(artifactRendererKeyFor(row({ kind: 'service' }), inPhase), 'service');
  assert.equal(artifactRendererKeyFor(row({ kind: 'uiDesign' }), inPhase), 'uiDesign');
  assert.equal(
    artifactRendererKeyFor(row({ kind: 'testing', variant: 'plan' }), { task: 'construction' }),
    'testing:plan'
  );
  for (const kind of ['deployment', 'documentation', 'integration'] as const) {
    assert.equal(artifactRendererKeyFor(row({ kind }), { task: 'construction' }), undefined, kind);
  }
  // A testing variant with no authored renderer falls back honestly too.
  assert.equal(
    artifactRendererKeyFor(row({ kind: 'testing', variant: 'perf' }), { task: 'construction' }),
    undefined
  );
  assert.equal(artifactRendererKeyFor(row({ classified: false }), inPhase), undefined);
});

void test('an artifact renderer is scoped to ITS OWN phase, never spread across the activity', () => {
  const service = row({ kind: 'service' });
  // The frozen contract belongs to Detailed Design...
  assert.equal(artifactRendererKeyFor(service, { task: 'detailedDesign' }), 'service');
  // ...so SRS, a Requirements task, must NOT be captioned with it. The service
  // contract is not the SRS, and placing it there would claim it is.
  assert.equal(artifactRendererKeyFor(service, { task: 'srs' }), undefined);
  assert.equal(detailBodyFor(service, { task: 'srs' }, 'passed'), 'episode');
  // The phase comes from the task's own profile entry, so a stale `p=` in the
  // URL cannot move an artifact into a phase it does not belong to.
  assert.equal(
    artifactRendererKeyFor(service, { lifecyclePhase: 'detailed_design', task: 'srs' }),
    undefined
  );
});

// ---------------------------------------------------------------------------
// A committed testing artifact outranks "no record" (N-STP unreachable fix)
// ---------------------------------------------------------------------------

void test('a testing:plan row with no build evidence still renders its committed plan', () => {
  const nStp = row({ kind: 'testing', variant: 'plan', hasBuildEvidence: false });
  const project = projectWith({ systemTestPlan: { scenarios: [scenario('STP-UC1')] } });

  // The bare activity row — the plainest click.
  assert.equal(detailBodyFor(nStp, {}, 'notStarted', project), 'artifact');
  // Its own phases (Plan Authoring = construction, Plan Review = integration),
  // with no task named.
  assert.equal(
    detailBodyFor(nStp, { lifecyclePhase: 'construction' }, 'notStarted', project),
    'artifact'
  );
  assert.equal(
    detailBodyFor(nStp, { lifecyclePhase: 'integration' }, 'notStarted', project),
    'artifact'
  );
  // A non-gate task within one of those phases.
  assert.equal(detailBodyFor(nStp, { task: 'construction' }, 'unknown', project), 'artifact');
  // A gate task still owes the review surface, not the plain artifact one.
  assert.equal(detailBodyFor(nStp, { task: 'codeReview' }, 'unknown', project), 'review');
  assert.equal(detailBodyFor(nStp, { task: 'testing' }, 'unknown', project), 'review');
});

void test('the bypass never widens past the plan’s own phases', () => {
  const nStp = row({ kind: 'testing', variant: 'plan', hasBuildEvidence: false });
  const project = projectWith({ systemTestPlan: { scenarios: [scenario('STP-UC1')] } });
  // Requirements ("Use-Case Trace": srs/srsReview) is not where the plan lives —
  // untouched by the bypass, exactly the pre-fix reading.
  assert.equal(
    detailBodyFor(nStp, { lifecyclePhase: 'requirements' }, 'notStarted', project),
    'unknown'
  );
  assert.equal(detailBodyFor(nStp, { task: 'srs' }, 'unknown', project), 'unknown');
  assert.equal(detailBodyFor(nStp, { task: 'srsReview' }, 'unknown', project), 'unknown');
});

void test('a testing:plan row with no committed plan is untouched by the bypass', () => {
  const noPlan = row({ kind: 'testing', variant: 'plan', hasBuildEvidence: false });
  assert.equal(testingArtifactRendererKeyFor(noPlan, {}, undefined), undefined);
  assert.equal(detailBodyFor(noPlan, {}, 'notStarted', undefined), 'unknown');
  // An empty scenarios array reads the same as none at all.
  const emptyPlan = projectWith({ systemTestPlan: { scenarios: [] } });
  assert.equal(detailBodyFor(noPlan, {}, 'notStarted', emptyPlan), 'unknown');
});

void test('a testing:systemTest row renders once a test run is recorded', () => {
  const nIt = row({ kind: 'testing', variant: 'systemTest', hasBuildEvidence: false });
  const project = projectWith({ testRuns: [{ id: 'TR-1', passed: 1, failed: 0, note: '' }] });
  assert.equal(testingArtifactRendererKeyFor(nIt, {}, project), 'testing:systemTest');
  assert.equal(detailBodyFor(nIt, {}, 'notStarted', project), 'artifact');
  // No recorded run: untouched.
  const noRuns = projectWith({ testRuns: [] });
  assert.equal(testingArtifactRendererKeyFor(nIt, {}, noRuns), undefined);
  assert.equal(detailBodyFor(nIt, {}, 'notStarted', noRuns), 'unknown');
});

void test('the bypass never reaches service/uiDesign/frontend — their gate stays exactly as is', () => {
  const project = projectWith({ systemTestPlan: { scenarios: [scenario('STP-UC1')] } });
  for (const kind of ['service', 'uiDesign', 'frontend'] as const) {
    const r = row({ kind, hasBuildEvidence: false });
    assert.equal(testingArtifactRendererKeyFor(r, {}, project), undefined, kind);
    assert.equal(detailBodyFor(r, {}, 'notStarted', project), 'unknown', kind);
  }
});

// ---------------------------------------------------------------------------
// The pane's provenance scoping — the laundering fix
// ---------------------------------------------------------------------------

void test('a ruling-derived task reads RECONSTRUCTED in the pane and quotes its basis', () => {
  const r = row({
    activityId: 'C-AA',
    kind: 'service',
    worstOrigin: 'backfilled',
    attempts: [rulingAttempt('srs'), rulingAttempt('srsReview')],
  });
  const node = provenanceNodeFor(r, { activityId: 'C-AA', task: 'srs' });
  assert.equal(worstOriginOf(node), 'backfilled');
  const bases = provenanceBasesOf(node);
  assert.equal(bases.length, 1);
  assert.match(bases[0] ?? '', /founderRuling\[2026-09-09\]=/);
});

void test('an empty ledger is UNKNOWN provenance in the pane, never "observed"', () => {
  const r = row({ kind: 'service', attempts: [] });
  assert.equal(worstOriginOf(provenanceNodeFor(r, { task: 'srs' })), 'unknown');
  assert.equal(worstOriginOf(provenanceNodeFor(r, {})), 'unknown');
  assert.equal(worstOriginOf(provenanceNodeFor(undefined, {})), 'unknown');
});

void test('an activity-level reading folds in the server roll-up as well as the ledger', () => {
  // The ledger only carries tasks the profile names; an off-profile reconstructed
  // attempt would otherwise hide in the gap between the two.
  const r = row({ kind: 'service', worstOrigin: 'synthesized', attempts: [attempt()] });
  assert.equal(worstOriginOf(provenanceNodeFor(r, {})), 'synthesized');
});

void test('an empty evidence ref renders as absent, never as a target to click', () => {
  const r = row({ kind: 'service', attempts: [rulingAttempt('srs')] });
  const pointer = evidencePointerFor(r, { task: 'srs' });
  assert.deepEqual(pointer, { kind: '', ref: '', present: false });

  const withEvidence = row({
    kind: 'service',
    attempts: [
      attempt({ task: 'detailedDesign', evidence: { kind: 'contract', ref: 'artifactAccess' } }),
    ],
  });
  assert.deepEqual(evidencePointerFor(withEvidence, { task: 'detailedDesign' }), {
    kind: 'contract',
    ref: 'artifactAccess',
    present: true,
  });

  // No attempt at all is a THIRD answer, distinct from an empty ref.
  assert.equal(evidencePointerFor(r, { task: 'stp' }), undefined);
});

void test('selectedAttemptOf honours an explicit attempt, else the highest NUMBER', () => {
  const r = row({
    kind: 'service',
    attempts: [
      attempt({ attemptId: 'a2', attempt: 2 }),
      attempt({ attemptId: 'a1', attempt: 1, outcome: 'failed' }),
    ],
  });
  assert.equal(selectedAttemptOf(r, { task: 'srs' })?.attemptId, 'a2');
  assert.equal(selectedAttemptOf(r, { task: 'srs', attempt: 1 })?.attemptId, 'a1');
  assert.equal(selectedAttemptOf(r, {}), undefined);
});
