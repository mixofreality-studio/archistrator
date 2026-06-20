/**
 * Pure adapters mapping the typed server head-state into render-ready view models
 * the screens / diagram renderers consume. Every function is total and resilient
 * to an absent `model` (locked / empty slots) — it returns a safe empty view
 * rather than throwing, so the UI can render a placeholder. No React here.
 *
 * The discriminator everywhere is the string `kind` on ArtifactModelEnvelope /
 * ArtifactSlotView; we narrow on it before reading the concrete typed model.
 */
import type { components } from './schema';
import type {
  ArtifactKindFull,
  ArtifactModelEnvelope,
  ArtifactSlotView,
  ProjectState,
  PlanningAssumptionsModel,
  ActivityListModel,
  NetworkModel,
  SolutionModel,
  RiskModelModel,
  SdpReviewModel,
  Money,
} from './types';
import { METHOD_METADATA, PHASE1_ORDER, PHASE2_ORDER } from '../constants/MethodMetadata';

type Schemas = components['schemas'];

// ---------------------------------------------------------------------------
// Phase spine — the three Method phases as locked/active/done cards.
// ---------------------------------------------------------------------------

/** Stable identifier for one of the three Method phases (used in routes/testids). */
export type PhaseId = 'systemDesign' | 'projectDesign' | 'construction';

/** One phase-card view model: progress + lock/active state for the home base. */
export interface PhaseCardView {
  id: PhaseId;
  index: number;
  title: string;
  subtitle: string;
  /** Committed slots in this phase. */
  done: number;
  /** Total artifact slots required in this phase (0 for construction). */
  total: number;
  /** True until the prior phase is the current/sealed phase. */
  locked: boolean;
  /** True when this is the project's current phase and still has owed slots. */
  active: boolean;
}

const PHASE_META: Record<PhaseId, { index: number; title: string; subtitle: string }> = {
  systemDesign: {
    index: 1,
    title: 'System Design',
    subtitle: 'Business alignment → volatilities → architecture.',
  },
  projectDesign: {
    index: 2,
    title: 'Project Design',
    subtitle: 'Activities, network, the four options, SDP review.',
  },
  construction: {
    index: 3,
    title: 'Construction',
    subtitle: 'Supervised build against the committed plan.',
  },
};

/** Phase-ordinal for lock comparison — earlier phases unlock later ones. */
const PHASE_ORDINAL: Record<Schemas['ProjectPhase'], number> = {
  systemDesign: 1,
  projectDesign: 2,
  construction: 3,
  unknown: 0,
};

/**
 * Builds the three phase cards from the project head-state. Phase progress is the
 * committed-slot count over the phase's required slots; a phase is locked until
 * the project has reached (or passed) it, and active when it is the current phase
 * with owed slots.
 */
export function toPhaseCards(project: ProjectState): PhaseCardView[] {
  const committed = new Set(
    project.slots.filter((s) => slotStageFromOrdinal(s.stage) === 'committed').map((s) => s.kind)
  );
  const current = PHASE_ORDINAL[project.phase];

  const card = (id: PhaseId, kinds: readonly ArtifactKindFull[]): PhaseCardView => {
    const meta = PHASE_META[id];
    const total = kinds.length;
    const done = kinds.filter((k) => committed.has(k)).length;
    const locked = meta.index > current;
    const active = meta.index === current && (total === 0 || done < total);
    return {
      id,
      index: meta.index,
      title: meta.title,
      subtitle: meta.subtitle,
      done,
      total,
      locked,
      active,
    };
  };

  return [
    card('systemDesign', PHASE1_ORDER),
    card('projectDesign', PHASE2_ORDER),
    card('construction', []),
  ];
}

// ---------------------------------------------------------------------------
// Table of contents — per-slot stage + metadata.
// ---------------------------------------------------------------------------

/** Display stage for one artifact slot, derived from the head-state ordinal. */
export type SlotStage = 'empty' | 'awaitingReview' | 'committed' | 'rejected' | 'withdrawn';

/** One table-of-contents row: static metadata joined with the live slot stage. */
export interface ArtifactMeta {
  kind: ArtifactKindFull;
  title: string;
  file: string;
  blurb: string;
  hasPmCritic: boolean;
  stage: SlotStage;
  /** Architect rationale on Reject / Withdraw, when present. */
  notes?: string;
}

