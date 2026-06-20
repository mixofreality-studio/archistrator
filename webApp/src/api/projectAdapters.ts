/**
 * Pure adapters mapping the typed Phase-2 head-state / session models into
 * render-ready view models the Project-Design renderers consume. The Phase-2 TWIN
 * of api/adapters.ts. Every function is total and resilient to an absent model
 * (returns a safe empty view rather than throwing). No React here.
 *
 * The Method distinction: much of Project Design is COMPUTED (CPM derivations over
 * the one network) rather than authored. The CPM + float-band math now runs on the
 * SERVER (founder gate 2026-06-19); these adapters are PURE MAPPERS that read the
 * server's computed/summary blocks and shape them for the renderers — they never
 * re-derive floats, columns, or bands.
 */
import type {
  ActivityItem,
  ActivityListModel,
  FloatBand,
  Money,
  NetworkModel,
  PlanningAssumptionsModel,
  ProjectArtifactKind,
  ProjectArtifactModelEnvelope,
  RiskModelModel,
  SdpReviewModel,
  SolutionModel,
} from './types';

/** Re-exported so the renderers import the band type from one adapter surface. */
export type { FloatBand } from './types';
import type { Tokens } from '../theme/themes';

// ---------------------------------------------------------------------------
// Envelope narrowing.
// ---------------------------------------------------------------------------

interface ProjectModelForKind {
  planningAssumptions: PlanningAssumptionsModel;
  activityList: ActivityListModel;
  network: NetworkModel;
  normalSolution: SolutionModel;
  decompressedSolution: SolutionModel;
  subcriticalSolution: SolutionModel;
  compressedSolution: SolutionModel;
  riskModel: RiskModelModel;
  sdpReview: SdpReviewModel;
}

/** Narrows a Phase-2 envelope to the concrete model for `kind`, else undefined. */
export function narrowProject<K extends keyof ProjectModelForKind>(
  envelope: ProjectArtifactModelEnvelope | undefined,
  kind: K
): ProjectModelForKind[K] | undefined {
  if (envelope?.kind !== kind || envelope.model === undefined) return undefined;
  return envelope.model as ProjectModelForKind[K];
}

/** Whether the four-solution slot kind is one of the option solutions. */
export function isSolutionKind(kind: ProjectArtifactKind): boolean {
  return (
    kind === 'normalSolution' ||
    kind === 'decompressedSolution' ||
    kind === 'subcriticalSolution' ||
    kind === 'compressedSolution'
  );
}

/** A stable accent colour per solution option, shared by every Phase-2 renderer. */
export function solutionAccentColor(t: Tokens, kind: ProjectArtifactKind): string {
  if (kind === 'decompressedSolution') return t.committedDot;
  if (kind === 'compressedSolution') return t.accent;
  if (kind === 'normalSolution') return t.accent2;
  return t.muted;
}

// ---------------------------------------------------------------------------
// Money formatting.
// ---------------------------------------------------------------------------

/** Formats a Money value as a localized currency string (minor → major units). */
export function formatMoney(m: Money | undefined): string {
  if (m === undefined) return '—';
  const major = m.minorUnits / 100;
  try {
    return new Intl.NumberFormat(undefined, {
      style: 'currency',
      currency: m.currency.length > 0 ? m.currency : 'USD',
      maximumFractionDigits: 0,
    }).format(major);
  } catch {
    return `${major.toFixed(0)} ${m.currency}`;
  }
}

// ---------------------------------------------------------------------------
// Activity list → grouped rows by ID prefix.
// ---------------------------------------------------------------------------

export interface ActivityRowView {
  name: string;
  effortDays: number;
  workerClass: string;
  coding: boolean;
  riskBucket: number;
}

export interface ActivityGroupView {
  /** Worker class (the grouping key). */
  group: string;
  rows: ActivityRowView[];
  count: number;
  totalDays: number;
}

export interface ActivityListView {
  groups: ActivityGroupView[];
  totalActivities: number;
  totalPersonDays: number;
  codingCount: number;
  noncodingCount: number;
}

