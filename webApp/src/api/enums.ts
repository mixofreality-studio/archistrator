/**
 * Wire-enum ordinal to app-string mappers.
 *
 * The generated OpenAPI schema encodes every Go enum as its integer ordinal (iota
 * order). The SPA view/state-machine logic is written against stable lowerCamel
 * strings, so the api layer converts at the wire boundary (hooks/wire.ts). Ordinals
 * are the authoritative Go iota order (server internal manager contract.gen.go and
 * resourceaccess projectstate). Every integer enum carries an unknown zero value.
 */
import type {
  ArtifactKind,
  ArtifactKindFull,
  CiStatus,
  ProjectArtifactKind,
  ProjectPhase,
  ProjectSessionStage,
  ReviewDecision,
  SDPDecision,
  SessionStage,
} from './types';
import type { ConstructionStage, PipelinePhase, OverrideKind } from './types';
import type { RuntimePhase } from './operationsTypes';
import type { components } from './schema';

type Schemas = components['schemas'];

// --- ArtifactKind (Phase-1 + full, shared 0..16 ordering) ------------------

const ARTIFACT_KIND_BY_ORDINAL: readonly ArtifactKindFull[] = [
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
];

const ARTIFACT_KIND_TO_ORDINAL: Record<ArtifactKindFull, number> =
  ARTIFACT_KIND_BY_ORDINAL.reduce<Record<string, number>>((acc, kind, i) => {
    acc[kind] = i;
    return acc;
  }, {});

export function artifactKindFromOrdinal(ordinal: number): ArtifactKindFull {
  return ARTIFACT_KIND_BY_ORDINAL[ordinal] ?? 'mission';
}

export function artifactKindToOrdinal(
  kind: ArtifactKindFull
): Schemas['SystemDesignArtifactKind'] {
  return ARTIFACT_KIND_TO_ORDINAL[kind] as Schemas['SystemDesignArtifactKind'];
}

/** Phase-1 narrowing — the same table, typed back to the Phase-1 union. */
export function systemArtifactKindFromOrdinal(ordinal: number): ArtifactKind {
  return artifactKindFromOrdinal(ordinal) as ArtifactKind;
}

/** Phase-2 narrowing — the same table, typed back to the Phase-2 union. */
export function projectArtifactKindFromOrdinal(ordinal: number): ProjectArtifactKind {
  return artifactKindFromOrdinal(ordinal) as ProjectArtifactKind;
}

// --- System-design SessionStage (0..7) -------------------------------------

const SESSION_STAGE_BY_ORDINAL: readonly SessionStage[] = [
  'unknown',
  'drafting',
  'awaitingReview',
  'redrafting',
  'committed',
  'withdrawn',
  'refused',
  'draftFailed',
];

export function sessionStageFromOrdinal(ordinal: number): SessionStage {
  return SESSION_STAGE_BY_ORDINAL[ordinal] ?? 'unknown';
}

// --- Project-design SessionStage (0..8) ------------------------------------

const PROJECT_SESSION_STAGE_BY_ORDINAL: readonly ProjectSessionStage[] = [
  'unknown',
  'drafting',
  'assemblingSdp',
  'awaitingReview',
  'redrafting',
  'committed',
  'withdrawn',
  'refused',
  'draftFailed',
];

export function projectSessionStageFromOrdinal(ordinal: number): ProjectSessionStage {
  return PROJECT_SESSION_STAGE_BY_ORDINAL[ordinal] ?? 'unknown';
}

// --- ReviewDecision / SDPDecision (request bodies) -------------------------

const REVIEW_DECISION_TO_ORDINAL: Record<ReviewDecision, number> = {
  approve: 1,
  reject: 2,
  withdraw: 3,
};

export function reviewDecisionToOrdinal(
  decision: ReviewDecision
): Schemas['SystemDesignReviewDecision'] {
  return REVIEW_DECISION_TO_ORDINAL[decision] as Schemas['SystemDesignReviewDecision'];
}

const SDP_DECISION_TO_ORDINAL: Record<SDPDecision, number> = {
  commit: 1,
  rejectAll: 2,
};

export function sdpDecisionToOrdinal(decision: SDPDecision): Schemas['ProjectDesignSDPDecision'] {
  return SDP_DECISION_TO_ORDINAL[decision] as Schemas['ProjectDesignSDPDecision'];
}

// --- ProjectPhase (0..2) ---------------------------------------------------