/** Maps the ArtifactStage ordinal (0..4) to a display stage. */
export function slotStageFromOrdinal(ordinal: Schemas['ArtifactStageOrdinal']): SlotStage {
  switch (ordinal) {
    case 0:
      return 'empty';
    case 1:
      return 'awaitingReview';
    case 2:
      return 'committed';
    case 3:
      return 'rejected';
    case 4:
      return 'withdrawn';
  }
}

/** Builds the table-of-contents rows from a project's head-state slots. */
export function toArtifactTableOfContents(project: ProjectState): ArtifactMeta[] {
  return project.slots.map((slot) => toArtifactMeta(slot));
}

function toArtifactMeta(slot: ArtifactSlotView): ArtifactMeta {
  const meta = METHOD_METADATA[slot.kind];
  return {
    kind: slot.kind,
    title: meta.title,
    file: meta.file,
    blurb: meta.blurb,
    hasPmCritic: meta.hasPmCritic,
    stage: slotStageFromOrdinal(slot.stage),
    ...(slot.notes !== undefined && slot.notes.length > 0 ? { notes: slot.notes } : {}),
  };
}

// ---------------------------------------------------------------------------
// Volatilities → scatter points on the two axes.
// ---------------------------------------------------------------------------

/** One volatility placed on the two-axis map. */
export interface VolatilityPoint {
  name: string;
  rationale: string;
  axis: Schemas['Axis'];
  /** 0..1 — strength on Axis 2 (all customers, one moment). */
  x: number;
  /** 0..1 — strength on Axis 1 (same customer, over time). */
  y: number;
}

export interface VolatilityView {
  points: VolatilityPoint[];
}

const EMPTY_VOLATILITY_VIEW: VolatilityView = { points: [] };

/**
 * Maps the typed Volatilities model into scatter points. The typed model carries
 * no coordinates, so we place each point deterministically: axis decides the
 * dominant dimension, and the per-axis index spreads points across the band.
 */
export function toVolatilityView(envelope: ArtifactModelEnvelope | undefined): VolatilityView {
  const model = narrow(envelope, 'volatilities');
  if (model === undefined) return EMPTY_VOLATILITY_VIEW;
  const items = model.items ?? [];

  const axisCounts: Record<Schemas['Axis'], number> = {
    sameCustomerOverTime: 0,
    allCustomersAtOneTime: 0,
  };
  const totals: Record<Schemas['Axis'], number> = {
    sameCustomerOverTime: items.filter((i) => i.axis === 'sameCustomerOverTime').length,
    allCustomersAtOneTime: items.filter((i) => i.axis === 'allCustomersAtOneTime').length,
  };

  const points = items.map((item): VolatilityPoint => {
    const index = axisCounts[item.axis];
    axisCounts[item.axis] += 1;
    const total = Math.max(totals[item.axis], 1);
    const spread = (index + 1) / (total + 1); // 0..1 within the band
    const strong = 0.55 + spread * 0.4; // dominant dimension
    const weak = 0.1 + spread * 0.25; // secondary dimension
    const isAxis1 = item.axis === 'sameCustomerOverTime';
    return {
      name: item.name,
      rationale: item.rationale,
      axis: item.axis,
      x: isAxis1 ? weak : strong,
      y: isAxis1 ? strong : weak,
    };
  });

  return { points };
}

export const AXIS1_LABEL = 'Axis 1 — same customer, over time';
export const AXIS2_LABEL = 'Axis 2 — all customers, one moment';

// ---------------------------------------------------------------------------
// System → C4 component view.
// ---------------------------------------------------------------------------

export interface C4Component {
  id: string;
  name: string;
  kind: Schemas['ComponentKind'];
  layer: Schemas['Layer'];
  /** The volatility this component encapsulates (empty for Resource / Utility). */
  encapsulates: string;
}

export interface C4Relationship {
  from: string;
  to: string;
  mode: Schemas['CallMode'];
  label: string;
}

export interface C4View {
  components: C4Component[];
  relationships: C4Relationship[];
}

const EMPTY_C4_VIEW: C4View = { components: [], relationships: [] };

/** Maps the typed System model into a C4 component + relationship view. */
export function toC4View(envelope: ArtifactModelEnvelope | undefined): C4View {
  const model = narrow(envelope, 'system');
  if (model === undefined) return EMPTY_C4_VIEW;
  const components = (model.components ?? []).map(
    (c): C4Component => ({
      id: c.id,
      name: c.name,
      kind: c.kind,
      layer: c.layer,
      encapsulates: c.encapsulates,
    })
  );
  const relationships = (model.relationships ?? []).map(
    (r): C4Relationship => ({ from: r.from, to: r.to, mode: r.mode, label: r.label })
  );
  return { components, relationships };
}

