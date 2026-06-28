/**
 * App-level decode types for the typed artifact MODEL payloads.
 *
 * The generated OpenAPI schema deliberately leaves every artifact model opaque:
 * the session/slot envelopes type `model` as `null` (Phase-1 and Phase-2 alike).
 * The Go wire form IS fully typed (projectstate.* models, byte-stable camelCase
 * `json` tags), but the OAS does not enumerate it — so the SPA hand-mirrors the
 * model shapes here and decodes the opaque `model` payload through `unknown`.
 *
 * These are NOT a duplicate of anything in schema.ts (schema.ts has no model
 * types) — they are the app's decode contract for the opaque payload, the same
 * justification the per-manager Go codecs carry. Field names match the Go
 * `json:"…"` tags EXACTLY.
 */
import type { ArtifactKind, ArtifactKindFull, ProjectArtifactKind } from './types';

// ---------------------------------------------------------------------------
// Phase-1 (System Design) models.
// ---------------------------------------------------------------------------

export interface Objective {
  number: number;
  statement: string;
}

/** projectstate.MissionStatement (ch. 5 business alignment). */
export interface MissionStatement {
  vision: string;
  objectives?: Objective[] | null;
  mission: string;
}

export interface GlossaryItem {
  term: string;
  definition: string;
  category?: string;
}

/** projectstate.Glossary (ch. 3 ubiquitous language). */
export interface Glossary {
  items: GlossaryItem[] | null;
}

export interface Requirement {
  id: string;
  statement: string;
}

/** projectstate.ScrubbedRequirements (OQ-2). */
export interface ScrubbedRequirements {
  items: Requirement[] | null;
}

/** projectstate.Axis (the two volatility axes). */
export type Axis = 'sameCustomerOverTime' | 'allCustomersAtOneTime';

export interface Volatility {
  name: string;
  rationale: string;
  axis: Axis;
}

/** projectstate.Volatilities (ch. 2, the two axes). */
export interface Volatilities {
  items: Volatility[] | null;
}

/** projectstate.ActivityNodeKind. */
export type ActivityNodeKind =
  | 'start'
  | 'action'
  | 'decision'
  | 'merge'
  | 'fork'
  | 'join'
  | 'end'
  | 'swimLane'
  | 'note'
  | 'loop'
  | 'switch'
  | 'goto'
  | 'interruptEdge';

export interface ActivityNode {
  id: string;
  kind: ActivityNodeKind;
  label: string;
  roleName: string;
  linkedActorId?: string | null;
  linkedCompId?: string | null;
}

/** projectstate.EdgeKind (activity-edge kind). */
export type EdgeKind = 'controlFlow' | 'guardedFlow';

export interface ActivityEdge {
  from: string;
  to: string;
  kind: EdgeKind;
  guard: string;
}

export interface ActivityDiagram {
  nodes: ActivityNode[] | null;
  edges: ActivityEdge[] | null;
}

export interface Actor {
  id: string;
  role: string;
}

/** projectstate.Trigger (use-case trigger kind). */
export type Trigger = 'clientAction' | 'timer' | 'busMessage';

/** projectstate.Classification (Core vs NonCore). */
export type Classification = 'core' | 'nonCore';

/** projectstate.UseCase (Grammar B, ch. 4). */
export interface UseCase {
  id: string;
  name: string;
  actors?: Actor[] | null;
  trigger: Trigger;
  classification: Classification;
  variationOf?: string | null;
  activity?: ActivityDiagram | null;
}

export interface UseCaseDecision {
  useCase: UseCase;
  rejectionReason: string;
}

/** projectstate.CoreUseCases (ch. 4 core-use-case selection). */
export interface CoreUseCases {
  decisions: UseCaseDecision[] | null;
}

/** projectstate.ComponentKind (component taxonomy). */
export type ComponentKind =
  | 'client'
  | 'manager'
  | 'engine'
  | 'resourceAccess'
  | 'resource'
  | 'utility';

