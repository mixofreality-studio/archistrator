/**
 * Pure logic for the Deployment & Operations Model (slot 6, wire kind
 * `operationalConcepts`): projecting the typed model into the per-project
 * ratifiable core the DeploymentOperationsView renders. Kept as a leaf module (no
 * runtime imports — types are erased) so it is directly unit-testable under
 * `node --test`, the same convention as glossaryLogic / volatilityMapLogic /
 * dynamicViewLabels; adapters.ts re-exports it, so callers still import from there.
 *
 * The platform-doctrine decisions are NOT here — they moved to the platform runtime
 * doctrine asset and are never per-project state.
 */
import type {
  ArtifactModelEnvelope,
  DeploymentOperationsModel,
  InfraBlock,
  Objective,
  ScalingPolicy,
} from './types';

/**
 * The per-project ratifiable core of the Deployment & Operations Model — the
 * customer's SELECTIONS (scenario / venue / review policy / scaling), the supported
 * infra building blocks, and the three customer trust summaries. Deployment topology
 * is a separate view (listDeploymentProfiles / toDeploymentView).
 */
export interface DeploymentOperationsView {
  deploymentScenario: string;
  constructionVenue: { kind: string; repositoryHost: string; note: string };
  reviewPolicyRef: string;
  /** Absent under the deployed-not-operated scenario (nothing operates, nothing scales). */
  scalingPolicy?: ScalingPolicy;
  infraBuildingBlocks: InfraBlock[];
  trustSummaries: { billing: string; usageMetering: string; dataOwnership: string };
  /**
   * Ch.-5 traceability: knob name → the mission business-objective numbers that
   * selection realizes. Optional — absent on states committed before the field
   * existed (views render nothing then).
   */
  objectiveLinks?: ObjectiveLinks;
}

// ── objectiveLinks traceability (Righting Software ch. 5) ────────────────────

/** The five per-project knobs, in canonical (render) order. */
export const DEPLOYMENT_KNOBS = [
  'deploymentScenario',
  'constructionVenue',
  'reviewPolicyRef',
  'scalingPolicy',
  'infraBuildingBlocks',
] as const;
export type DeploymentKnob = (typeof DEPLOYMENT_KNOBS)[number];

/** Human labels for the knobs — the "realized by" chips on the Mission view. */
export const KNOB_LABELS: Record<DeploymentKnob, string> = {
  deploymentScenario: 'Deployment scenario',
  constructionVenue: 'Construction venue',
  reviewPolicyRef: 'Review policy',
  scalingPolicy: 'Scaling',
  infraBuildingBlocks: 'Infrastructure building blocks',
};

/** The wire map (knob name → objective numbers); keys beyond the five knobs are
 *  tolerated and ignored. Undefined on older committed states. */
export type ObjectiveLinks = Record<string, null | number[]> | undefined;

/** One "Obj N" chip: the number plus the joined mission-objective statement
 *  (empty when the number resolves to no committed objective — chip renders,
 *  tooltip doesn't). */
export interface LinkedObjective {
  number: number;
  statement: string;
}

/**
 * The forward join for one knob: its cited objective numbers (deduped, authored
 * order) joined onto the committed mission objectives for tooltip statements.
 * Safe-empty when the links map is absent (older states) or the knob is unlinked.
 */
export function linkedObjectives(
  links: ObjectiveLinks,
  knob: DeploymentKnob,
  objectives: readonly Objective[]
): LinkedObjective[] {
  const numbers = links?.[knob] ?? [];
  const seen = new Set<number>();
  const out: LinkedObjective[] = [];
  for (const n of numbers) {
    if (seen.has(n)) continue;
    seen.add(n);
    out.push({ number: n, statement: objectives.find((o) => o.number === n)?.statement ?? '' });
  }
  return out;
}

/**
 * The reverse join for one mission objective: which knobs cite its number, in
 * canonical knob order. Safe-empty when the links map is absent — an objective no
 * knob cites renders nothing (coverage enforcement is the server's job).
 */
export function realizingKnobs(links: ObjectiveLinks, objectiveNumber: number): DeploymentKnob[] {
  if (links === undefined) return [];
  return DEPLOYMENT_KNOBS.filter((knob) => (links[knob] ?? []).includes(objectiveNumber));
}

/** Narrows an envelope to the reshaped deployment-ops model; undefined when absent. */
function asDeploymentModel(
  envelope: ArtifactModelEnvelope | undefined
): DeploymentOperationsModel | undefined {
  if (envelope?.kind !== 'operationalConcepts' || envelope.model === undefined) return undefined;
  return envelope.model as DeploymentOperationsModel;
}

/** Projects the typed model into the per-project view; undefined when the slot is empty. */
export function toDeploymentOperationsView(
  envelope: ArtifactModelEnvelope | undefined
): DeploymentOperationsView | undefined {
  const model = asDeploymentModel(envelope);
  if (model === undefined) return undefined;
  const venue = model.constructionVenue;
  const scaling = model.scalingPolicy;
  const trust = model.trustSummaries;
  return {
    deploymentScenario: model.deploymentScenario,
    constructionVenue: {
      kind: venue.kind,
      repositoryHost: venue.repositoryHost ?? '',
      note: venue.note ?? '',
    },
    reviewPolicyRef: model.reviewPolicyRef,
    ...(scaling != null ? { scalingPolicy: scaling } : {}),
    infraBuildingBlocks: model.infraBuildingBlocks ?? [],
    trustSummaries: {
      billing: trust.billing,
      usageMetering: trust.usageMetering,
      dataOwnership: trust.dataOwnership,
    },
    ...(model.objectiveLinks !== undefined ? { objectiveLinks: model.objectiveLinks } : {}),
  };
}
