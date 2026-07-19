/**
 * Pure adapters mapping the typed server head-state into render-ready view models
 * the screens / diagram renderers consume. Every function is total and resilient
 * to an absent `model` (locked / empty slots) — it returns a safe empty view
 * rather than throwing, so the UI can render a placeholder. No React here.
 *
 * The discriminator everywhere is the string `kind` on ArtifactModelEnvelope /
 * ArtifactSlotView; we narrow on it before reading the concrete typed model.
 */
import type {
  ArtifactKindFull,
  ArtifactModelEnvelope,
  ArtifactSlotView,
  ArtifactStageOrdinal,
  ProjectPhase,
  ProjectState,
  PlanningAssumptionsModel,
  ActivityListModel,
  NetworkModel,
  SolutionModel,
  RiskModelModel,
  SdpReviewModel,
  Money,
  ActivityNodeKind,
  Axis,
  CallMode,
  CheckItem,
  Classification,
  ComponentKind,
  ContainerInstance,
  CoreUseCases,
  DeployContainer,
  DeploymentNode,
  DeploymentProfile,
  EdgeKind,
  Glossary,
  GlossaryItem,
  Layer,
  MissionStatement,
  OperationalConcepts,
  OperationalDecision,
  RejectedVolatility,
  Requirement,
  ScrubbedRequirements,
  StandardCheck,
  System,
  UseCaseDecision,
  Volatilities,
} from './types';
import { METHOD_METADATA, PHASE1_ORDER, PHASE2_ORDER } from './methodMetadata';
import { ARTIFACT_STAGE_APP_STRINGS } from './enums.gen';
import { dynamicViewLabel, indexUseCaseNames } from './dynamicViewLabels';
import { assertNever } from './exhaustive';

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
const PHASE_ORDINAL: Record<ProjectPhase, number> = {
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
  /** Commit count; > 1 once the slot has been amended. */
  revisions?: number;
  /** True when an upstream basis changed since this slot was committed. */
  staleBasis?: boolean;
}

/**
 * Maps the ArtifactStage ordinal (0..4) to a display stage, sourced from the
 * generated enums.gen.ts — mirrors how the other …FromOrdinal reads in wire.ts
 * work, so a Go ArtifactStage member add/remove/reorder breaks tsc here instead
 * of drifting silently (the hand switch this replaced had no such guard).
 * SlotStage is structurally identical to the generated ArtifactStage union.
 * Reads ARTIFACT_STAGE_APP_STRINGS (the `as const` tuple ARTIFACT_STAGE_ORDINAL_TO_APP
 * is itself built from) rather than that wider `readonly ArtifactStage[]` typing:
 * indexing a literal-length tuple with the literal ArtifactStageOrdinal domain
 * type-checks to a definite ArtifactStage under noUncheckedIndexedAccess, so no
 * cast/non-null-assertion is needed — same generated data either way.
 */
export function slotStageFromOrdinal(ordinal: ArtifactStageOrdinal): SlotStage {
  return ARTIFACT_STAGE_APP_STRINGS[ordinal];
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
    ...(slot.revisions !== undefined ? { revisions: slot.revisions } : {}),
    ...(slot.staleBasis === true ? { staleBasis: true } : {}),
  };
}

// ---------------------------------------------------------------------------
// Volatilities → per-axis points + rejected candidates.
// ---------------------------------------------------------------------------

/**
 * One volatility on its Löwy axis. Deliberately carries NO 2D coordinates: the
 * typed model is categorical (one axis per volatility), and the old fabricated
 * x/y scatter placement collapsed everything onto a diagonal (see VolatilityMap).
 * Rendering positions (lane order, axes-diagram spacing) are derived honestly
 * from the point's per-axis order by volatilityMapLogic.axesLayout.
 */
export interface VolatilityPoint {
  name: string;
  rationale: string;
  axis: Axis;
  /** Scrubbed-requirement ids (SR-…) this volatility traces to, when recorded. */
  traces?: string[];
}

export interface VolatilityView {
  points: VolatilityPoint[];
  /** Candidates the architect explicitly rejected, with the classified reason.
   *  Empty for older artifacts drafted before the model carried them. */
  rejected: RejectedVolatility[];
}

const EMPTY_VOLATILITY_VIEW: VolatilityView = { points: [], rejected: [] };

/**
 * Maps the typed Volatilities model into the view: accepted items in model order
 * (the index in `points` is the stable `$.items[n]` comment anchor) plus the
 * rejected-candidate list (`$.rejected[n]`).
 */
