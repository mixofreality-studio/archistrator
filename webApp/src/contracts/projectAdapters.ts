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
  NetworkDependency,
  NetworkMilestone,
  NetworkModel,
  NetworkNodeCompute,
  NetworkSummary,
  PlanningAssumptionsModel,
  ProjectArtifactKind,
  ProjectArtifactModelEnvelope,
  RiskModelModel,
  SdpReviewModel,
  SolutionModel,
} from './types';

/** Re-exported so the renderers import the band type from one adapter surface. */
export type { FloatBand } from './types';

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

/** Formats a duration in days to at most 1 decimal place (e.g. "533.3 d"),
 * dropping the fraction entirely for whole-day durations (e.g. "737 d"). */
export function formatDurationDays(days: number): string {
  const rounded = Math.round(days * 10) / 10;
  return `${Number.isInteger(rounded) ? rounded.toFixed(0) : rounded.toFixed(1)} d`;
}

// ---------------------------------------------------------------------------
// Activity list → grouped rows by ID prefix.
// ---------------------------------------------------------------------------

export interface ActivityRowView {
  /** Network id (e.g. "C-CW"). */
  name: string;
  /** Human-readable activity name (e.g. "Build Web Client"); falls back to the id. */
  title: string;
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
      title: a.title !== undefined && a.title.length > 0 ? a.title : a.name,
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

// The near-critical float band (Löwy ch.8 §2 / server defaultBandPolicy): an
// OFF-critical-path activity whose total float is within this many days.
const NEAR_CRITICAL_DAYS = 5;
const YELLOW_MAX_FLOAT_DAYS = 25;

/** Float-criticality band from a node's total float (mirrors the server band policy:
 *  critical = 0 float / on-CP, red ≤5d, yellow ≤25d, green >25d). */
function bandOf(totalFloat: number, onCriticalPath: boolean): FloatBand {
  if (onCriticalPath || totalFloat <= 0) return 'critical';
  if (totalFloat <= NEAR_CRITICAL_DAYS) return 'red';
  if (totalFloat <= YELLOW_MAX_FLOAT_DAYS) return 'yellow';
  return 'green';
}

/**
 * Client-side CPM fallback. The network artifact authored on disk carries only
 * dependencies / criticalPath / milestones — the durations live on the activity
 * list. When the server hasn't run its compute-at-read pass (`computed`/`summary`
 * absent on the wire), this derives the same figures the server would: forward
 * pass (earliest start/finish + topological column), backward pass (latest
 * start/finish), total float, on-critical-path (zero float), float band, and the
 * project-level roll-up. Activity-on-node CPM over the dependency graph.
 */
function computeCpm(
  ids: readonly string[],
  deps: readonly NetworkDependency[],
  durationOf: (id: string) => number
): { computed: Map<string, NetworkNodeCompute>; summary: NetworkSummary } {
  const idSet = new Set(ids);
  const preds = new Map<string, string[]>();
  const succs = new Map<string, string[]>();
  for (const id of ids) {
    preds.set(id, []);
    succs.set(id, []);
  }
  for (const d of deps) {
    if (!idSet.has(d.activity)) continue;
    for (const p of d.dependsOn) {
      if (!idSet.has(p)) continue;
      preds.get(d.activity)?.push(p);
      succs.get(p)?.push(d.activity);
    }
  }

  // Kahn topological order (leaves-first). A cycle would strand nodes; we append any
  // stragglers so every id still gets a (best-effort) value rather than none.
  const indeg = new Map<string, number>();
  for (const id of ids) indeg.set(id, preds.get(id)?.length ?? 0);
  const ready = ids.filter((id) => (indeg.get(id) ?? 0) === 0);
  const order: string[] = [];
  while (ready.length > 0) {
    const cur = ready.shift();
    if (cur === undefined) break;
    order.push(cur);
    for (const s of succs.get(cur) ?? []) {
      const next = (indeg.get(s) ?? 0) - 1;
      indeg.set(s, next);
      if (next === 0) ready.push(s);
    }
  }
  if (order.length < ids.length) {
    const seen = new Set(order);
    for (const id of ids) if (!seen.has(id)) order.push(id);
  }

  // Forward pass: earliest start/finish + topological column (longest predecessor
  // chain in HOPS — the swimlane layer, independent of duration).
  const es = new Map<string, number>();
  const ef = new Map<string, number>();
  const col = new Map<string, number>();
  for (const id of order) {
    let start = 0;
    let layer = 0;
    for (const p of preds.get(id) ?? []) {
      start = Math.max(start, ef.get(p) ?? 0);
      layer = Math.max(layer, (col.get(p) ?? 0) + 1);
    }
    es.set(id, start);
    ef.set(id, start + durationOf(id));
    col.set(id, layer);
  }
  const projectEnd = order.reduce((m, id) => Math.max(m, ef.get(id) ?? 0), 0);

  // Backward pass: latest finish/start over the reverse topological order.
  const lf = new Map<string, number>();
  const ls = new Map<string, number>();
  for (let i = order.length - 1; i >= 0; i--) {
    const id = order[i];
    if (id === undefined) continue;
    const outs = succs.get(id) ?? [];
    const finish =
      outs.length === 0 ? projectEnd : outs.reduce((m, s) => Math.min(m, ls.get(s) ?? projectEnd), Infinity);
    lf.set(id, finish);
    ls.set(id, finish - durationOf(id));
  }

  const computed = new Map<string, NetworkNodeCompute>();
  for (const id of ids) {
    const totalFloat = (ls.get(id) ?? 0) - (es.get(id) ?? 0);
    const onCriticalPath = totalFloat <= 0;
    computed.set(id, {
      earliestStart: es.get(id) ?? 0,
      earliestFinish: ef.get(id) ?? 0,
      latestStart: ls.get(id) ?? 0,
      latestFinish: lf.get(id) ?? 0,
      totalFloat,
      freeFloat: 0,
      onCriticalPath,
      nearCritical: !onCriticalPath && totalFloat <= NEAR_CRITICAL_DAYS,
      band: bandOf(totalFloat, onCriticalPath),
      column: col.get(id) ?? 0,
    });
  }

  const values = [...computed.values()];
  const summary: NetworkSummary = {
    totalDurationDays: projectEnd,
    criticalPathActivityCount: values.filter((c) => c.onCriticalPath).length,
    criticalPathDays: projectEnd,
    maxFloat: values.reduce((m, c) => Math.max(m, c.totalFloat), 0),
    nearCriticalCount: values.filter((c) => c.nearCritical).length,
  };
  return { computed, summary };
}

/**
 * Maps the NetworkModel into the render-ready NetworkView. Prefers the server's
 * compute-at-read block (`computed[id]` CPM result + band, `summary` roll-up) when
 * present; otherwise derives the identical figures CLIENT-SIDE from the authored
 * dependency graph joined with the activity-list durations (see computeCpm). The
 * network artifact on disk carries only dependencies / criticalPath / milestones —
 * durations live on the activity list — so without this join the graph would be a
 * degenerate single column (all col 0) with a zeroed summary. Milestones join the
 * resolved CPM for their column / event time / criticality.
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

  const serverComputed = net.computed ?? {};

  // The activity universe = everything named in dependencies (activities + their
  // predecessors), so a node with no declared deps row still appears.
  const ids = new Set<string>();
  for (const d of net.dependencies) {
    ids.add(d.activity);
    for (const p of d.dependsOn) ids.add(p);
  }
  if (ids.size === 0 && (net.milestones ?? []).length === 0) return EMPTY_NETWORK_VIEW;

  // Prefer the server's compute-at-read block; otherwise run CPM client-side over the
  // dependency graph joined with the activity-list durations (the authored network
  // carries no durations/floats/columns — without this the graph collapses to col 0).
  const hasServerCompute = Object.keys(serverComputed).length > 0 && net.summary !== undefined;
  const fallback = hasServerCompute
    ? undefined
    : computeCpm([...ids], net.dependencies, (id) => activityByName.get(id)?.effortDays ?? 0);
  const cpmOf = (id: string): NetworkNodeCompute | undefined =>
    serverComputed[id] ?? fallback?.computed.get(id);

  // Ordered by resolved column then id for a stable left-to-right layered layout.
  const orderedIds = [...ids].sort((a, b) => {
    const ca = cpmOf(a)?.column ?? 0;
    const cb = cpmOf(b)?.column ?? 0;
    return ca !== cb ? ca - cb : a.localeCompare(b);
  });

  const activityNodes: NetworkNodeView[] = orderedIds.map((id) => {
    const c = cpmOf(id);
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

  const projectEnd = net.summary?.totalDurationDays ?? fallback?.summary.totalDurationDays ?? 0;

  // A milestone (zero-duration event node) is on the critical path when its event
  // time (the latest predecessor finish) coincides with the project end AND at least
  // one predecessor is itself critical. Authored onCriticalPath/eventTime win when
  // present; otherwise both are resolved from the CPM.
  const milestoneEventTime = (m: NetworkMilestone): number =>
    m.eventTime ?? (m.dependsOn ?? []).reduce((mx, p) => Math.max(mx, cpmOf(p)?.earliestFinish ?? 0), 0);
  const milestoneOnCp = (m: NetworkMilestone): boolean => {
    if (m.onCriticalPath !== undefined) return m.onCriticalPath;
    const preds = m.dependsOn ?? [];
    const anyCritical = preds.some((p) => cpmOf(p)?.onCriticalPath ?? false);
    return anyCritical && milestoneEventTime(m) >= projectEnd && projectEnd > 0;
  };

  // Milestones are zero-duration event nodes; they get their own band (critical
  // when on-CP, else green) and a column past the deepest predecessor.
  const milestones: NetworkMilestoneView[] = (net.milestones ?? []).map((m) => ({
    id: m.id,
    name: m.name,
    isPublic: m.public,
    onCriticalPath: milestoneOnCp(m),
    eventTime: milestoneEventTime(m),
  }));
  const milestoneNodes: NetworkNodeView[] = (net.milestones ?? []).map((m) => {
    const onCp = milestoneOnCp(m);
    const predCol = Math.max(-1, ...(m.dependsOn ?? []).map((p) => cpmOf(p)?.column ?? 0));
    return {
      id: m.id,
      kind: 'milestone',
      days: 0,
      workerClass: '',
      earlyStart: milestoneEventTime(m),
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
          (cpmOf(p)?.onCriticalPath ?? false) && (cpmOf(d.activity)?.onCriticalPath ?? false),
      });
    }
  }
  // Milestone fan-in edges (dependsOn → milestone); on-CP when the milestone is.
  for (const m of net.milestones ?? []) {
    const onCp = milestoneOnCp(m);
    for (const p of m.dependsOn ?? []) {
      edges.push({ from: p, to: m.id, onCriticalPath: onCp });
    }
  }

  // Prefer the server summary; else the client CPM roll-up.
  const s = net.summary ?? fallback?.summary;
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
  durationDays: number;
  totalCost: Money;
  included: boolean;
  exclusionReason: string;
}

export interface RiskModelView {
  rows: RiskRowView[];
  tooRiskyThreshold: number;
  overSafeThreshold: number;
}

const EMPTY_RISK_MODEL_VIEW: RiskModelView = { rows: [], tooRiskyThreshold: 0, overSafeThreshold: 0 };

/** Maps the typed RiskModel into per-option rows. */
export function toRiskRows(envelope: ProjectArtifactModelEnvelope | undefined): RiskRowView[] {
  const model = narrowProject(envelope, 'riskModel');
  if (model === undefined) return [];
  return model.rows.map((r) => ({
    solutionKind: r.solutionKind,
    criticalityRisk: r.criticalityRisk,
    activityRisk: r.activityRisk,
    composite: r.composite,
    durationDays: r.durationDays,
    totalCost: r.totalCost,
    included: r.included,
    exclusionReason: r.exclusionReason,
  }));
}

/** Maps the typed RiskModel into the full curve/exclusion-zone view. */
export function toRiskModelView(envelope: ProjectArtifactModelEnvelope | undefined): RiskModelView {
  const model = narrowProject(envelope, 'riskModel');
  if (model === undefined) return EMPTY_RISK_MODEL_VIEW;
  return {
    rows: toRiskRows(envelope),
    tooRiskyThreshold: model.tooRiskyThreshold,
    overSafeThreshold: model.overSafeThreshold,
  };
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
