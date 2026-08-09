/**
 * Hand-authored app-string mappings for the enums whose Go varname -> app-string
 * derivation is NOT mechanical (strip the shared type-name prefix, lowerFirst the
 * remainder — see enums.gen.ts's header). Each block below documents, in its
 * per-enum comment, the SAME divergence enums.gen.ts records on its "NOT
 * mechanically derivable" note: a casing convention, a semantic rename, or a
 * deliberate product-decision collapse that a bare mechanical transform cannot
 * reproduce.
 *
 * Every table is a `Record<GoVarname, AppString>` KEYED BY THE GENERATED varname
 * union imported from enums.gen.ts — NOT a parallel hand-authored ordinal array.
 * A Go const rename/add/remove therefore fails to satisfy the Record literal's
 * exhaustiveness here at tsc time, the same drift protection the mechanical
 * tables get for free from the generator, instead of silently going stale.
 *
 * This file used to be enums.ts (18 hand ordinal tables); Task 3 of the appgen
 * step-4 migration re-pointed every mechanical table onto enums.gen.ts (see
 * wire.ts) and left only these 7 non-mechanical mappings here, rebuilt over the
 * generated varname tables. scripts/check-enum-derivation.mjs — which proved the
 * 13 mechanical tables byte-identical to the old hand arrays and asserted the
 * exact shape of these 7 divergences — is deleted; its referent hand ordinal
 * tables no longer exist, and the Record-over-generated-varname-union pattern
 * here is the standing structural guard the script used to provide by hand.
 */
import {
  ACTIVITY_BUILD_STATUS_ORDINAL_TO_GO_VARNAME,
  type ActivityBuildStatusGoVarname,
  AUTOSCALER_MODE_ORDINAL_TO_GO_VARNAME,
  type AutoscalerModeGoVarname,
  CI_CHECK_STATE_ORDINAL_TO_GO_VARNAME,
  type CICheckStateGoVarname,
  PIPELINE_PHASE_ORDINAL_TO_GO_VARNAME,
  type PipelinePhaseGoVarname,
  PROJECT_SESSION_STAGE_ORDINAL_TO_GO_VARNAME,
  type ProjectSessionStageGoVarname,
  RUNTIME_STATUS_SEAM_ORDINAL_TO_GO_VARNAME,
  type RuntimeStatusSeamGoVarname,
  TESTING_VARIANT_ORDINAL_TO_GO_VARNAME,
  type TestingVariantGoVarname,
} from './enums.gen.ts';
import type {
  ActivityBuildStatusRow,
  CiStatus,
  PipelinePhase,
  ProjectSessionStage,
  TestingVariantName,
} from './types';
import type { RuntimePhase } from './operationsTypes';

// --- ActivityBuildStatus (row status) ---------------------------------------
// Mechanical derivation gives ("inConstruction"/"inReview"/"integrated"/"failed");
// the app uses kebab-case. Every member maps 1:1 — BuildFailed is a TERMINAL row
// state of its own. It used to collapse into "in-construction" ("no terminal-fail
// row state"), which rendered an activity the pump had durably given up on as
// "In construction" forever; the operator only ever saw the stall in a log.

const ACTIVITY_BUILD_STATUS_APP_STRING: Readonly<
  Record<ActivityBuildStatusGoVarname, ActivityBuildStatusRow>
> = {
  BuildInConstruction: 'in-construction',
  BuildInReview: 'in-review',
  BuildIntegrated: 'integrated',
  BuildFailed: 'failed',
};

/** ProjectActivityBuildStatus (0 in-construction,1 in-review,2 integrated,3 failed). */
export function buildStatusRowFromOrdinal(ordinal: number): ActivityBuildStatusRow {
  const varname = ACTIVITY_BUILD_STATUS_ORDINAL_TO_GO_VARNAME[ordinal];
  return varname !== undefined ? ACTIVITY_BUILD_STATUS_APP_STRING[varname] : 'in-construction';
}

// --- AutoscalerMode ----------------------------------------------------------
// Mechanical derivation gives lowerCamel ("auto"/"manual"); the app uses
// PascalCase ("Auto"/"Manual"/"Unknown"). Casing convention diff, not a bug.

const AUTOSCALER_MODE_APP_STRING: Readonly<Record<AutoscalerModeGoVarname, string>> = {
  AutoscalerModeUnknown: 'Unknown',
  AutoscalerModeAuto: 'Auto',
  AutoscalerModeManual: 'Manual',
};

/** OperationsAutoscalerMode (0 unknown,1 auto,2 manual). */
export function autoscalerModeFromOrdinal(ordinal: number): string {
  const varname = AUTOSCALER_MODE_ORDINAL_TO_GO_VARNAME[ordinal];
  return varname !== undefined ? AUTOSCALER_MODE_APP_STRING[varname] : 'Unknown';
}

// --- CICheckState --------------------------------------------------------
// Mechanical derivation gives ("pending"/"success"/"failure"); the app uses
// ("in_progress"/"success"/"failed") — different words for 2 of 3 members.
// Semantic naming diff, not a bug.