export function toVolatilityView(envelope: ArtifactModelEnvelope | undefined): VolatilityView {
  const model = narrow(envelope, 'volatilities');
  if (model === undefined) return EMPTY_VOLATILITY_VIEW;
  const items = model.items ?? [];

  const points = items.map(
    (item): VolatilityPoint => ({
      name: item.name,
      rationale: item.rationale,
      axis: item.axis,
      ...(item.traces != null && item.traces.length > 0 ? { traces: item.traces } : {}),
    })
  );

  return { points, rejected: model.rejected ?? [] };
}

export const AXIS1_LABEL = 'Axis 1 — same customer, over time';
export const AXIS2_LABEL = 'Axis 2 — all customers, one moment';

// ---------------------------------------------------------------------------
// System → C4 component view.
// ---------------------------------------------------------------------------

export interface C4Component {
  id: string;
  name: string;
  kind: ComponentKind;
  layer: Layer;
  /** The volatility this component encapsulates (empty for Resource / Utility). */
  encapsulates: string;
  /**
   * The camelCase serviceContracts key this component is the architecture home of
   * (e.g. "systemDesignManager"). Empty when the component owns no contract
   * (utilities, resources) or the document predates the field (heuristic fallback).
   */
  contractKey: string;
}

export interface C4Relationship {
  from: string;
  to: string;
  mode: CallMode;
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
      contractKey: c.contractKey ?? '',
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

/**
 * Lists the System's dynamic views (key + display label) for a picker. Empty when
 * absent. Pass the committed coreUseCases envelope so a view with a blank title can
 * fall back to its linked use case's name (see dynamicViewLabel) — options (and the
 * Select's selected-value rendering, which reuses them) are never blank.
 */
export function listDynamicViews(
  envelope: ArtifactModelEnvelope | undefined,
  useCasesEnvelope?: ArtifactModelEnvelope
): DynamicViewRef[] {
  const model = narrow(envelope, 'system');
  if (model === undefined) return [];
  const nameById = indexUseCaseNames(narrow(useCasesEnvelope, 'coreUseCases'));
  return (model.dynamicViews ?? []).map((v, i) => ({
    key: v.key,
    title: dynamicViewLabel(v, i, nameById),
  }));
}

/**
 * Returns the subset of the System's dynamic views whose participant list includes
 * the given componentId (kebab-case, e.g. "web-client"). Empty when absent or when
 * no view includes the component. Labels carry the same blank-title fallback chain
 * as listDynamicViews (positional placeholders keep the view's ORIGINAL index).
 */
export function listDynamicViewsForComponent(
  envelope: ArtifactModelEnvelope | undefined,
  componentId: string,
  useCasesEnvelope?: ArtifactModelEnvelope
): DynamicViewRef[] {
  const model = narrow(envelope, 'system');
  if (model === undefined || componentId.length === 0) return [];
  const nameById = indexUseCaseNames(narrow(useCasesEnvelope, 'coreUseCases'));
  return (model.dynamicViews ?? [])
    .map((v, i) => ({ v, i }))
    .filter(({ v }) => (v.participants ?? []).includes(componentId))
    .map(({ v, i }) => ({ key: v.key, title: dynamicViewLabel(v, i, nameById) }));
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
      contractKey: c.contractKey ?? '',
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

/** A packaged System component reference (name + layer), for the hover/expand list. */
export interface ComponentRef {
  name: string;
  layer: Layer;
}

/** A container instance placed in a deployment node, resolved to its packaged components. */
export interface ContainerInstanceView {
  key: string;
  name: string;
  technology: string;
  description: string;
  note: string;
  components: ComponentRef[]; // packaged System components (resolved), for the hover/expand list
}

export interface InfraView {
  name: string;
  technology: string;
  description: string;
}

export interface ExternalView {
  name: string;
  technology: string;
  description: string;
}

/** A nested deployment node: child nodes + the container instances / infra / externals it hosts. */
export interface DeploymentNodeView {
  name: string;
  technology: string;
  description: string;
  instances: number;
  children: DeploymentNodeView[];
  containers: ContainerInstanceView[];
  infrastructure: InfraView[];
  externals: ExternalView[];
}

/** A pickable deployment-environment reference: its profile + display title. */
export interface DeploymentProfileRef {
  profile: DeploymentProfile;
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
  return (op?.deployment.environments ?? []).map((e) => ({ profile: e.profile, title: e.title }));
}

/**
 * Builds the nested deployment topology for one profile, resolving each
 * ContainerInstance to its DeployContainer definition and packaged System
 * components (name + layer, for colouring), plus infrastructure and external
 * software-system nodes. Absent deployment / missing profile environment →
 * undefined, so the caller renders nothing.
 */
export function toDeploymentView(
  opEnvelope: ArtifactModelEnvelope | undefined,
  systemEnvelope: ArtifactModelEnvelope | undefined,
  profile: DeploymentProfile
): DeploymentNodeView[] | undefined {
  const op = narrow(opEnvelope, 'operationalConcepts');
  const topo = op?.deployment;
  const env = (topo?.environments ?? []).find((e) => e.profile === profile);
  if (env === undefined) return undefined;

  const system = narrow(systemEnvelope, 'system');
  const byName = new Map<string, Layer>();
  for (const c of system?.components ?? []) byName.set(c.name, c.layer);

  const containersByKey = new Map<string, DeployContainer>();
  for (const c of topo?.containers ?? []) containersByKey.set(c.key, c);

  const resolveContainer = (ci: ContainerInstance): ContainerInstanceView => {
    const c = containersByKey.get(ci.containerKey);
    return {
      key: ci.containerKey,
      name: c?.name ?? ci.containerKey,
      technology: c?.technology ?? '',
      description: c?.description ?? '',
      note: ci.note,
      components: (c?.components ?? []).map((n) => ({
        name: n,
        layer: byName.get(n) ?? 'utility',
      })),
    };
  };

  const mapNode = (node: DeploymentNode): DeploymentNodeView => ({
    name: node.name,
    technology: node.technology,
    description: node.description,
    instances: node.instances > 0 ? node.instances : 1,
    children: (node.children ?? []).map(mapNode),
    containers: (node.containerInstances ?? []).map(resolveContainer),
    infrastructure: (node.infrastructureNodes ?? []).map((n) => ({
      name: n.name,
      technology: n.technology,
      description: n.description,
    })),
    externals: (node.softwareSystemInstances ?? []).map((n) => ({
      name: n.name,
      technology: n.technology,
      description: n.description,
    })),
  });

  return (env.nodes ?? []).map(mapNode);
}

// ---------------------------------------------------------------------------
// Core use cases → activity views (lanes / nodes / edges).
// ---------------------------------------------------------------------------

export interface ActivityNodeView {
  id: string;
  kind: ActivityNodeKind;
  label: string;
  /** The swim-lane (role) this node sits in. */
  lane: string;
}

export interface ActivityEdgeView {
  from: string;
  to: string;
  kind: EdgeKind;
  /** Guard text on a guardedFlow edge (empty otherwise). */
  guard: string;
}

export interface UseCaseView {
  id: string;
  name: string;
  classification: Classification;
  rejectionReason: string;
  /** The id of the use case this one is a variation of (shares its activity
   *  diagram), or empty when this use case owns its own diagram. */
  variationOf: string;
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

function toUseCaseView(decision: UseCaseDecision): UseCaseView {
  const uc = decision.useCase;
  const activity = uc.activity;
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
    variationOf: uc.variationOf ?? '',
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
  switch (kind) {
    case 'mission':
      return missionToMarkdown(model as MissionStatement);
    case 'glossary':
      return glossaryToMarkdown(model as Glossary);
    case 'scrubbedRequirements':
      return scrubbedRequirementsToMarkdown(model as ScrubbedRequirements);
    case 'operationalConcepts':
      return operationalConceptsToMarkdown(model as OperationalConcepts);
    case 'standardCheck':
      return standardCheckToMarkdown(model as StandardCheck);
    // Phase-2 kinds — the home base passes these through ArtifactModelEnvelope
    // (ArtifactKindFull includes both phases); the model is the hand-mirrored type.
    case 'planningAssumptions':
      return planningAssumptionsToMarkdown(model as unknown as PlanningAssumptionsModel);
    case 'activityList':
      return activityListToMarkdown(model as unknown as ActivityListModel);
    case 'network':
      return networkToMarkdown(model as unknown as NetworkModel);
    case 'normalSolution':
    case 'decompressedSolution':
    case 'subcriticalSolution':
    case 'compressedSolution':
      return solutionToMarkdown(model as unknown as SolutionModel);
    case 'riskModel':
      return riskModelToMarkdown(model as unknown as RiskModelModel);
    case 'sdpReview':
      return sdpReviewToMarkdown(model as unknown as SdpReviewModel);
    // Non-prose kinds (volatilities / coreUseCases / system) have dedicated diagram
    // adapters; they carry no markdown projection.
    case 'volatilities':
    case 'coreUseCases':
    case 'system':
      return '';
    default:
      return assertNever(kind);
  }
}

function missionToMarkdown(m: MissionStatement): string {
  const objectives = (m.objectives ?? [])
    .map((o) => `${String(o.number)}. ${o.statement}`)
    .join('\n');
  const parts = [`## Vision\n\n${m.vision}`];
  if (objectives.length > 0) parts.push(`## Business Objectives\n\n${objectives}`);
  parts.push(`## Mission Statement\n\n${m.mission}`);
  return parts.join('\n\n');
}

function glossaryToMarkdown(g: Glossary): string {
  const items = g.items ?? [];
  if (items.length === 0) return '';
  const rows = items
    .map((i) => {
      const hasCategory = i.category.length > 0;
      const category = hasCategory ? ` _(${i.category})_` : '';
      return `- **${i.term}**${category} — ${i.definition}`;
    })
    .join('\n');
  return `## Glossary\n\n${rows}`;
}

/**
 * Typed glossary items for the dedicated GlossaryView (search + category filter +
 * grouped sticky subheaders) — the glossary is a ~40-term reference, not prose, so
 * it gets a real renderer instead of the flat markdown fallback. Safe-empty.
 */
export function toGlossaryView(envelope: ArtifactModelEnvelope | undefined): GlossaryItem[] {
  const model = narrow(envelope, 'glossary');
  return model?.items ?? [];
}

/** The typed MissionStatement (vision / objectives / mission), safe-empty. */
export function toMissionView(envelope: ArtifactModelEnvelope | undefined): MissionStatement {
  const model = narrow(envelope, 'mission');
  return {
    vision: model?.vision ?? '',
    objectives: model?.objectives ?? [],
    mission: model?.mission ?? '',
  };
}

/** The typed scrubbed requirements (id + statement), safe-empty. */
export function toScrubbedRequirementsView(
  envelope: ArtifactModelEnvelope | undefined
): Requirement[] {
  const model = narrow(envelope, 'scrubbedRequirements');
  return model?.items ?? [];
}

/** The typed operational-concept decisions, safe-empty. */
export function toOperationalDecisionsView(
  envelope: ArtifactModelEnvelope | undefined
): OperationalDecision[] {
  const model = narrow(envelope, 'operationalConcepts');
  return model?.decisions ?? [];
}

/** The typed standard-check rows, safe-empty. */
export function toStandardCheckView(envelope: ArtifactModelEnvelope | undefined): CheckItem[] {
  const model = narrow(envelope, 'standardCheck');
  return model?.items ?? [];
}

function scrubbedRequirementsToMarkdown(r: ScrubbedRequirements): string {
  const items = r.items ?? [];
  if (items.length === 0) return '';
  const rows = items.map((i) => `- **${i.id}** — ${i.statement}`).join('\n');
  return `## Scrubbed Requirements\n\n${rows}`;
}

function operationalConceptsToMarkdown(o: OperationalConcepts): string {
  const decisions = o.decisions ?? [];
  if (decisions.length === 0) return '';
  const rows = decisions
    .map((d) => `- **${d.topic}** — ${d.decision} _(objective ${String(d.justifyingObjective)})_`)
    .join('\n');
  return `## Operational Concepts\n\n${rows}`;
}

function standardCheckToMarkdown(s: StandardCheck): string {
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

  // Resources (generated slice is nullable — nil→null on the wire)
  const resources = m.resources ?? [];
  if (resources.length > 0) {
    parts.push(`## Resources\n\n${resources.map((r) => `- ${r}`).join('\n')}`);
  }

  // Key settings
  const settings = [
    `- **Calendar days/week:** ${String(m.calendarDaysPerWeek)}`,
    `- **Infrastructure:** ${labelFor(INFRA_KIND_LABELS, m.infrastructureKind)}`,
  ];
  parts.push(`## Settings\n\n${settings.join('\n')}`);

  // Declared usage
  const usage = m.declaredUsage;
  const usageRows = [
    `- **Daily active users:** ${String(usage.expectedDailyActiveUsers)}`,
    `- **Requests/minute:** ${String(usage.requestsPerMinute)}`,
    `- **Avg payload:** ${String(usage.avgPayloadBytes)} bytes`,
  ];
  parts.push(`## Declared Usage\n\n${usageRows.join('\n')}`);

  // Settlement terms
  const t = m.terms;
  const termsRows = [
    `- **Revenue share:** ${labelFor(REVENUE_SHARE_LABELS, t.revenueShare)} (${String(t.revenueSharePercent)}%)`,
    `- **Compute cost:** ${labelFor(COMPUTE_COST_LABELS, t.computeCost)} (markup ${String(t.computeMarkupPercent)}%)`,
    `- **Schedule:** ${labelFor(SCHEDULE_LABELS, t.schedule)}`,
  ];
  parts.push(`## Settlement Terms\n\n${termsRows.join('\n')}`);

  // Notes
  if (m.notes.length > 0) {
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

  const rates = m.classRates;
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

  if (m.recommendation.length > 0) {
    parts.push(`## Recommendation\n\n**${m.recommendation}**`);
  }

  if (m.rationale.length > 0) {
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
  mission: MissionStatement;
  glossary: Glossary;
  scrubbedRequirements: ScrubbedRequirements;
  volatilities: Volatilities;
  coreUseCases: CoreUseCases;
  system: System;
  operationalConcepts: OperationalConcepts;
  standardCheck: StandardCheck;
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