const EMPTY_ACTIVITY_LIST_VIEW: ActivityListView = {
  groups: [],
  totalActivities: 0,
  totalPersonDays: 0,
  codingCount: 0,
  noncodingCount: 0,
};

/** Maps the typed ActivityList model into grouped-by-worker-class view rows. */
export function toActivityListView(
  envelope: ProjectArtifactModelEnvelope | undefined
): ActivityListView {
  const model = narrowProject(envelope, 'activityList');
  if (model === undefined) return EMPTY_ACTIVITY_LIST_VIEW;
  const activities = model.activities;

  const byGroup = new Map<string, ActivityRowView[]>();
  for (const a of activities) {
    const key = a.workerClass.length > 0 ? a.workerClass : 'unassigned';
    const rows = byGroup.get(key) ?? [];
    rows.push({
      name: a.name,
      effortDays: a.effortDays,
      workerClass: a.workerClass,
      coding: a.coding,
      riskBucket: a.riskBucket,
    });
    byGroup.set(key, rows);
  }

  const groups: ActivityGroupView[] = [...byGroup.entries()].map(([group, rows]) => ({
    group,
    rows,
    count: rows.length,
    totalDays: rows.reduce((s, r) => s + r.effortDays, 0),
  }));

  return {
    groups,
    totalActivities: activities.length,
    totalPersonDays: activities.reduce((s, a) => s + a.effortDays, 0),
    codingCount: activities.filter((a) => a.coding).length,
    noncodingCount: activities.filter((a) => !a.coding).length,
  };
}

// ---------------------------------------------------------------------------
// Network → render-ready node/edge graph (PURE MAPPER over server CPM).
//
// The CPM + float-band math moved off the client onto the server (founder gate
// 2026-06-19). toNetworkView is now a pure mapper: it reads NetworkModel.computed
// (per-activity CPM result), .summary (the roll-up), .milestones, and joins the
// activity-list slot for the display-only fields the compute block doesn't carry
// (effort days, worker class, coding). It does NOT re-derive floats/columns/bands.
// ---------------------------------------------------------------------------

export type NetworkNodeKind = 'activity' | 'milestone';

export interface NetworkNodeView {
  id: string;
  kind: NetworkNodeKind;
  /** Activity effort (days) from the joined activity-list; 0 for milestones. */
  days: number;
  workerClass: string;
  /** Earliest start (server CPM), in days. */
  earlyStart: number;
  /** Total float (slack) — 0 on the critical path. Server-computed. */
  float: number;
  onCriticalPath: boolean;
  coding: boolean;
  /** Float-criticality band — straight from the server, never re-derived. */
  band: FloatBand;
  /** Topological depth column (longest-path layer) for the swimlane layout. */
  col: number;
  /** Milestone-only: a public demo gate vs an internal hurdle. */
  isPublic?: boolean;
  /** Human label (milestone name; activities reuse the id as label). */
  label: string;
}

export interface NetworkEdgeView {
  from: string;
  to: string;
  /** Both endpoints on the critical path → the path edge is bold/animated. */
  onCriticalPath: boolean;
}

export interface NetworkMilestoneView {
  id: string;
  name: string;
  isPublic: boolean;
  onCriticalPath: boolean;
  eventTime: number;
}

export interface NetworkView {
  nodes: NetworkNodeView[];
  edges: NetworkEdgeView[];
  criticalPath: string[];
  milestones: NetworkMilestoneView[];
  totalDurationDays: number;
  /** Count of activities on the critical path (= criticalPath.length). */
  criticalPathActivityCount: number;
  nearCriticalCount: number;
  /** The largest total float across all nodes — the loosest slack in the plan. */
  maxFloat: number;
}

const EMPTY_NETWORK_VIEW: NetworkView = {
  nodes: [],
  edges: [],
  criticalPath: [],
  milestones: [],
  totalDurationDays: 0,
  criticalPathActivityCount: 0,
  nearCriticalCount: 0,
  maxFloat: 0,
};

