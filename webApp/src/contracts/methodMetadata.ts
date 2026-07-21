/**
 * Static, human-facing metadata for each Method artifact slot — the label, the
 * state address where the artifact actually lives (shown as a mono sub-label),
 * and a one-line blurb. Keyed by the string ArtifactKind wire discriminator
 * (both phases). Pure data, no logic — the screens/adapters read it to build the
 * table-of-contents.
 *
 * The single hand-authored source for artifact-kind display data (appgen
 * step4-task5 consolidation): PHASE1_ORDER/PHASE2_ORDER below are the ordered
 * kind lists (this DISPLAY order is PRODUCT DATA — it legitimately differs from
 * the wire's ArtifactKind ordinal order, e.g. Phase-2's wire ordinals run
 * mission..sdpReview across both phases, not per-phase from 0) and
 * METHOD_METADATA[kind].title is the display label. types.ts used to duplicate
 * both (PHASE1_ARTIFACTS/PHASE2_DRAFTABLE_ARTIFACTS for order,
 * ARTIFACT_LABELS/PROJECT_ARTIFACT_LABELS for title) — deleted, since every
 * value was either identical to what lives here or (PHASE2_DRAFTABLE_ARTIFACTS)
 * unused. No separate numeric `order`/`phase` fields were added to
 * MethodArtifactMeta: a kind's position in PHASE1_ORDER/PHASE2_ORDER already IS
 * its order, and which array it appears in already IS its phase — consumers
 * that need an ordered, phase-scoped kind list read PHASE1_ORDER/PHASE2_ORDER
 * directly (cast to the narrower ArtifactKind/ProjectArtifactKind union, same
 * pattern as HomeBase.tsx/ProjectDesignExperience.tsx/DesignExperience.tsx)
 * rather than re-deriving it from per-entry fields.
 */
import type { ArtifactKindFull } from './types';

export interface MethodArtifactMeta {
  /** The string ArtifactKind wire discriminator. */
  kind: ArtifactKindFull;
  /** Human-readable title. */
  title: string;
  /**
   * Where this artifact's state actually lives — the git-as-DB head-state
   * (project.json) slot, not a file. The Method emits NO markdown/DSL files;
   * every artifact is a typed slot under `.slots`, so this is the honest
   * address rendered as a mono sub-label (replaces the old fictional filenames).
   */
  stateAddress: string;
  /** One-line description of the artifact's purpose. */
  blurb: string;
  /**
   * Short natural-language noun phrase for this artifact, used mid-sentence in the
   * generating scene's role line (e.g. "Architect is crafting the {phrase}"). Kept
   * lowercase and Method-true so it reads naturally after "the".
   */
  phrase: string;
  /** Whether the Method assigns a PM critic to this step. */
  hasPmCritic: boolean;
}

/** Phase-1 (System Design) artifacts, in server-exposed order. */
export const PHASE1_ORDER: readonly ArtifactKindFull[] = [
  'mission',
  'glossary',
  'scrubbedRequirements',
  'volatilities',
  'coreUseCases',
  'system',
  'operationalConcepts',
  'standardCheck',
] as const;

/** Phase-2 (Project Design) artifacts, in server-exposed order. */
export const PHASE2_ORDER: readonly ArtifactKindFull[] = [
  'planningAssumptions',
  'activityList',
  'network',
  'normalSolution',
  'decompressedSolution',
  'subcriticalSolution',
  'compressedSolution',
  'riskModel',
  'sdpReview',
] as const;