/** projectstate.Layer (layer set). */
export type Layer =
  | 'client'
  | 'manager'
  | 'engine'
  | 'resourceAccess'
  | 'resource'
  | 'utility';

export interface Component {
  id: string;
  name: string;
  kind: ComponentKind;
  layer: Layer;
  encapsulates: string;
  atomicBusinessVerbs?: string[] | null;
}

/** projectstate.CallMode (edge-mode set). */
export type CallMode = 'sync' | 'queued' | 'eventPubSub';

export interface Relationship {
  from: string;
  to: string;
  mode: CallMode;
  label: string;
}

/** projectstate.DynamicView (one call chain per use case). */
export interface DynamicView {
  useCaseId: string;
  key: string;
  title: string;
  participants?: string[] | null;
  edges?: Relationship[] | null;
}

/** projectstate.System (Grammar A, ch. 3/4 static architecture). */
export interface System {
  components: Component[] | null;
  relationships: Relationship[] | null;
  dynamicViews: DynamicView[] | null;
}

/** projectstate.DeliveryStyle. */
export type DeliveryStyle = 'cloud' | 'local' | 'both';

/** projectstate.DeploymentProfile. */
export type DeploymentProfile = 'cloud' | 'local' | 'test';

export interface ContainerInstance {
  componentId: string;
  note: string;
}

export interface DeploymentNode {
  name: string;
  technology: string;
  children: DeploymentNode[] | null;
  instances: ContainerInstance[] | null;
}

export interface DeploymentEnvironment {
  profile: DeploymentProfile;
  title: string;
  nodes: DeploymentNode[] | null;
}

export interface DeploymentTopology {
  deliveryStyle: DeliveryStyle;
  environments: DeploymentEnvironment[] | null;
}

export interface OperationalDecision {
  topic: string;
  decision: string;
  justifyingObjective: number;
}

/** projectstate.OperationalConcepts (ch. 5). */
export interface OperationalConcepts {
  decisions: OperationalDecision[] | null;
  deployment?: DeploymentTopology;
}

/** projectstate.CheckStatus (App C design-standard item outcome). */
export type CheckStatus = 'pass' | 'waived' | 'fail';

export interface CheckItem {
  section: string;
  guideline: string;
  status: CheckStatus;
  justification: string;
}

/** projectstate.StandardCheck (App C design-standard walk). */
export interface StandardCheck {
  items: CheckItem[] | null;
}

// ---------------------------------------------------------------------------
// Phase-2 (Project Design) models.
// ---------------------------------------------------------------------------

/** projectstate.Money — exact integer minor units + ISO-4217 currency. */
export interface Money {
  /** Signed minor units, e.g. 1299 == 12.99. */
  minorUnits: number;
  /** ISO-4217, e.g. "USD". */
  currency: string;
}

/** projectstate.UsageAssumption — the customer's declared load. */
export interface UsageAssumption {
  expectedDailyActiveUsers: number;
  requestsPerMinute: number;
  avgPayloadBytes: number;
}

/** projectstate.SettlementTerms — integer enum ordinals + percent figures. */
export interface SettlementTerms {
  revenueShare: number;
  revenueSharePercent: number;
  computeCost: number;
  computeMarkupPercent: number;
  schedule: number;
}

/** projectstate.PlanningAssumptions — the Phase-2 planning-assumptions artifact. */
export interface PlanningAssumptionsModel {
  resources: string[];
  calendarDaysPerWeek: number;
  /** Integer enum ordinal (InfrastructureKind). */
  infrastructureKind: number;
  declaredUsage: UsageAssumption;
  terms: SettlementTerms;
  notes: string;
}

/** projectstate.ActivityItem — one activity in the activity list. */
export interface ActivityItem {
  name: string;
  effortDays: number;
  workerClass: string;
  coding: boolean;
  riskBucket: number;
  title?: string;
}