/**
 * Maps the server-computed NetworkModel into the render-ready NetworkView. Reads
 * `computed[id]` (CPM result + band per activity), `summary` (the roll-up), and
 * `milestones`, joining the activity-list slot only for display fields the compute
 * block omits (effortDays, workerClass, coding). NO CPM derivation here — if the
 * server hasn't computed yet (`computed`/`summary` absent) the affected nodes fall
 * back to safe zero/green so the renderer never throws.
 */
export function toNetworkView(
  networkEnvelope: ProjectArtifactModelEnvelope | undefined,
  activityEnvelope: ProjectArtifactModelEnvelope | undefined
): NetworkView {
  const net = narrowProject(networkEnvelope, 'network');
  if (net === undefined) return EMPTY_NETWORK_VIEW;

  const activityModel = narrowProject(activityEnvelope, 'activityList');
  const activityByName = new Map<string, ActivityItem>();
  for (const a of activityModel?.activities ?? []) activityByName.set(a.name, a);

  const computed = net.computed ?? {};

  // The activity universe = everything named in dependencies (activities + their
  // predecessors), so a node with no declared deps row still appears. Ordered by
  // computed column then id for a stable, server-driven layout.
  const ids = new Set<string>();
  for (const d of net.dependencies) {
    ids.add(d.activity);
    for (const p of d.dependsOn) ids.add(p);
  }
  if (ids.size === 0 && (net.milestones ?? []).length === 0) return EMPTY_NETWORK_VIEW;

  const orderedIds = [...ids].sort((a, b) => {
    const ca = computed[a]?.column ?? 0;
    const cb = computed[b]?.column ?? 0;
    return ca !== cb ? ca - cb : a.localeCompare(b);
  });

  const activityNodes: NetworkNodeView[] = orderedIds.map((id) => {
    const c = computed[id];
    const item = activityByName.get(id);
    return {
      id,
      kind: 'activity',
      days: item?.effortDays ?? 0,
      workerClass: item?.workerClass ?? '',
      earlyStart: c?.earliestStart ?? 0,
      float: c?.totalFloat ?? 0,
      onCriticalPath: c?.onCriticalPath ?? false,
      coding: item?.coding ?? false,
      band: c?.band ?? 'green',
      col: c?.column ?? 0,
      label: id,
    };
  });

  // Milestones are zero-duration event nodes; they get their own band (critical
  // when on-CP, else green) and a column past the deepest predecessor.
  const milestones: NetworkMilestoneView[] = (net.milestones ?? []).map((m) => ({
    id: m.id,
    name: m.name,
    isPublic: m.public,
    onCriticalPath: m.onCriticalPath ?? false,
    eventTime: m.eventTime ?? 0,
  }));
  const milestoneNodes: NetworkNodeView[] = (net.milestones ?? []).map((m) => {
    const onCp = m.onCriticalPath ?? false;
    const predCol = Math.max(-1, ...(m.dependsOn ?? []).map((p) => computed[p]?.column ?? 0));
    return {
      id: m.id,
      kind: 'milestone',
      days: 0,
      workerClass: '',
      earlyStart: m.eventTime ?? 0,
      float: 0,
      onCriticalPath: onCp,
      coding: false,
      band: onCp ? 'critical' : 'green',
      col: predCol + 1,
      isPublic: m.public,
      label: m.name,
    };
  });

  const edges: NetworkEdgeView[] = [];
  for (const d of net.dependencies) {
    for (const p of d.dependsOn) {
      edges.push({
        from: p,
        to: d.activity,
        onCriticalPath:
          (computed[p]?.onCriticalPath ?? false) && (computed[d.activity]?.onCriticalPath ?? false),
      });
    }
  }
  // Milestone fan-in edges (dependsOn → milestone); on-CP when the milestone is.
  for (const m of net.milestones ?? []) {
    for (const p of m.dependsOn ?? []) {
      edges.push({ from: p, to: m.id, onCriticalPath: m.onCriticalPath ?? false });
    }
  }

  const s = net.summary;
  return {
    nodes: [...activityNodes, ...milestoneNodes],
    edges,
    criticalPath: net.criticalPath,
    milestones,
    totalDurationDays: s?.totalDurationDays ?? 0,
    criticalPathActivityCount: s?.criticalPathActivityCount ?? net.criticalPath.length,
    nearCriticalCount: s?.nearCriticalCount ?? 0,
    maxFloat: s?.maxFloat ?? 0,
  };
}