export const METHOD_METADATA: Record<ArtifactKindFull, MethodArtifactMeta> = {
  mission: {
    kind: 'mission',
    title: 'Mission',
    stateAddress: 'project.json → slots.mission',
    blurb: 'Business alignment — vision, objectives, mission statement.',
    phrase: 'vision and mission statement',
    hasPmCritic: true,
  },
  glossary: {
    kind: 'glossary',
    title: 'Glossary',
    stateAddress: 'project.json → slots.glossary',
    blurb: 'The ubiquitous language via the Four Questions.',
    phrase: 'glossary',
    hasPmCritic: true,
  },
  scrubbedRequirements: {
    kind: 'scrubbedRequirements',
    title: 'Scrubbed Requirements',
    stateAddress: 'project.json → slots.scrubbedRequirements',
    blurb: 'Solutions-masquerading-as-requirements removed.',
    phrase: 'scrubbed requirements',
    hasPmCritic: true,
  },
  volatilities: {
    kind: 'volatilities',
    title: 'Volatilities',
    stateAddress: 'project.json → slots.volatilities',
    blurb: 'Areas of change along the two axes — the architect’s signature.',
    phrase: 'areas of volatility',
    hasPmCritic: false,
  },
  coreUseCases: {
    kind: 'coreUseCases',
    title: 'Core Use Cases',
    stateAddress: 'project.json → slots.coreUseCases',
    blurb: 'The 2–6 use cases the architecture must satisfy.',
    phrase: 'core use cases',
    hasPmCritic: true,
  },
  system: {
    kind: 'system',
    title: 'Architecture',
    stateAddress: 'project.json → slots.system',
    blurb: 'Layered decomposition + a dynamic view for every use case.',
    phrase: 'architecture',
    hasPmCritic: false,
  },
  operationalConcepts: {
    kind: 'operationalConcepts',
    title: 'Operational Concepts',
    stateAddress: 'project.json → slots.operationalConcepts',
    blurb: 'Runtime interaction decisions, each tied to a business objective.',
    phrase: 'operational concepts',
    hasPmCritic: true,
  },
  standardCheck: {
    kind: 'standardCheck',
    title: 'Standard Check',
    stateAddress: 'project.json → slots.standardCheck',
    blurb: 'The Appendix C design-standard gate before Phase 2.',
    phrase: 'standard check',
    hasPmCritic: false,
  },
  planningAssumptions: {
    kind: 'planningAssumptions',
    title: 'Planning Assumptions',
    stateAddress: 'project.json → slots.planningAssumptions',
    blurb: 'Explicit resource, calendar, and dependency assumptions.',
    phrase: 'planning assumptions',
    hasPmCritic: false,
  },
  activityList: {
    kind: 'activityList',
    title: 'Activity List',
    stateAddress: 'project.json → slots.activityList',
    blurb: 'Coding + noncoding activities with 5-day quantum estimates.',
    phrase: 'activity list',
    hasPmCritic: false,
  },
  network: {
    kind: 'network',
    title: 'Project Network',
    stateAddress: 'project.json → slots.network',
    blurb: 'Activities as a network with float and the critical path.',
    phrase: 'project network',
    hasPmCritic: false,
  },
  normalSolution: {
    kind: 'normalSolution',
    title: 'Normal Solution',
    stateAddress: 'project.json → slots.normalSolution',
    blurb: 'Minimum staffing for unimpeded critical-path progress.',
    phrase: 'normal solution',
    hasPmCritic: false,
  },
  decompressedSolution: {
    kind: 'decompressedSolution',
    title: 'Decompressed Solution',
    stateAddress: 'project.json → slots.decompressedSolution',
    blurb: 'Extended duration to drop criticality risk toward the tipping point.',
    phrase: 'decompressed solution',
    hasPmCritic: false,
  },
  subcriticalSolution: {
    kind: 'subcriticalSolution',
    title: 'Subcritical Solution',
    stateAddress: 'project.json → slots.subcriticalSolution',
    blurb: 'Deliberately understaffed — longer, costlier, riskier than normal.',
    phrase: 'subcritical solution',
    hasPmCritic: false,
  },
  compressedSolution: {
    kind: 'compressedSolution',
    title: 'Compressed Solution',
    stateAddress: 'project.json → slots.compressedSolution',
    blurb: 'Shorter duration via parallel work then top resources.',
    phrase: 'compressed solution',
    hasPmCritic: false,
  },
  riskModel: {
    kind: 'riskModel',
    title: 'Risk Model',
    stateAddress: 'project.json → slots.riskModel',
    blurb: 'Criticality + activity risk per option; time-risk / time-cost curves.',
    phrase: 'risk model',
    hasPmCritic: false,
  },
  sdpReview: {
    kind: 'sdpReview',
    title: 'SDP Review',
    stateAddress: 'project.json → slots.sdpReview',
    blurb: 'The four options with duration / cost / risk and a recommendation.',
    phrase: 'SDP review',
    hasPmCritic: false,
  },
};

/**
 * The URL slug for an artifact kind — the kebab-case of its display title. Used
 * as the optional deep-link path segment for the design experiences
 * (…/design/system/scrubbed-requirements). Derived from METHOD_METADATA.title so
 * the slug set can never drift from the step set (no hand-maintained slug list).
 */
export function slugForKind(kind: ArtifactKindFull): string {
  return METHOD_METADATA[kind].title
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '');
}
