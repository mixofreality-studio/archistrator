/**
 * Static, human-facing metadata for each Method artifact slot — the label, the
 * state address where the artifact actually lives (shown as a mono sub-label),
 * and a one-line blurb. Keyed by the string ArtifactKind wire discriminator
 * (both phases). Pure data, no logic — the screens/adapters read it to build the
 * table-of-contents.
 *
 * The single hand-authored source for artifact-kind display data (appgen
 * step4-task5 consolidation): PHASE1_ORDER below is the ordered kind list (this
 * DISPLAY order is PRODUCT DATA — it legitimately differs from the wire's
 * ArtifactKind ordinal order, which runs mission..sdpReview across both phases,
 * not per-phase from 0) and
 * METHOD_METADATA[kind].title is the display label. types.ts used to duplicate
 * both (PHASE1_ARTIFACTS/PHASE2_DRAFTABLE_ARTIFACTS for order,
 * ARTIFACT_LABELS/PROJECT_ARTIFACT_LABELS for title) — deleted, since every
 * value was either identical to what lives here or (PHASE2_DRAFTABLE_ARTIFACTS)
 * unused. No separate numeric `order`/`phase` fields were added to
 * MethodArtifactMeta: a kind's position in PHASE1_ORDER already IS its order —
 * consumers that need an ordered, phase-scoped kind list read PHASE1_ORDER
 * directly (cast to the narrower ArtifactKind union, the pattern HomeBase.tsx
 * uses) rather than re-deriving it from per-entry fields.
 *
 * There was a PHASE2_ORDER beside it. Stage 5 Task 13 deleted it with its last
 * three readers — `adapters.toPhaseCards`, `graph/m0Gate.ts` and
 * `routes/ProjectDesignExperience.tsx` — all of which went with the design rails
 * and the construction console (spec §7.4). The Phase-2 kinds themselves are
 * untouched: they keep their METHOD_METADATA entries and their wire ordinals, and
 * the plan reaches them through the Project Design ACTIVITY.
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

/**
 * Phase-1 (System Design) artifacts, in server-exposed order.
 *
 * RETIRED IN PLACE (2026-08-30, founder ruling — Phase 1 collapses to Requirements
 * + Architecture): `scrubbedRequirements`, `operationalConcepts` and `standardCheck`
 * left the DRAFTING SEQUENCE and lost their step pages, but they remain valid wire
 * kinds with their original ordinals — every committed project.json still carries
 * them, `ArtifactKindFull`/`enums.gen.ts` still enumerate them, `toMarkdown` still
 * projects them, and their METHOD_METADATA entries below stay so slugForKind and the
 * Record<ArtifactKindFull, …> typing remain total. Nothing is renumbered.
 */
export const PHASE1_ORDER: readonly ArtifactKindFull[] = [
  'mission',
  'glossary',
  'volatilities',
  'coreUseCases',
  'system',
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
  // RETIRED IN PLACE — no longer in PHASE1_ORDER and no step page renders it. The
  // entry stays so the Record stays total and slugForKind still answers for old
  // deep links (an unresolvable slug falls back to the default step, see
  // SystemDesignContainer).
  scrubbedRequirements: {
    kind: 'scrubbedRequirements',
    title: 'Required Behaviors',
    stateAddress: 'project.json → slots.scrubbedRequirements',
    blurb:
      'The behaviors the system must exhibit — solutions-masquerading-as-requirements removed.',
    phrase: 'required behaviors',
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
  // RETIRED IN PLACE (see scrubbedRequirements above). The MODEL is very much alive:
  // the Architecture step's Deployment lens reads this committed slot's topology.
  operationalConcepts: {
    kind: 'operationalConcepts',
    title: 'Deployment & Operations Model',
    stateAddress: 'project.json → slots.operationalConcepts',
    blurb: 'Your per-project deployment choices, trust guarantees, and the runtime picture.',
    phrase: 'deployment & operations model',
    hasPmCritic: true,
  },
  // RETIRED IN PLACE (see scrubbedRequirements above).
  standardCheck: {
    kind: 'standardCheck',
    title: 'Design Health',
    stateAddress: 'project.json → slots.standardCheck',
    blurb: 'Live design-standard checks, standing waivers, and attestations before Phase 2.',
    phrase: 'design health',
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
