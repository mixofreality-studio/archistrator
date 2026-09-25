/**
 * WHICH ARTIFACT a review task is about, and which of the SHIPPED review aids can
 * draw it (R17). Pure and React-free, so `node --test` loads it directly — which
 * is also why the relative VALUE imports carry an explicit `.ts`.
 *
 * ── This file REPLACED `construction/detail/bodies/ArtifactBody.tsx`'s dispatch ──
 * It was a PORT, written beside the originals in Task 9; Task 13 then deleted
 * them with the console. Three modules were its source, each cited where it
 * landed (the two deleted paths are named as history, not as live files):
 *
 *   detail/bodies/bodyDispatch.ts   `ArtifactBodyKind` (:64), `ARTIFACT_PHASES`
 *                                   (:89) — carried over verbatim below,
 *                                   comment and all — `isArtifactBodyKind`
 *                                   (:105) and `lifecyclePhaseOfTask` (:110).
 *   construction/artifactRenderers  the `Classification → renderer` REGISTRY
 *                                   itself, which is KEPT and imported by
 *                                   `ArtifactPanel`; only the path to it moves.
 *   detail/bodies/ArtifactBody.tsx  the BRANCH ORDER, which is the part that
 *                                   must survive: `service` falls to
 *                                   `ServiceContractView` through the contract
 *                                   JOIN (not through the registry, which has no
 *                                   `service` entry); `testing:plan` /
 *                                   `testing:systemTest` / `frontend` /
 *                                   `uiDesign` go to
 *                                   `artifactRenderers[classification]`;
 *                                   everything else falls to the unknown body,
 *                                   which is `{ kind: 'unavailable' }` here.
 *
 * ── ONE THING CHANGES SHAPE, ON PURPOSE ─────────────────────────────────────
 * `lifecyclePhaseOfTask` read the phase off a `ConstructionRow`'s generated
 * profile. The Activity Experience has no row to read on this path — a planned,
 * never-dispatched activity has none at all, and the review body must still say
 * what its gate is about — so the phase is read from `lifecycles.gen.ts`, the
 * same platform-fixed table the server generates that profile from.
 *
 * ── AND THE THREE DESIGN ACTIVITIES ARE NEW ─────────────────────────────────
 * `requirements`, `architecture` and `projectDesign` (activities 1–3) never
 * reached the old dispatch at all: their artifacts are committed SLOTS, not
 * construction rows, so they take a branch of their own straight to
 * `ArtifactRenderer` / `ProjectArtifactRenderer`.
 */
import type { ArtifactKindFull } from '../../contracts/types.ts';
import type { Classification } from '../construction/artifactClassification.ts';
import { artifactNotOfThisPhase, artifactUnavailable, NO_ARTIFACT_KIND } from './activityCopy.ts';
import { lifecycleKeyFor } from './activityViewToGraph.ts';
import { lifecycleFor } from './lifecycles.gen.ts';

/**
 * The six lifecycle artifact kinds that name a committed SLOT. The other five
 * ('SRS', 'DetailedDesign', 'Construction', 'Integration', 'STP') are
 * CONSTRUCTION artifacts with no slot and no app string — they are decided
 * through the phase decision, never through a design review op, so they are
 * absent here on purpose rather than mapped to something plausible.
 */
export const SLOT_KIND: Readonly<Record<string, ArtifactKindFull>> = {
  Mission: 'mission',
  Glossary: 'glossary',
  Volatilities: 'volatilities',
  CoreUseCases: 'coreUseCases',
  System: 'system',
  SdpReview: 'sdpReview',
};

/**
 * The Architecture activity's id — the one activity this screen knows by name,
 * because the Project Design M0 gate has NO send-back: its plan is derived, so
 * the only way to change it is to amend what it derives from (spec §6/R7). Both
 * the gate's `Amend Architecture →` link and its stale-basis reconcile open it,
 * so the id lives beside the rule that identifies that gate rather than in the
 * two components that navigate there.
 */