// ---------------------------------------------------------------------------
// System → dynamic view (one call-chain per use case).
// ---------------------------------------------------------------------------

/** A C4 relationship carrying its 1-based position in an ordered call chain. */
export type SequencedRelationship = C4Relationship & { seq: number };

/** A single dynamic call-chain view: ordered participants + sequenced edges. */
export interface DynamicViewModel {
  title: string;
  participants: C4Component[];
  edges: SequencedRelationship[];
}

const EMPTY_DYNAMIC_VIEW: DynamicViewModel = { title: '', participants: [], edges: [] };

/** A pickable dynamic-view reference: its stable key + display title. */
export interface DynamicViewRef {
  key: string;
  title: string;
}

/** Lists the System's dynamic views (key + title) for a picker. Empty when absent. */
export function listDynamicViews(envelope: ArtifactModelEnvelope | undefined): DynamicViewRef[] {
  const model = narrow(envelope, 'system');
  if (model === undefined) return [];
  return (model.dynamicViews ?? []).map((v) => ({ key: v.key, title: v.title }));
}

/**
 * Returns the subset of the System's dynamic views whose participant list includes
 * the given componentId (kebab-case, e.g. "web-client"). Empty when absent or when
 * no view includes the component.
 */
export function listDynamicViewsForComponent(
  envelope: ArtifactModelEnvelope | undefined,
  componentId: string
): DynamicViewRef[] {
  const model = narrow(envelope, 'system');
  if (model === undefined || componentId.length === 0) return [];
  return (model.dynamicViews ?? [])
    .filter((v) => (v.participants ?? []).includes(componentId))
    .map((v) => ({ key: v.key, title: v.title }));
}

/**
 * Maps one named DynamicView of the System model into render-ready participants +
 * ordered, sequence-numbered edges. Participants are looked up against the full
 * component set (unknown ids are dropped); edges are numbered 1..n in declared
 * order. Absent system / missing key → an empty view.
 */
export function toDynamicView(
  envelope: ArtifactModelEnvelope | undefined,
  key: string
): DynamicViewModel {
  const model = narrow(envelope, 'system');
  if (model === undefined) return EMPTY_DYNAMIC_VIEW;
  const view = (model.dynamicViews ?? []).find((v) => v.key === key);
  if (view === undefined) return EMPTY_DYNAMIC_VIEW;

  const byId = new Map<string, C4Component>();
  for (const c of model.components ?? []) {
    byId.set(c.id, {
      id: c.id,
      name: c.name,
      kind: c.kind,
      layer: c.layer,
      encapsulates: c.encapsulates,
    });
  }

  const participants = (view.participants ?? [])
    .map((id) => byId.get(id))
    .filter((c): c is C4Component => c !== undefined);

  const edges = (view.edges ?? []).map(
    (r, i): SequencedRelationship => ({
      from: r.from,
      to: r.to,
      mode: r.mode,
      label: r.label,
      seq: i + 1,
    })
  );

  return { title: view.title, participants, edges };
}

// ---------------------------------------------------------------------------
// System → component perspective (one component, its inbound + outbound edges).
// ---------------------------------------------------------------------------

/** A component-focused slice of the static view: the focus + its two edge sets. */
export interface PerspectiveModel {
  focus: C4Component | undefined;
  inbound: C4Relationship[];
  outbound: C4Relationship[];
}

/**
 * Pure derivation: given the static C4 view and a component id, returns that
 * component plus the relationships pointing INTO it (inbound) and OUT of it
 * (outbound). Unknown id → an empty (undefined-focus) perspective.
 */
export function toPerspective(view: C4View, componentId: string): PerspectiveModel {
  const focus = view.components.find((c) => c.id === componentId);
  const inbound = view.relationships.filter((r) => r.to === componentId);
  const outbound = view.relationships.filter((r) => r.from === componentId);
  return { focus, inbound, outbound };
}

// ---------------------------------------------------------------------------
// OperationalConcepts → deployment view (one profile's nested topology).
// ---------------------------------------------------------------------------