// ---------------------------------------------------------------------------
// Solution → defining-knobs view.
// ---------------------------------------------------------------------------

export interface SolutionView {
  slotKind: ProjectArtifactKind;
  staffingCap: number;
  calendarDaysPerWeek: number;
  bufferDays: number;
  classRates: { workerClass: string; rate: Money }[];
}

/** Maps a typed Solution model into a defining-knobs view (or undefined when empty). */
export function toSolutionView(
  envelope: ProjectArtifactModelEnvelope | undefined,
  kind: ProjectArtifactKind
): SolutionView | undefined {
  const model = narrowProject(
    envelope,
    kind as 'normalSolution' | 'decompressedSolution' | 'subcriticalSolution' | 'compressedSolution'
  );
  if (model === undefined) return undefined;
  const classRates = Object.entries(model.classRates).map(([workerClass, rate]) => ({
    workerClass,
    rate,
  }));
  return {
    slotKind: model.slotKind,
    staffingCap: model.staffingCap,
    calendarDaysPerWeek: model.calendarDaysPerWeek,
    bufferDays: model.bufferDays,
    classRates,
  };
}

// ---------------------------------------------------------------------------
// Risk model → rows.
// ---------------------------------------------------------------------------

export interface RiskRowView {
  solutionKind: ProjectArtifactKind;
  criticalityRisk: number;
  activityRisk: number;
  composite: number;
}

/** Maps the typed RiskModel into per-option rows. */
export function toRiskRows(envelope: ProjectArtifactModelEnvelope | undefined): RiskRowView[] {
  const model = narrowProject(envelope, 'riskModel');
  if (model === undefined) return [];
  return model.rows.map((r) => ({
    solutionKind: r.solutionKind,
    criticalityRisk: r.criticalityRisk,
    activityRisk: r.activityRisk,
    composite: r.composite,
  }));
}

// ---------------------------------------------------------------------------
// SDP review → options table + curve points + recommendation.
// ---------------------------------------------------------------------------

export interface SdpOptionView {
  optionId: string;
  solutionKind: ProjectArtifactKind;
  durationDays: number;
  buildCost: Money;
  compositeRisk: number;
  projectedMonthlyCost: Money;
  expectedPerCycleNet: Money;
  revenueSharePercent: number;
  recommended: boolean;
}

export interface SdpReviewView {
  options: SdpOptionView[];
  recommendation: string;
  rationale: string;
}

const EMPTY_SDP_REVIEW_VIEW: SdpReviewView = { options: [], recommendation: '', rationale: '' };

/** Maps the assembled SdpReview model into the options table view. */
export function toSdpReviewView(envelope: ProjectArtifactModelEnvelope | undefined): SdpReviewView {
  const model = narrowProject(envelope, 'sdpReview');
  if (model === undefined) return EMPTY_SDP_REVIEW_VIEW;
  const options = model.options.map(
    (o): SdpOptionView => ({
      optionId: o.optionId,
      solutionKind: o.solutionKind,
      durationDays: o.durationDays,
      buildCost: o.buildCost,
      compositeRisk: o.compositeRisk,
      projectedMonthlyCost: o.projectedMonthlyCost,
      expectedPerCycleNet: o.expectedPerCycleNet,
      revenueSharePercent: o.revenueSharePercent,
      recommended: o.optionId === model.recommendation,
    })
  );
  return { options, recommendation: model.recommendation, rationale: model.rationale };
}