export const ARCHITECTURE_ACTIVITY_ID = 'architecture';

/**
 * The artifact classifications this stage actually renders — ported verbatim
 * from `bodyDispatch.ts:64`.
 *
 * A deliberate SUBSET of `Classification`. The three cut kinds
 * (deployment/documentation/integration) and the three testing variants with no
 * authored renderer (harness/perf/qaProcess) are absent on purpose: they resolve
 * to `undefined` and fall back to the unknown body, which says plainly that
 * there is nothing recorded to show. An empty renderer frame would say the
 * opposite.
 */
export type ArtifactBodyKind =
  | 'service'
  | 'uiDesign'
  | 'frontend'
  | 'testing:plan'
  | 'testing:systemTest';

/**
 * WHICH LIFECYCLE PHASE each renderer's artifact actually belongs to — ported
 * verbatim from `bodyDispatch.ts:89`, because it is the rule that stops the
 * service contract being labelled the SRS's artifact.
 *
 * The renderers are activity-scoped — `ServiceContractView` renders THE service
 * contract, `FrontendArtifactView` the ui-design concept and the built UI.
 * Dispatching on classification alone therefore puts the service contract under
 * `SRS`, a Requirements task, and labels it that task's artifact by placement
 * alone. That is a quieter version of exactly the mis-attribution this stage
 * keeps removing, so the phase is part of the question: outside its artifact's
 * own phase a task has no artifact to show.
 */
const ARTIFACT_PHASES: Record<ArtifactBodyKind, readonly string[]> = {
  // The service contract is placed by the contract JOIN rather than a phase
  // alone: a missing contract and a contract by design need different bodies.
  service: [],
  // A uiDesign activity's whole output is its Design Concept.
  uiDesign: ['detailed_design'],
  // FrontendArtifactView renders the built ui-code. The frontend's Detailed
  // Design is its client contract, placed by the JOIN.
  frontend: ['construction'],
  // The STP is written in Plan Authoring and signed off in Plan Review.
  'testing:plan': ['construction', 'integration'],
  // SystemTestRunView renders the run: execution, then regression & sign-off.
  'testing:systemTest': ['construction', 'integration'],
};

function isArtifactBodyKind(value: string): value is ArtifactBodyKind {
  return Object.prototype.hasOwnProperty.call(ARTIFACT_PHASES, value);
}

/**
 * The ONE phase whose artifact IS the service contract.
 *
 * `ARTIFACT_PHASES.service` is empty on purpose (see its own comment): the
 * contract never reaches the renderer REGISTRY, which has no `service` entry.
 * It is still placed on exactly one phase, and that placement is
 * `artifactPlacement.ts`'s `detailed_design → designPrimary(join)` (:110-118) —
 * named here rather than folded into `ARTIFACT_PHASES`, so that table keeps
 * meaning precisely what it meant: which phases reach `artifactRenderers[...]`.
 */
const SERVICE_CONTRACT_PHASE = 'detailed_design';

/** The three design activities (1–3): their artifact is a committed SLOT. */
const DESIGN_TYPES: ReadonlySet<string> = new Set([
  'requirements',
  'architecture',
  'projectDesign',
]);

export type TaskArtifact =
  /** ArtifactRenderer (Phase-1 kinds) / ProjectArtifactRenderer (Phase-2 kinds). */
  | { kind: 'slot'; artifactKind: ArtifactKindFull }
  /** ServiceContractView, over the contract JOIN. */
  | { kind: 'serviceContract'; componentId: string }
  /** components/construction/artifactRenderers.tsx. */
  | { kind: 'classified'; classification: Classification }
  /** An honest panel carrying this reason verbatim. */
  | { kind: 'unavailable'; reason: string };