/** A System component instance placed in a deployment node, joined to its layer. */
export interface DeploymentInstance {
  componentId: string;
  name: string;
  layer: Schemas['Layer'];
  note: string;
}

/** A nested deployment node: child nodes + the component instances it hosts. */
export interface DeploymentNodeView {
  name: string;
  technology: string;
  children: DeploymentNodeView[];
  instances: DeploymentInstance[];
}

/** A pickable deployment-environment reference: its profile + display title. */
export interface DeploymentProfileRef {
  profile: Schemas['DeploymentProfile'];
  title: string;
}

/**
 * Lists the deployment environments present in the OperationalConcepts model
 * (profile + title) for a profile switcher. Empty when deployment is absent.
 */
export function listDeploymentProfiles(
  opEnvelope: ArtifactModelEnvelope | undefined
): DeploymentProfileRef[] {
  const op = narrow(opEnvelope, 'operationalConcepts');
  return (op?.deployment?.environments ?? []).map((e) => ({ profile: e.profile, title: e.title }));
}

/**
 * Builds the nested deployment topology for one profile, joining each
 * ContainerInstance to its System Component's name + layer (so the renderer can
 * colour instances by layer). Absent deployment / missing profile environment →
 * undefined, so the caller renders nothing.
 */
export function toDeploymentView(
  opEnvelope: ArtifactModelEnvelope | undefined,
  systemEnvelope: ArtifactModelEnvelope | undefined,
  profile: Schemas['DeploymentProfile']
): DeploymentNodeView[] | undefined {
  const op = narrow(opEnvelope, 'operationalConcepts');
  const env = (op?.deployment?.environments ?? []).find((e) => e.profile === profile);
  if (env === undefined) return undefined;

  const system = narrow(systemEnvelope, 'system');
  const byId = new Map<string, { name: string; layer: Schemas['Layer'] }>();
  for (const c of system?.components ?? []) {
    byId.set(c.id, { name: c.name, layer: c.layer });
  }

  const mapNode = (node: Schemas['DeploymentNode']): DeploymentNodeView => ({
    name: node.name,
    technology: node.technology,
    children: (node.children ?? []).map(mapNode),
    instances: (node.instances ?? []).map((inst): DeploymentInstance => {
      const comp = byId.get(inst.componentId);
      return {
        componentId: inst.componentId,
        name: comp?.name ?? inst.componentId,
        layer: comp?.layer ?? 'utility',
        note: inst.note,
      };
    }),
  });

  return (env.nodes ?? []).map(mapNode);
}

// ---------------------------------------------------------------------------
// Core use cases → activity views (lanes / nodes / edges).
// ---------------------------------------------------------------------------

export interface ActivityNodeView {
  id: string;
  kind: Schemas['ActivityNodeKind'];
  label: string;
  /** The swim-lane (role) this node sits in. */
  lane: string;
}

export interface ActivityEdgeView {
  from: string;
  to: string;
  kind: Schemas['EdgeKind'];
  /** Guard text on a guardedFlow edge (empty otherwise). */
  guard: string;
}

export interface UseCaseView {
  id: string;
  name: string;
  classification: Schemas['Classification'];
  rejectionReason: string;
  /** Distinct swim-lanes, in first-seen order. */
  lanes: string[];
  nodes: ActivityNodeView[];
  edges: ActivityEdgeView[];
}

export interface CoreUseCasesView {
  useCases: UseCaseView[];
}

const EMPTY_USE_CASES_VIEW: CoreUseCasesView = { useCases: [] };

/** Maps the typed CoreUseCases model into per-use-case activity views. */
export function toCoreUseCasesView(envelope: ArtifactModelEnvelope | undefined): CoreUseCasesView {
  const model = narrow(envelope, 'coreUseCases');
  if (model === undefined) return EMPTY_USE_CASES_VIEW;
  const decisions = model.decisions ?? [];
  return { useCases: decisions.map((d) => toUseCaseView(d)) };
}

function toUseCaseView(decision: Schemas['UseCaseDecision']): UseCaseView {
  const uc = decision.useCase;
  const activity = uc.activity ?? null;
  const rawNodes = activity?.nodes ?? [];
  const rawEdges = activity?.edges ?? [];

  const nodes = rawNodes.map(
    (n): ActivityNodeView => ({
      id: n.id,
      kind: n.kind,
      label: n.label,
      lane: n.roleName.length > 0 ? n.roleName : 'Machine',
    })
  );
  const edges = rawEdges.map(
    (e): ActivityEdgeView => ({ from: e.from, to: e.to, kind: e.kind, guard: e.guard })
  );

  const lanes: string[] = [];
  for (const node of nodes) {
    if (!lanes.includes(node.lane)) lanes.push(node.lane);
  }

  return {
    id: uc.id,
    name: uc.name,
    classification: uc.classification,
    rejectionReason: decision.rejectionReason,
    lanes,
    nodes,
    edges,
  };
}