const PROJECT_PHASE_BY_ORDINAL: readonly ProjectPhase[] = [
  'systemDesign',
  'projectDesign',
  'construction',
];

export function projectPhaseFromOrdinal(ordinal: number): ProjectPhase {
  return PROJECT_PHASE_BY_ORDINAL[ordinal] ?? 'unknown';
}

// --- Construction enums ----------------------------------------------------

const CONSTRUCTION_STAGE_BY_ORDINAL: readonly ConstructionStage[] = [
  'unknown',
  'dispatching',
  'pipelineRunning',
  'reviewing',
  'awaitingTakeover',
  'paused',
  'exited',
];

export function constructionStageFromOrdinal(ordinal: number): ConstructionStage {
  return CONSTRUCTION_STAGE_BY_ORDINAL[ordinal] ?? 'unknown';
}

const PIPELINE_PHASE_BY_ORDINAL: readonly PipelinePhase[] = [
  'unknown',
  'pending',
  'running',
  'succeeded',
  'failed',
  'failed', // 5 = cancelled → surfaced as failed (the app has no distinct cancelled state)
];

export function pipelinePhaseFromOrdinal(ordinal: number): PipelinePhase {
  return PIPELINE_PHASE_BY_ORDINAL[ordinal] ?? 'unknown';
}

const OVERRIDE_KIND_TO_ORDINAL: Record<OverrideKind, number> = {
  takeover: 1,
  retry: 2,
  skip: 3,
  reassign: 4,
};

export function overrideKindToOrdinal(kind: OverrideKind): Schemas['ConstructionOverrideKind'] {
  return OVERRIDE_KIND_TO_ORDINAL[kind] as Schemas['ConstructionOverrideKind'];
}

// --- Project head-state row enums ------------------------------------------

/** ProjectCICheckState (0 Pending, 1 Success, 2 Failure) → app CiStatus. */
export function ciStatusFromOrdinal(ordinal: number): CiStatus {
  switch (ordinal) {
    case 1:
      return 'success';
    case 2:
      return 'failed';
    default:
      return 'in_progress';
  }
}

/** ProjectActivityType (0 service,1 frontend,2 testing,3 deployment,4 documentation). */
export function activityRowKindFromOrdinal(ordinal: number): 'service' | 'frontend' | 'testing' {
  switch (ordinal) {
    case 1:
      return 'frontend';
    case 2:
      return 'testing';
    default:
      return 'service';
  }
}

/** ProjectActivityBuildStatus (0 in-construction,1 in-review,2 integrated,3 failed). */
export function buildStatusRowFromOrdinal(
  ordinal: number
): 'integrated' | 'in-review' | 'in-construction' {
  switch (ordinal) {
    case 1:
      return 'in-review';
    case 2:
      return 'integrated';
    default:
      // 0 in-construction; 3 failed surfaces as in-construction (no terminal-fail row state).
      return 'in-construction';
  }
}

// --- Operations enums ------------------------------------------------------

/** OperationsRuntimeStatusSeam (0 unknown,1 pending,2 healthy,3 degraded,4 withdrawn). */
export function runtimePhaseFromOrdinal(ordinal: number): RuntimePhase {
  switch (ordinal) {
    case 1:
      return 'Pending';
    case 2:
      return 'Running';
    case 3:
      return 'Degraded';
    case 4:
      return 'Withdrawn';
    default:
      return 'Unknown';
  }
}

/** OperationsAutoscalerMode (0 unknown,1 auto,2 manual). */
export function autoscalerModeFromOrdinal(ordinal: number): string {
  switch (ordinal) {
    case 1:
      return 'Auto';
    case 2:
      return 'Manual';
    default:
      return 'Unknown';
  }
}

/** OperationsAutoscaleAction (0 noChange,1 scaleUp,2 scaleDown,3 pause,4 resume). */
export function autoscaleActionFromOrdinal(ordinal: number): string {
  switch (ordinal) {
    case 1:
      return 'scaleUp';
    case 2:
      return 'scaleDown';
    case 3:
      return 'pause';
    case 4:
      return 'resume';
    default:
      return 'noChange';
  }
}

// --- Operations desired-state-change ordinals (request bodies) -------------

/** OperationsDesiredStateReason ordinals. */
export const REASON_DEPLOY_AFTER_CONSTRUCTION = 1;
export const REASON_OPERATOR = 2;

/** OperationsPatchKind ordinals. */
export const PATCH_FULL_BUNDLE = 1;
export const PATCH_SCALE = 2;
export const PATCH_POLICY = 3;
