/**
 * One-off (kept, re-runnable) check: for every generated ordinal-indexed table
 * in src/contracts/enums.gen.ts that claims to be "mechanical", assert it is
 * element-wise identical to the corresponding hand table in
 * src/contracts/enums.ts (or adapters.ts, for ArtifactStage). Also documents
 * — and asserts — the intentional mismatches (semantic collapses / casing
 * conventions) for the enums marked NOT mechanically derivable, so a future
 * change to either side that silently "fixes" one of them trips this check
 * for re-review rather than passing unnoticed.
 *
 * Run: node --experimental-strip-types scripts/check-enum-derivation.mjs
 */
import * as gen from '../src/contracts/enums.gen.ts';

let failures = 0;

function assertEqual(label, actual, expected) {
  const a = JSON.stringify(actual);
  const e = JSON.stringify(expected);
  if (a === e) {
    console.log(`OK   ${label}`);
  } else {
    failures += 1;
    console.log(`FAIL ${label}\n     generated: ${a}\n     hand:      ${e}`);
  }
}

// --- Mechanical enums: generated APP_STRINGS must equal the hand table -----

assertEqual('ArtifactKind', gen.ARTIFACT_KIND_APP_STRINGS, [
  'mission',
  'glossary',
  'scrubbedRequirements',
  'volatilities',
  'coreUseCases',
  'system',
  'operationalConcepts',
  'standardCheck',
  'planningAssumptions',
  'activityList',
  'network',
  'normalSolution',
  'subcriticalSolution',
  'compressedSolution',
  'decompressedSolution',
  'riskModel',
  'sdpReview',
]);

assertEqual(
  'ReviewDecision (ordinals 1..3 only — hand has no "unknown" member)',
  gen.REVIEW_DECISION_APP_STRINGS.slice(1),
  ['approve', 'reject', 'withdraw']
);

assertEqual('SessionStage', gen.SESSION_STAGE_APP_STRINGS, [
  'unknown',
  'drafting',
  'awaitingReview',
  'redrafting',
  'committed',
  'withdrawn',
  'refused',
  'draftFailed',
]);

assertEqual('ConstructionStage', gen.CONSTRUCTION_STAGE_APP_STRINGS, [
  'unknown',
  'dispatching',
  'pipelineRunning',
  'reviewing',
  'awaitingTakeover',
  'paused',
  'exited',
  'awaitingApproval',
]);

assertEqual('OverrideKind (ordinals 1..4 only)', gen.OVERRIDE_KIND_APP_STRINGS.slice(1), [
  'takeover',
  'retry',
  'skip',
  'reassign',
]);

assertEqual('PhaseDecision (ordinals 1..2 only)', gen.PHASE_DECISION_APP_STRINGS.slice(1), [
  'approve',
  'sendBack',
]);

assertEqual('AutoscaleAction', gen.AUTOSCALE_ACTION_APP_STRINGS, [
  'noChange',
  'scaleUp',
  'scaleDown',
  'pause',
  'resume',
]);

assertEqual('ArtifactStage (adapters.ts slotStageFromOrdinal)', gen.ARTIFACT_STAGE_APP_STRINGS, [
  'empty',
  'awaitingReview',
  'committed',
  'rejected',
  'withdrawn',
]);

assertEqual(
  'ActivityType (adapters.ts activityRowKindFromOrdinal)',
  gen.ACTIVITY_TYPE_APP_STRINGS,
  ['service', 'frontend', 'testing', 'deployment', 'documentation']
);

assertEqual(
  'ProjectPhase (wire.ts projectPhaseFromOrdinal, only 3 real ordinals)',
  gen.PROJECT_PHASE_APP_STRINGS,
  ['systemDesign', 'projectDesign', 'construction']
);

assertEqual('SDPDecision (ordinals 1..2 only)', gen.SDP_DECISION_APP_STRINGS.slice(1), [
  'commit',
  'rejectAll',
]);

// --- Non-mechanical enums: confirm the KNOWN divergence, not a random one --

assertEqual(
  'ProjectSessionStage divergence is exactly the AssemblingSDP casing (index 2)',
  gen.PROJECT_SESSION_STAGE_GO_VARNAMES[2],
  'StageAssemblingSDP'
);

assertEqual(
  'PipelinePhase raw varnames include PipelineCancelled at ordinal 5 (hand collapses it into "failed")',
  gen.PIPELINE_PHASE_GO_VARNAMES[5],
  'PipelineCancelled'
);

console.log(failures === 0 ? '\nAll checks passed.' : `\n${failures} check(s) FAILED.`);
process.exit(failures === 0 ? 0 : 1);