// ---------------------------------------------------------------------------
// Prose kinds → markdown strings.
// ---------------------------------------------------------------------------

/**
 * Renders the prose-style artifact models (mission / glossary /
 * scrubbedRequirements / operationalConcepts / standardCheck) into a markdown
 * string. Returns an empty string when the slot has no staged model.
 */
export function toMarkdown(envelope: ArtifactModelEnvelope | undefined): string {
  if (envelope?.model === undefined) return '';
  const { kind, model } = envelope;
  // Non-prose kinds (volatilities / coreUseCases / system) have dedicated diagram
  // adapters; everything else with no markdown projection renders nothing.
  if (kind === 'mission') return missionToMarkdown(model as Schemas['MissionStatement']);
  if (kind === 'glossary') return glossaryToMarkdown(model as Schemas['Glossary']);
  if (kind === 'scrubbedRequirements') {
    return scrubbedRequirementsToMarkdown(model as Schemas['ScrubbedRequirements']);
  }
  if (kind === 'operationalConcepts') {
    return operationalConceptsToMarkdown(model as Schemas['OperationalConcepts']);
  }
  if (kind === 'standardCheck') return standardCheckToMarkdown(model as Schemas['StandardCheck']);
  // Phase-2 kinds — the home base passes these through ArtifactModelEnvelope
  // (ArtifactKindFull includes both phases); the model is the hand-mirrored type.
  if (kind === 'planningAssumptions')
    return planningAssumptionsToMarkdown(model as unknown as PlanningAssumptionsModel);
  if (kind === 'activityList')
    return activityListToMarkdown(model as unknown as ActivityListModel);
  if (kind === 'network') return networkToMarkdown(model as unknown as NetworkModel);
  if (
    kind === 'normalSolution' ||
    kind === 'decompressedSolution' ||
    kind === 'subcriticalSolution' ||
    kind === 'compressedSolution'
  ) return solutionToMarkdown(model as unknown as SolutionModel);
  if (kind === 'riskModel') return riskModelToMarkdown(model as unknown as RiskModelModel);
  if (kind === 'sdpReview') return sdpReviewToMarkdown(model as unknown as SdpReviewModel);
  return '';
}

function missionToMarkdown(m: Schemas['MissionStatement']): string {
  const objectives = (m.objectives ?? [])
    .map((o) => `${String(o.number)}. ${o.statement}`)
    .join('\n');
  const parts = [`## Vision\n\n${m.vision}`];
  if (objectives.length > 0) parts.push(`## Business Objectives\n\n${objectives}`);
  parts.push(`## Mission Statement\n\n${m.mission}`);
  return parts.join('\n\n');
}

function glossaryToMarkdown(g: Schemas['Glossary']): string {
  const items = g.items ?? [];
  if (items.length === 0) return '';
  const rows = items
    .map((i) => {
      const hasCategory = (i.category?.length ?? 0) > 0;
      const category = hasCategory ? ` _(${String(i.category)})_` : '';
      return `- **${i.term}**${category} — ${i.definition}`;
    })
    .join('\n');
  return `## Glossary\n\n${rows}`;
}

function scrubbedRequirementsToMarkdown(r: Schemas['ScrubbedRequirements']): string {
  const items = r.items ?? [];
  if (items.length === 0) return '';
  const rows = items.map((i) => `- **${i.id}** — ${i.statement}`).join('\n');
  return `## Scrubbed Requirements\n\n${rows}`;
}

function operationalConceptsToMarkdown(o: Schemas['OperationalConcepts']): string {
  const decisions = o.decisions ?? [];
  if (decisions.length === 0) return '';
  const rows = decisions
    .map((d) => `- **${d.topic}** — ${d.decision} _(objective ${String(d.justifyingObjective)})_`)
    .join('\n');
  return `## Operational Concepts\n\n${rows}`;
}