/** projectstate.ActivityList — the Phase-2 activity-list artifact. */
export interface ActivityListModel {
  activities: ActivityItem[];
}

/** projectstate.NetworkDependency — one activity's predecessor set. */
export interface NetworkDependency {
  activity: string;
  dependsOn: string[];
}

/** Float-criticality band (Löwy ch.8 §2) — server-computed. */
export type FloatBand = 'critical' | 'red' | 'yellow' | 'green';

/** projectstate.NetworkNodeCompute — the per-activity CPM result (compute-at-read). */
export interface NetworkNodeCompute {
  earliestStart: number;
  earliestFinish: number;
  latestStart: number;
  latestFinish: number;
  totalFloat: number;
  freeFloat: number;
  onCriticalPath: boolean;
  nearCritical: boolean;
  band: FloatBand;
  column: number;
}

/** projectstate.NetworkSummary — the project-level CPM roll-up (server-computed). */
export interface NetworkSummary {
  totalDurationDays: number;
  criticalPathActivityCount: number;
  criticalPathDays: number;
  maxFloat: number;
  nearCriticalCount: number;
}

/** projectstate.NetworkMilestone — one authored zero-duration event node. */
export interface NetworkMilestone {
  id: string;
  name: string;
  public: boolean;
  dependsOn?: string[];
  onCriticalPath?: boolean;
  eventTime?: number;
}

/** projectstate.Network — the Phase-2 project-network artifact. */
export interface NetworkModel {
  dependencies: NetworkDependency[];
  criticalPath: string[];
  milestones?: NetworkMilestone[];
  computed?: Record<string, NetworkNodeCompute>;
  summary?: NetworkSummary;
}

/** projectstate.Solution — one of the four solution-option artifacts. */
export interface SolutionModel {
  slotKind: ProjectArtifactKind;
  staffingCap: number;
  calendarDaysPerWeek: number;
  classRates: Record<string, Money>;
  bufferDays: number;
}

/** projectstate.RiskRow — per-option risk decomposition. */
export interface RiskRow {
  solutionKind: ProjectArtifactKind;
  criticalityRisk: number;
  activityRisk: number;
  composite: number;
}

/** projectstate.RiskModel — the Phase-2 risk-model artifact. */
export interface RiskModelModel {
  rows: RiskRow[];
}

/** projectstate.SdpOptionRow — one joined row of the SDP-review options table. */
export interface SdpOptionRow {
  optionId: string;
  solutionKind: ProjectArtifactKind;
  durationDays: number;
  buildCost: Money;
  compositeRisk: number;
  projectedMonthlyCost: Money;
  expectedPerCycleNet: Money;
  revenueSharePercent: number;
}

/** projectstate.SdpReview — the assembled SDP-review artifact. */
export interface SdpReviewModel {
  options: SdpOptionRow[];
  recommendation: string;
  rationale: string;
}

// ---------------------------------------------------------------------------
// Discriminated model envelopes — the decoded form of the opaque {kind, model}.
// ---------------------------------------------------------------------------

/** The decoded Phase-1+2 model envelope (the SPA narrows on the string `kind`). */
export interface ArtifactModelEnvelope {
  kind: ArtifactKindFull;
  model?:
    | MissionStatement
    | Glossary
    | ScrubbedRequirements
    | Volatilities
    | CoreUseCases
    | System
    | OperationalConcepts
    | StandardCheck
    | PlanningAssumptionsModel
    | ActivityListModel
    | NetworkModel
    | SolutionModel
    | RiskModelModel
    | SdpReviewModel;
}

/** The decoded Phase-2 model envelope. */
export interface ProjectArtifactModelEnvelope {
  kind: ProjectArtifactKind;
  model?:
    | PlanningAssumptionsModel
    | ActivityListModel
    | NetworkModel
    | SolutionModel
    | RiskModelModel
    | SdpReviewModel;
}

/** Re-export so prose adapters can refer to the Phase-1 ArtifactKind discriminator. */
export type { ArtifactKind };