const CI_CHECK_STATE_APP_STRING: Readonly<Record<CICheckStateGoVarname, CiStatus>> = {
  CICheckPending: 'in_progress',
  CICheckSuccess: 'success',
  CICheckFailure: 'failed',
};

/** ProjectCICheckState (0 Pending, 1 Success, 2 Failure) -> app CiStatus. */
export function ciStatusFromOrdinal(ordinal: number): CiStatus {
  const varname = CI_CHECK_STATE_ORDINAL_TO_GO_VARNAME[ordinal];
  return varname !== undefined ? CI_CHECK_STATE_APP_STRING[varname] : 'in_progress';
}

// --- PipelinePhase -----------------------------------------------------------
// Mechanical derivation gives ordinal 5 (PipelineCancelled) -> "cancelled"; the
// app deliberately folds ordinal 5 into the same value as ordinal 4 ("failed") —
// the app has no distinct cancelled state. Deliberate product simplification,
// not a bug.

const PIPELINE_PHASE_APP_STRING: Readonly<Record<PipelinePhaseGoVarname, PipelinePhase>> = {
  PipelinePhaseUnknown: 'unknown',
  PipelinePending: 'pending',
  PipelineRunning: 'running',
  PipelineSucceeded: 'succeeded',
  PipelineFailed: 'failed',
  PipelineCancelled: 'failed',
};

export function pipelinePhaseFromOrdinal(ordinal: number): PipelinePhase {
  const varname = PIPELINE_PHASE_ORDINAL_TO_GO_VARNAME[ordinal];
  return varname !== undefined ? PIPELINE_PHASE_APP_STRING[varname] : 'unknown';
}

// --- ProjectSessionStage ------------------------------------------------------
// StageAssemblingSDP derives mechanically to "assemblingSDP" (lowerFirst only
// lowercases the leading letter); the app uses "assemblingSdp". Casing
// convention diff, not a bug — every other member is mechanical.

const PROJECT_SESSION_STAGE_APP_STRING: Readonly<
  Record<ProjectSessionStageGoVarname, ProjectSessionStage>
> = {
  SessionStageUnknown: 'unknown',
  StageDrafting: 'drafting',
  StageAssemblingSDP: 'assemblingSdp',
  StageAwaitingReview: 'awaitingReview',
  StageRedrafting: 'redrafting',
  StageCommitted: 'committed',
  StageWithdrawn: 'withdrawn',
  StageRefused: 'refused',
  StageDraftFailed: 'draftFailed',
};

export function projectSessionStageFromOrdinal(ordinal: number): ProjectSessionStage {
  const varname = PROJECT_SESSION_STAGE_ORDINAL_TO_GO_VARNAME[ordinal];
  return varname !== undefined ? PROJECT_SESSION_STAGE_APP_STRING[varname] : 'unknown';
}

// --- RuntimeStatusSeam ---------------------------------------------------
// Mechanical derivation gives ("unknown"/"pending"/"healthy"/"degraded"/
// "withdrawn"); the app uses PascalCase AND renames ordinal 2
// (RuntimeStatusHealthy) to "Running". Casing + semantic rename, not a bug.

const RUNTIME_STATUS_SEAM_APP_STRING: Readonly<Record<RuntimeStatusSeamGoVarname, RuntimePhase>> = {
  RuntimeStatusUnknown: 'Unknown',
  RuntimeStatusPending: 'Pending',
  RuntimeStatusHealthy: 'Running',
  RuntimeStatusDegraded: 'Degraded',
  RuntimeStatusWithdrawn: 'Withdrawn',
};

/** OperationsRuntimeStatusSeam (0 unknown,1 pending,2 healthy,3 degraded,4 withdrawn). */
export function runtimePhaseFromOrdinal(ordinal: number): RuntimePhase {
  const varname = RUNTIME_STATUS_SEAM_ORDINAL_TO_GO_VARNAME[ordinal];
  return varname !== undefined ? RUNTIME_STATUS_SEAM_APP_STRING[varname] : 'Unknown';
}

// --- TestingVariant ------------------------------------------------------
// The local type name is "TestingVariant" but the Go consts use the short
// prefix "Test" (TestVariantPlan, ...) — "Test" does not whole-word-strip from
// "TestingVariant", so the mechanical derivation falls through to the full
// lowerFirst varname ("testVariantPlan"). The app instead uses short forms.
// Not mechanically derivable.

const TESTING_VARIANT_APP_STRING: Readonly<Record<TestingVariantGoVarname, TestingVariantName>> = {
  TestVariantPlan: 'plan',
  TestVariantHarness: 'harness',
  TestVariantPerf: 'perf',
  TestVariantSystemTest: 'systemTest',
  TestVariantQAProcess: 'qaProcess',
};

/** TestingVariant (0 plan,1 harness,2 perf,3 systemTest,4 qaProcess); undefined for non-testing. */
export function testingVariantFromOrdinal(ordinal: number): TestingVariantName | undefined {
  const varname = TESTING_VARIANT_ORDINAL_TO_GO_VARNAME[ordinal];
  return varname !== undefined ? TESTING_VARIANT_APP_STRING[varname] : undefined;
}