function standardCheckToMarkdown(s: Schemas['StandardCheck']): string {
  const items = s.items ?? [];
  if (items.length === 0) return '';
  const rows = items
    .map((i) => {
      const just =
        i.status === 'waived' && i.justification.length > 0
          ? ` — _waived: ${i.justification}_`
          : '';
      return `- \`${i.status.toUpperCase()}\` ${i.section}: ${i.guideline}${just}`;
    })
    .join('\n');
  return `## Standard Check\n\n${rows}`;
}

// ---------------------------------------------------------------------------
// Phase-2 prose → markdown helpers.
// ---------------------------------------------------------------------------

/** Format a Money value as "$X.XX USD" (minor units are cents). */
function formatMoney(m: Money): string {
  const dollars = (m.minorUnits / 100).toFixed(2);
  return `$${dollars} ${m.currency}`;
}

/** Integer ordinal labels for the Go InfrastructureKind enum (0-based). */
const INFRA_KIND_LABELS: Record<number, string> = {
  0: 'Unknown',
  1: 'Go + Temporal + Postgres',
};

/** Integer ordinal labels for the Go RevenueShare enum (0-based). */
const REVENUE_SHARE_LABELS: Record<number, string> = {
  0: 'None',
  1: 'Launch flat 10%',
  2: 'Negotiated rate',
};

/** Integer ordinal labels for the Go ComputeCost enum (0-based). */
const COMPUTE_COST_LABELS: Record<number, string> = {
  0: 'Unknown',
  1: 'Flat markup',
  2: 'Tiered floors',
};

/** Integer ordinal labels for the Go Schedule enum (0-based). */
const SCHEDULE_LABELS: Record<number, string> = {
  0: 'Unknown',
  1: 'Monthly',
  2: 'Weekly',
  3: 'Daily',
};

function labelFor(labels: Record<number, string>, value: number): string {
  return labels[value] ?? String(value);
}

function planningAssumptionsToMarkdown(m: PlanningAssumptionsModel): string {
  const parts: string[] = [];

  // Resources
  if ((m.resources ?? []).length > 0) {
    parts.push(`## Resources\n\n${m.resources.map((r) => `- ${r}`).join('\n')}`);
  }

  // Key settings
  const settings = [
    `- **Calendar days/week:** ${String(m.calendarDaysPerWeek)}`,
    `- **Infrastructure:** ${labelFor(INFRA_KIND_LABELS, m.infrastructureKind)}`,
  ];
  parts.push(`## Settings\n\n${settings.join('\n')}`);

  // Declared usage
  const usage = m.declaredUsage;
  if (usage) {
    const rows = [
      `- **Daily active users:** ${String(usage.expectedDailyActiveUsers)}`,
      `- **Requests/minute:** ${String(usage.requestsPerMinute)}`,
      `- **Avg payload:** ${String(usage.avgPayloadBytes)} bytes`,
    ];
    parts.push(`## Declared Usage\n\n${rows.join('\n')}`);
  }

  // Settlement terms
  const t = m.terms;
  if (t) {
    const rows = [
      `- **Revenue share:** ${labelFor(REVENUE_SHARE_LABELS, t.revenueShare)} (${String(t.revenueSharePercent)}%)`,
      `- **Compute cost:** ${labelFor(COMPUTE_COST_LABELS, t.computeCost)} (markup ${String(t.computeMarkupPercent)}%)`,
      `- **Schedule:** ${labelFor(SCHEDULE_LABELS, t.schedule)}`,
    ];
    parts.push(`## Settlement Terms\n\n${rows.join('\n')}`);
  }

  // Notes
  if ((m.notes ?? '').length > 0) {
    parts.push(`## Notes\n\n${m.notes}`);
  }

  return parts.join('\n\n');
}

function activityListToMarkdown(m: ActivityListModel): string {
  const activities = m.activities ?? [];
  if (activities.length === 0) return '';
  const header = '| Activity | Effort (d) | Worker Class | Coding | Risk |';
  const sep = '|---|---|---|---|---|';
  const rows = activities
    .map(
      (a) =>
        `| ${a.name} | ${String(a.effortDays)} | ${a.workerClass} | ${a.coding ? 'Yes' : 'No'} | ${String(a.riskBucket)} |`
    )
    .join('\n');
  return `## Activity List\n\n${header}\n${sep}\n${rows}`;
}

