/**
 * The construction console's profile view over the platform's lifecycles.
 *
 * The per-activity-type lifecycle is platform-fixed method DATA (method-assets
 * lifecycles.json), generated into `activity/lifecycles.gen.ts`. It used to be a Go
 * table in projectStateAccess rendered into this directory as
 * `lifecycleTemplates.gen.ts`; the table is gone, and this module presents the data in
 * the shape the console's five consumers already speak, so nothing they render moves.
 *
 * What the data deliberately does NOT carry, and this module supplies:
 *  - `bookLabel`, Figure A-1's own name for a task, beside the profile's word for it;
 *  - the two conditional sub-attempt rows. `someConstruction` (Löwy's pre-design spike)
 *    and `testClient` (Construction's tandem partner) are sub-attempts of their phase's
 *    work task, not nodes of the DAG (spec §3), so the lifecycle has no node for them.
 *    The console renders a row for each ONLY once a real attempt exists — the filter
 *    lives in `list/activityTree.ts` and `detail/detailPaneState.ts`, which is why the
 *    row must exist here, flagged, for them to filter.
 *
 * This module is the OLD console's adapter and dies with it in stage 5; the Activity
 * Experience reads `LifecycleDef` directly.
 *
 * Pure — no React — pinned by lifecycleProfiles.test.ts.
 */
import {
  lifecycleFor,
  type LifecycleDef,
  type LifecyclePhaseDef,
  type LifecycleTypeKey,
} from '../activity/lifecycles.gen.ts';
import type { ActivityKind } from './KindBadge';
import type { TestingVariantName } from '../../contracts/types';

/** Canonical Method lifecycle phase (Righting Software Appendix A / Table A-1). */
export type LifecyclePhase =
  | 'requirements'
  | 'detailed_design'
  | 'test_plan'
  | 'construction'
  | 'integration';

/** One Figure A-1 task row within a lifecycle phase. */
export interface GeneratedTask {
  /** The Figure A-1 task KEY — invariant across profiles; the ledger's join key. */
  task: string;
  /** This profile's display label for the task (the lifecycle task's title). */
  label: string;
  /** The book's own name for the task. */
  bookLabel: string;
  /** True when this task's success IS the phase's binary exit criterion (App A). */
  gate: boolean;
  /** True when the row is a sub-attempt, rendered only if a real attempt exists. */
  conditional: boolean;
}

export interface GeneratedPhase {
  /** The phase's slash-command cell, e.g. `service-detailed-design`. */
  id: string;
  phase: LifecyclePhase;
  name: string;
  /** % contribution (App A Table A-1); weights sum to 100 per kind. */
  weight: number;
  /** This profile's binary exit criterion for the phase. */
  exitCriterion: string;
  tasks: readonly GeneratedTask[];
}

/** The book's own name per Figure A-1 task (server: projectstate's LabelForTask). */
const BOOK_LABEL: Readonly<Record<string, string>> = {
  srs: 'SRS',
  srsReview: 'SRS Review',
  stp: 'STP',
  stpReview: 'STP Review',
  someConstruction: 'Some Construction',
  detailedDesign: 'Detailed Design',
  designReview: 'Design Review',
  construction: 'Construction',
  testClient: 'Test Client',
  codeReview: 'Code Review',
  integration: 'Integration',
  testing: 'Testing',
};

/**
 * The full task-row order of the two phases that carry a sub-attempt, as the server's
 * phaseTasks stated it. Every other phase is simply [work, gate]: task KEYS never vary
 * by profile, so keying this by task id holds for all of them.
 */
const PHASE_TASK_ORDER: Partial<Record<LifecyclePhase, readonly string[]>> = {
  detailed_design: ['someConstruction', 'detailedDesign', 'designReview'],
  construction: ['construction', 'testClient', 'codeReview'],
};

const KIND_LIFECYCLE: Readonly<Record<ActivityKind, LifecycleTypeKey>> = {
  service: 'service',
  frontend: 'frontend',
  // A testing row that arrived without a variant reads as the plan variant — the same
  // fallback the server's zero TestingVariant makes.
  testing: 'testing:plan',
  deployment: 'deployment',
  documentation: 'documentation',
  uiDesign: 'uiDesign',
  integration: 'integration',
  requirements: 'requirements',
  architecture: 'architecture',
  projectDesign: 'projectDesign',
};

const VARIANT_LIFECYCLE: Readonly<Record<TestingVariantName, LifecycleTypeKey>> = {
  plan: 'testing:plan',
  harness: 'testing:harness',
  perf: 'testing:perf',
  systemTest: 'testing:systemTest',
  qaProcess: 'testing:qaProcess',
};

function taskRow(def: LifecycleDef, phase: LifecyclePhaseDef, id: string): GeneratedTask {
  const task = def.tasks.find((t) => t.id === id);
  const bookLabel = BOOK_LABEL[id] ?? id;
  return {
    task: id,
    // A sub-attempt is no lifecycle node, so the data has no title for it and the
    // book's own name is the honest label.
    label: task?.title ?? bookLabel,
    bookLabel,
    gate: id === phase.gate,
    conditional: task === undefined,
  };
}

function phaseRow(def: LifecycleDef, phase: LifecyclePhaseDef): GeneratedPhase {
  const work = def.tasks.find((t) => t.phase === phase.id && t.kind === 'dispatch');
  const order = PHASE_TASK_ORDER[phase.id as LifecyclePhase] ?? [
    ...(work === undefined ? [] : [work.id]),
    phase.gate,
  ];
  return {
    id: work?.command ?? '',
    phase: phase.id as LifecyclePhase,
    name: phase.label,
    weight: phase.weight,
    exitCriterion: phase.exitCriterion,
    tasks: order.map((id) => taskRow(def, phase, id)),
  };
}

function profileOf(key: LifecycleTypeKey): readonly GeneratedPhase[] {
  const def = lifecycleFor(key);
  return def === undefined ? [] : def.phases.map((p) => phaseRow(def, p));
}

/**
 * The activity's Figure A-1 profile, or `undefined` when the server did not classify
 * it. A testing activity uses its VARIANT profile (the five variants have genuinely
 * different phase sets); a testing row that arrived without a variant falls back to the
 * plan profile. ONE copy of this rule — it used to be pasted into detailPaneState,
 * taskBriefing and activityTree.
 */
export function profileFor(
  kind: ActivityKind | undefined,
  variant: TestingVariantName | undefined
): readonly GeneratedPhase[] | undefined {
  if (kind === undefined) return undefined;
  if (kind === 'testing' && variant !== undefined) return profileOf(VARIANT_LIFECYCLE[variant]);
  return profileOf(KIND_LIFECYCLE[kind]);
}

/** The canonical five, in Method order — the Service profile's order. */
export const SERVICE_PROFILE: readonly GeneratedPhase[] = profileOf('service');