/**
 * The generated lifecycle phase a task belongs to — every task sits in exactly
 * one. The `lifecycles.gen.ts` counterpart of `bodyDispatch.lifecyclePhaseOfTask`
 * (:110), which read the same data off a row's profile.
 */
export function lifecyclePhaseOfTask(typeKey: string, taskId: string): string | undefined {
  return lifecycleFor(typeKey)?.tasks.find((t) => t.id === taskId)?.phase;
}

/**
 * Every `Classification`, as a lookup. A `Record<Classification, true>` is what
 * makes this exhaustive: a new classification fails to compile here rather than
 * silently reading as "unknown activity kind".
 */
const CLASSIFICATIONS: Readonly<Record<Classification, true>> = {
  requirements: true,
  architecture: true,
  projectDesign: true,
  service: true,
  frontend: true,
  deployment: true,
  documentation: true,
  uiDesign: true,
  integration: true,
  'testing:plan': true,
  'testing:harness': true,
  'testing:perf': true,
  'testing:systemTest': true,
  'testing:qaProcess': true,
};

function isClassification(value: string): value is Classification {
  return Object.prototype.hasOwnProperty.call(CLASSIFICATIONS, value);
}

/**
 * The classification of an activity, from its (type, variant) pair. It is the
 * SAME key `lifecycleKeyFor` builds — `testing:<variant>` for a testing
 * activity, the bare type otherwise — which is exactly the vocabulary
 * `classify(row)` returns, so the two cannot drift.
 */
function classificationOf(type: string, variant: string | undefined): Classification | undefined {
  const key = lifecycleKeyFor(type, variant);
  return isClassification(key) ? key : undefined;
}

export function taskArtifactFor(input: {
  type: string;
  variant?: string | undefined;
  taskId: string;
  componentId?: string | undefined;
  /** The RESOLVED kind from `taskFactsFor` — a review task has none of its own. */
  artifactKind?: string | undefined;
}): TaskArtifact {
  // 1. A DESIGN activity's artifact is a committed slot, named by the resolved
  //    kind. Asked first: these three types have no construction row at all, so
  //    every later question would answer "no record" for an artifact that is
  //    sitting in project.json.
  if (DESIGN_TYPES.has(input.type)) {
    const slot = input.artifactKind === undefined ? undefined : SLOT_KIND[input.artifactKind];
    return slot === undefined
      ? { kind: 'unavailable', reason: NO_ARTIFACT_KIND }
      : { kind: 'slot', artifactKind: slot };
  }

  const classification = classificationOf(input.type, input.variant);
  if (classification === undefined) {
    return { kind: 'unavailable', reason: artifactUnavailable(input.type) };
  }

  // 2. A classification with no renderer at all — deployment, documentation,
  //    integration, and the three testing variants with no authored view. Said
  //    out loud rather than drawn as an empty frame (ArtifactBody.tsx:17-21).
  if (!isArtifactBodyKind(classification)) {
    return { kind: 'unavailable', reason: artifactUnavailable(classification) };
  }

  const phase = lifecyclePhaseOfTask(lifecycleKeyFor(input.type, input.variant), input.taskId);
  if (phase === undefined) {
    return { kind: 'unavailable', reason: artifactUnavailable(classification) };
  }

  // 3. The service contract, through the JOIN and only on its own phase.
  if (classification === 'service') {
    if (phase !== SERVICE_CONTRACT_PHASE) {
      return { kind: 'unavailable', reason: artifactNotOfThisPhase(classification) };
    }
    const componentId = input.componentId ?? '';
    return componentId.length > 0
      ? { kind: 'serviceContract', componentId }
      : { kind: 'unavailable', reason: artifactUnavailable(classification) };
  }

  // 4. The registry, scoped by ARTIFACT_PHASES.
  return ARTIFACT_PHASES[classification].some((p) => p === phase)
    ? { kind: 'classified', classification }
    : { kind: 'unavailable', reason: artifactNotOfThisPhase(classification) };
}