function networkToMarkdown(m: NetworkModel): string {
  const parts: string[] = [];

  const cp = m.criticalPath ?? [];
  if (cp.length > 0) {
    parts.push(`## Critical Path\n\n${cp.join(' → ')}`);
  }

  const deps = m.dependencies ?? [];
  if (deps.length > 0) {
    const header = '| Activity | Depends On |';
    const sep = '|---|---|';
    const rows = deps
      .map((d) => `| ${d.activity} | ${(d.dependsOn ?? []).join(', ')} |`)
      .join('\n');
    parts.push(`## Dependencies\n\n${header}\n${sep}\n${rows}`);
  }

  return parts.join('\n\n');
}

function solutionToMarkdown(m: SolutionModel): string {
  const parts: string[] = [];

  const settings = [
    `- **Slot kind:** ${m.slotKind}`,
    `- **Staffing cap:** ${String(m.staffingCap)}`,
    `- **Calendar days/week:** ${String(m.calendarDaysPerWeek)}`,
    `- **Buffer days:** ${String(m.bufferDays)}`,
  ];
  parts.push(`## Solution Parameters\n\n${settings.join('\n')}`);

  const rates = m.classRates ?? {};
  const classes = Object.keys(rates);
  if (classes.length > 0) {
    const header = '| Worker Class | Rate/day |';
    const sep = '|---|---|';
    const rows = classes
      .map((cls) => {
        const rate = rates[cls];
        return `| ${cls} | ${rate !== undefined ? formatMoney(rate) : '—'} |`;
      })
      .join('\n');
    parts.push(`## Class Rates\n\n${header}\n${sep}\n${rows}`);
  }

  return parts.join('\n\n');
}

function riskModelToMarkdown(m: RiskModelModel): string {
  const rows = m.rows ?? [];
  if (rows.length === 0) return '';
  const header = '| Option | Criticality Risk | Activity Risk | Composite |';
  const sep = '|---|---|---|---|';
  const tableRows = rows
    .map(
      (r) =>
        `| ${r.solutionKind} | ${String(r.criticalityRisk)} | ${String(r.activityRisk)} | ${String(r.composite)} |`
    )
    .join('\n');
  return `## Risk Model\n\n${header}\n${sep}\n${tableRows}`;
}

function sdpReviewToMarkdown(m: SdpReviewModel): string {
  const parts: string[] = [];

  if ((m.recommendation ?? '').length > 0) {
    parts.push(`## Recommendation\n\n**${m.recommendation}**`);
  }

  if ((m.rationale ?? '').length > 0) {
    parts.push(`## Rationale\n\n${m.rationale}`);
  }

  const options = m.options ?? [];
  if (options.length > 0) {
    const header =
      '| Option | Solution | Duration (d) | Build Cost | Composite Risk | Monthly Cost | Per-Cycle Net | Rev Share % |';
    const sep = '|---|---|---|---|---|---|---|---|';
    const rows = options
      .map(
        (o) =>
          `| ${o.optionId} | ${o.solutionKind} | ${String(o.durationDays)} | ${formatMoney(o.buildCost)} | ${String(o.compositeRisk)} | ${formatMoney(o.projectedMonthlyCost)} | ${formatMoney(o.expectedPerCycleNet)} | ${String(o.revenueSharePercent)}% |`
      )
      .join('\n');
    parts.push(`## Options\n\n${header}\n${sep}\n${rows}`);
  }

  return parts.join('\n\n');
}

// ---------------------------------------------------------------------------
// Internals.
// ---------------------------------------------------------------------------

interface ModelForKind {
  mission: Schemas['MissionStatement'];
  glossary: Schemas['Glossary'];
  scrubbedRequirements: Schemas['ScrubbedRequirements'];
  volatilities: Schemas['Volatilities'];
  coreUseCases: Schemas['CoreUseCases'];
  system: Schemas['System'];
  operationalConcepts: Schemas['OperationalConcepts'];
  standardCheck: Schemas['StandardCheck'];
}

/**
 * Narrows an envelope to the concrete typed model for the expected `kind`.
 * Returns undefined when the envelope is absent, of a different kind, or carries
 * no staged model — so callers can fall back to a safe empty view.
 */
function narrow<K extends keyof ModelForKind>(
  envelope: ArtifactModelEnvelope | undefined,
  kind: K
): ModelForKind[K] | undefined {
  if (envelope?.kind !== kind || envelope.model === undefined) {
    return undefined;
  }
  return envelope.model as ModelForKind[K];
}
