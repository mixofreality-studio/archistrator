/**
 * Static, human-facing metadata for each Method artifact slot — the label, the
 * filename The Method produces (shown as a mono sub-label), and a one-line blurb.
 * Keyed by the string ArtifactKind wire discriminator (both phases). Pure data,
 * no logic — the screens/adapters read it to build the table-of-contents.
 */
import type { ArtifactKindFull } from '../api/types';

export interface MethodArtifactMeta {
  /** The string ArtifactKind wire discriminator. */
  kind: ArtifactKindFull;
  /** Human-readable title. */
  title: string;
  /** Filename The Method emits for this artifact. */
  file: string;
  /** One-line description of the artifact's purpose. */
  blurb: string;
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
    file: 'mission.md',
    blurb: 'Business alignment — vision, objectives, mission statement.',
    hasPmCritic: true,
  },
  glossary: {
    kind: 'glossary',
    title: 'Glossary',
    file: 'glossary.md',
    blurb: 'The ubiquitous language via the Four Questions.',
    hasPmCritic: true,
  },
  scrubbedRequirements: {
    kind: 'scrubbedRequirements',
    title: 'Scrubbed Requirements',
    file: 'scrubbed-requirements.md',
    blurb: 'Solutions-masquerading-as-requirements removed.',
    hasPmCritic: true,
  },
  volatilities: {
    kind: 'volatilities',
    title: 'Volatilities',
    file: 'volatilities.md',
    blurb: 'Areas of change along the two axes — the architect’s signature.',
    hasPmCritic: false,
  },
  coreUseCases: {
    kind: 'coreUseCases',
    title: 'Core Use Cases',
    file: 'core-use-cases.md',
    blurb: 'The 2–6 use cases the architecture must satisfy.',
    hasPmCritic: true,
  },
  system: {
    kind: 'system',
    title: 'Architecture',
    file: 'architecture.dsl',
    blurb: 'Layered decomposition + one dynamic view per core use case.',
    hasPmCritic: false,
  },
  operationalConcepts: {
    kind: 'operationalConcepts',
    title: 'Operational Concepts',
    file: 'operational-concepts.md',
    blurb: 'Runtime interaction decisions, each tied to a business objective.',
    hasPmCritic: true,
  },
  standardCheck: {
    kind: 'standardCheck',
    title: 'Standard Check',
    file: 'standard-checklist.md',
    blurb: 'The Appendix C design-standard gate before Phase 2.',
    hasPmCritic: false,
  },
  planningAssumptions: {
    kind: 'planningAssumptions',
    title: 'Planning Assumptions',
    file: 'planning-assumptions.md',
    blurb: 'Explicit resource, calendar, and dependency assumptions.',
    hasPmCritic: false,
  },
  activityList: {
    kind: 'activityList',
    title: 'Activity List',
    file: 'activities.md',
    blurb: 'Coding + noncoding activities with 5-day quantum estimates.',
    hasPmCritic: false,
  },
  network: {
    kind: 'network',
    title: 'Project Network',
    file: 'network.yaml',
    blurb: 'Activities as a network with float and the critical path.',
    hasPmCritic: false,
  },
  normalSolution: {
    kind: 'normalSolution',
    title: 'Normal Solution',
    file: 'normal.md',
    blurb: 'Minimum staffing for unimpeded critical-path progress.',
    hasPmCritic: false,
  },
  decompressedSolution: {
    kind: 'decompressedSolution',
    title: 'Decompressed Solution',
    file: 'decompressed.md',
    blurb: 'Extended duration to drop criticality risk toward the tipping point.',
    hasPmCritic: false,
  },
  subcriticalSolution: {
    kind: 'subcriticalSolution',
    title: 'Subcritical Solution',
    file: 'subcritical.md',
    blurb: 'Deliberately understaffed — longer, costlier, riskier than normal.',
    hasPmCritic: false,
  },
  compressedSolution: {
    kind: 'compressedSolution',
    title: 'Compressed Solution',
    file: 'compressed.md',
    blurb: 'Shorter duration via parallel work then top resources.',
    hasPmCritic: false,
  },
  riskModel: {
    kind: 'riskModel',
    title: 'Risk Model',
    file: 'risk.md',
    blurb: 'Criticality + activity risk per option; time-risk / time-cost curves.',
    hasPmCritic: false,
  },
  sdpReview: {
    kind: 'sdpReview',
    title: 'SDP Review',
    file: 'sdp-review.md',
    blurb: 'The four options with duration / cost / risk and a recommendation.',
    hasPmCritic: false,
  },
};
