/**
 * Hand-curated, app-facing types derived from the generated OpenAPI schema.
 *
 * The generated `schema.ts` is the source of truth for wire shapes; this module
 * re-exports the few aliases the UI code references so screens/hooks never import
 * the verbose generated paths directly. Keep this thin — no logic.
 */
import type { components } from './schema';

export type ArtifactKind = components['schemas']['ArtifactKind'];
export type ArtifactKindFull = components['schemas']['ArtifactKindFull'];
export type ReviewDecision = components['schemas']['ReviewDecision'];
export type SessionStage = components['schemas']['SessionStage'];

export type AnchoredComment = components['schemas']['AnchoredComment'];
export type ResearchInput = components['schemas']['ResearchInput'];
export type ResearchSource = components['schemas']['ResearchSource'];

export type ProjectSummary = components['schemas']['ProjectSummary'];
export type ProjectState = components['schemas']['ProjectState'];
export type ProjectPhase = components['schemas']['ProjectPhase'];
export type ArtifactSlotView = components['schemas']['ArtifactSlotView'];
export type ArtifactStageOrdinal = components['schemas']['ArtifactStageOrdinal'];
export type ArtifactModelEnvelope = components['schemas']['ArtifactModelEnvelope'];

export type SessionStateResponse = components['schemas']['SessionStateResponse'];
export type SessionStateView = components['schemas']['SessionStateView'];
export type Finding = components['schemas']['Finding'];
export type PhaseAdvanceResponse = components['schemas']['PhaseAdvanceResponse'];
export type SessionRefResponse = components['schemas']['SessionRefResponse'];
export type ErrorResponse = components['schemas']['ErrorResponse'];

/**
 * The seven (eight slots incl. standardCheck) Phase-1 Method artifacts, in the
 * order the server exposes them (openapi.yaml ArtifactKind enum). Drives the
 * ordered co-author gate progression in the UI.
 */
export const PHASE1_ARTIFACTS: readonly ArtifactKind[] = [
  'mission',
  'glossary',
  'scrubbedRequirements',
  'volatilities',
  'coreUseCases',
  'system',
  'operationalConcepts',
  'standardCheck',
] as const;

/** Human-readable labels for each artifact kind. */
export const ARTIFACT_LABELS: Record<ArtifactKind, string> = {
  mission: 'Mission',
  glossary: 'Glossary',
  scrubbedRequirements: 'Scrubbed Requirements',
  volatilities: 'Volatilities',
  coreUseCases: 'Core Use Cases',
  system: 'System (Architecture)',
  operationalConcepts: 'Operational Concepts',
  standardCheck: 'Standard Check',
};

/** Stages at which the human review gate is open. */
export const REVIEWABLE_STAGE: SessionStage = 'awaitingReview';

/**
 * Terminal stages — no further action possible for that artifact session without an
 * explicit human exit (Retry / Withdraw). draftFailed is terminal-at-the-Manager
 * (the async design job failed in the user's CI): polling stops and the SPA renders
 * the DraftFailedPanel rather than a perpetual generating spinner (anti-wedge).
 */
export const TERMINAL_STAGES: readonly SessionStage[] = [
  'committed',
  'withdrawn',
  'refused',
  'draftFailed',
];

// ---------------------------------------------------------------------------
// UC2 — Phase-2 Project Design wire types (mirrors the Phase-1 block above).
// ---------------------------------------------------------------------------

export type ProjectArtifactKind = components['schemas']['ProjectArtifactKind'];
export type ProjectSessionStage = components['schemas']['ProjectSessionStage'];
export type SDPDecision = components['schemas']['SDPDecision'];

export type ProjectSessionStateResponse = components['schemas']['ProjectSessionStateResponse'];
export type ProjectPhaseAdvanceResponse = components['schemas']['ProjectPhaseAdvanceResponse'];

/**
 * The eight DRAFTABLE Phase-2 Method artifacts, in the order the server exposes
 * them (openapi.yaml ProjectArtifactKind enum, minus sdpReview). Drives the
 * ordered co-author gate progression in the Phase-2 workspace. sdpReview is
 * assembled (not drafted) and handled separately by SdpReviewPanel.
 */
export const PHASE2_DRAFTABLE_ARTIFACTS: readonly ProjectArtifactKind[] = [
  'planningAssumptions',
  'activityList',
  'network',
  'normalSolution',
  'decompressedSolution',
  'subcriticalSolution',
  'compressedSolution',
  'riskModel',
] as const;

/** The assembled (read-only via the sessions endpoint) SDP-review slot. */
export const SDP_REVIEW_KIND: ProjectArtifactKind = 'sdpReview';

/** Human-readable labels for each Phase-2 artifact kind. */
export const PROJECT_ARTIFACT_LABELS: Record<ProjectArtifactKind, string> = {
  planningAssumptions: 'Planning Assumptions',
  activityList: 'Activity List',
  network: 'Network',
  normalSolution: 'Normal Solution',
  decompressedSolution: 'Decompressed Solution',
  subcriticalSolution: 'Subcritical Solution',
  compressedSolution: 'Compressed Solution',
  riskModel: 'Risk Model',
  sdpReview: 'SDP Review',
};

/** Phase-2 stage at which the human per-artifact review gate is open. */
export const PROJECT_REVIEWABLE_STAGE: ProjectSessionStage = 'awaitingReview';

/** Phase-2 terminal stages — no further action possible for that session without an
 * explicit human exit. draftFailed mirrors the Phase-1 anti-wedge terminal stage. */
export const PROJECT_TERMINAL_STAGES: readonly ProjectSessionStage[] = [
  'committed',
  'withdrawn',
  'refused',
  'draftFailed',
];

// ---------------------------------------------------------------------------
// UC2 — Phase-2 typed CANDIDATE models.
//
// The generated OpenAPI schema deliberately leaves the Phase-2 model payloads
// opaque: `ProjectSessionStateResponse.view` is `Record<string, never>` and the
// `ArtifactModelEnvelope.model` oneOf enumerates only Phase-1 model schemas
// ("Phase-2 model schemas are TODO Phase-2"). The Go wire form is fully typed,
// though (projectstate/models_phase2.go + estimation.go, every `kind` field is a
// camelCase string discriminator). So we hand-mirror the Phase-2 model JSON here
// — field names match the Go `json:"…"` tags EXACTLY — and decode the opaque view
// into ProjectSessionStateView via api/projectDesign.getProjectSessionState.
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

/**
 * projectstate.SettlementTerms — the settlement-terms snapshot. RevenueShare /
 * computeCost / schedule are integer enum ordinals on the wire (the Go enum types
 * carry no string codec); only the percent figures are read by the UI.
 */
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
  /** Fibonacci risk bucket: 1,2,3,5,8,13. */
  riskBucket: number;
  /** Human-readable display title (e.g. "Build Billing Gateway Access"). Optional — absent in older data. */
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

/** Float-criticality band (Löwy ch.8 §2) — server-computed, wire-stable strings. */
export type FloatBand = 'critical' | 'red' | 'yellow' | 'green';

/**
 * projectstate.NetworkNodeCompute — the per-activity CPM result the server computes at
 * read time (compute-at-read; the math formerly run client-side in toNetworkView, now
 * server-authoritative). Keyed by activity id in NetworkModel.computed.
 */
export interface NetworkNodeCompute {
  earliestStart: number;
  earliestFinish: number;
  latestStart: number;
  latestFinish: number;
  totalFloat: number;
  freeFloat: number;
  onCriticalPath: boolean;
  /** Off-CP but within the near-critical float band. */
  nearCritical: boolean;
  band: FloatBand;
  /** Topological depth (longest-path layer) for the swimlane layout. */
  column: number;
}

/** projectstate.NetworkSummary — the project-level CPM roll-up (server-computed). */
export interface NetworkSummary {
  /** Project duration = longest path. */
  totalDurationDays: number;
  /** Count of on-CP activities (NOT the CP day-sum). */
  criticalPathActivityCount: number;
  /** = totalDurationDays (the longest path is the CP length). */
  criticalPathDays: number;
  /** The loosest slack across all nodes. */
  maxFloat: number;
  /** Off-CP nodes inside the near-critical band. */
  nearCriticalCount: number;
}

/**
 * projectstate.NetworkMilestone — one authored zero-duration event node (M0–M5 +
 * N-DOGFOOD). id/name/public/dependsOn are AUTHORED; onCriticalPath + eventTime are
 * server-COMPUTED at read. Excluded from the risk decomposition.
 */
export interface NetworkMilestone {
  id: string;
  name: string;
  /** A demo-to-management gate vs an internal hurdle. */
  public: boolean;
  /** Predecessor activity ids (the fan-in). */
  dependsOn?: string[];
  /** Computed at read; absent on the authored on-disk doc, present on the wire. */
  onCriticalPath?: boolean;
  /** Computed at read — sim-days; max predecessor earliestFinish (0 with no preds). Absent on disk. */
  eventTime?: number;
}

/**
 * projectstate.Network — the Phase-2 project-network artifact. AUTHORED inputs
 * (dependencies, criticalPath, milestones) plus the server's COMPUTE-AT-READ block
 * (computed, summary, and the milestone onCriticalPath/eventTime). The CPM + band math
 * moved off the client onto the server (founder gate 2026-06-19): the SPA now READS the
 * `computed`/`summary` blocks rather than re-deriving them in toNetworkView.
 */
export interface NetworkModel {
  // --- AUTHORED inputs ---
  dependencies: NetworkDependency[];
  /** Activity names on the computed critical path. */
  criticalPath: string[];
  /** Authored event nodes; onCriticalPath/eventTime are server-computed at read. */
  milestones?: NetworkMilestone[];

  // --- COMPUTE-AT-READ block (absent on disk; present on the wire) ---
  /** Per-activity CPM result, keyed by activity id. Absent until the server computes it. */
  computed?: Record<string, NetworkNodeCompute>;
  /** Project-level CPM roll-up. Absent until the server computes it. */
  summary?: NetworkSummary;
}

/**
 * projectstate.Solution — one of the four solution-option artifacts. Distinguished
 * by `slotKind` (a camelCase wire name; one of the four KindXxxSolution). Duration
 * / cost / risk are NOT stored here — they are joined into the SDP review.
 */
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
  /** OptionID the assembly recommends. */
  recommendation: string;
  rationale: string;
}

/**
 * The discriminated wire form of one Phase-2 typed model: the camelCase `kind`
 * string + the concrete model's own JSON under `model` (absent before the first
 * stage). Mirrors the Go modelEnvelope; the SPA narrows on `kind`.
 */
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

/**
 * The decoded projectdesign.SessionStateView — the typed answer the SPA reads from
 * `ProjectSessionStateResponse.view`. `stage` here is the integer SessionStage
 * ordinal (the Go SessionStage carries no string codec at the view level, mirroring
 * Phase-1's SessionStateView); the OUTER response's `stage` is the string the SPA
 * polls on and the state machine consumes — `view.stage` is NEVER read by the SPA.
 *
 * Wire fields confirmed 1:1 against projectdesign.sessionStateViewWire
 * (server/internal/manager/projectdesign/contract.go): projectId, artifactKind,
 * stage, draft (the {kind,model} envelope), findings (omitted when empty).
 *
 * NOTE: unlike the Phase-1 systemdesign.SessionStateView, the Phase-2
 * projectdesign.SessionStateView has NO `failureReason` field — the Phase-2 wire
 * form never emits it. The field below is therefore RESERVED (always undefined at
 * runtime today); it is kept optional so that if the Phase-2 backend later adds the
 * same `json:"failureReason"` tag the refused panel lights up with no SPA change.
 * Do not treat it as populated until the Go view grows the field.
 */
export interface ProjectSessionStateView {
  projectId: string;
  artifactKind: ProjectArtifactKind;
  stage: number;
  draft: ProjectArtifactModelEnvelope;
  findings?: Finding[];
  /** RESERVED — not emitted by the Phase-2 Go view; always undefined today. */
  failureReason?: string;
}

/**
 * The decoded Phase-2 session-state poll result. Identical shape to the generated
 * ProjectSessionStateResponse except `view` is the concrete typed view (the
 * generated schema leaves it opaque). The string `stage` drives the SPA state
 * machine; `view.draft` carries the typed candidate model.
 */
export interface ProjectSessionState {
  projectId: string;
  artifactKind: ProjectArtifactKind;
  stage: ProjectSessionStage;
  view: ProjectSessionStateView;
}

/** Human-readable labels for the four solution options, keyed by slot kind. */
export const SOLUTION_LABELS: Partial<Record<ProjectArtifactKind, string>> = {
  normalSolution: 'Normal',
  decompressedSolution: 'Decompressed-normal',
  subcriticalSolution: 'Subcritical',
  compressedSolution: 'Compressed',
};

// ---------------------------------------------------------------------------
// Git head-state (C-CW-GIT) — the GIT-FORWARD per-activity row the SPA's
// U-SPA-GIT row cluster consumes.
//
// The git rows ride the EXISTING project head-state read (GET project →
// projectStateResponse), keyed by ActivityID — but the generated OpenAPI schema
// does NOT carry `gitRows` (codegen gap, same pattern as the Phase-2 models
// above). So we hand-mirror the `gitRow` wire DTO here — field names match the
// Go `json:"…"` tags from internal/client/web/catalog.go EXACTLY — and decode
// the project read into ProjectStateWithGit via api/projects.getProject.
//
// `prNumber` ("#N") and `prUrl` (clickable href) are SERVER-side read-time
// projections (the webClient already composed the url from the per-deployment
// repo base) — the SPA reads them DIRECTLY and NEVER constructs the url itself.
// The opaque durable `pullRequestRef` is IGNORED by the SPA (the server derives
// the display fields from it). `prNumber`/`prUrl`/`crLabel`/`isRevert` are all
// omitempty on the wire, so optional here. CI status NEVER gates any Approve
// control — it only displays.
// ---------------------------------------------------------------------------

/** The GitHub-Actions run status for the unit's PR head — a dumb reflection. */
export type CiStatus = 'in_progress' | 'failed' | 'success';

/**
 * web.gitRowDTO (catalog.go) — the git facts a single git-backed activity row
 * carries. Keyed by ActivityID in `ProjectStateWithGit.gitRows`. Field names
 * mirror the Go `json:` tags 1:1.
 */
export interface GitRow {
  /** The branch this activity lives on (e.g. `activity/C-MST`); always present. */
  branchName: string;
  /** The PR number for display ("#N") — read-time projection; absent until the PR opens. */
  prNumber?: number;
  /** The clickable PR url (server already composed it); absent until the PR opens / repo unconfigured. */
  prUrl?: string;
  /** The GitHub-Actions run status — a DUMB reflection; never gates Approve. */
  ciStatus: CiStatus;
  /** Has the human posted an "architecture +1" review onto the PR? */
  architectureApproved: boolean;
  /** The terminal git fact — the PR was merged to `main`. */
  merged: boolean;
  /** A `cr-NN` label when this branch belongs to a change request (else absent). */
  crLabel?: string;
  /** True when this PR carries inverse commits (a revert). */
  isRevert?: boolean;
  /** Server-resolved last-touch, RFC3339. */
  updatedAt: string;
}

/** The per-activity git head-state map (key = ActivityID); omitted when empty. */
export type GitRows = Record<string, GitRow>;

/**
 * The project head-state as the SPA actually consumes it: the generated
 * `ProjectState` PLUS the hand-mirrored `gitRows` map (the codegen gap above).
 * `gitRows` is omitted from the wire when the project has no git head-state, so
 * it is optional here — `gitFor` returns undefined for every not-yet-branched
 * activity (honest-empty; no fabricated row).
 *
 * `serviceContracts` is omitted from the wire when no contracts are seeded
 * (same codegen-gap pattern as gitRows/constructionRows).
 */
export type ProjectStateWithGit = ProjectState & {
  gitRows?: GitRows;
  constructionRows?: ConstructionRows;
  constructionProgress?: ConstructionProgress;
  serviceContracts?: ServiceContracts;
};

// ---------------------------------------------------------------------------
// UC3 — Construction head-state wire types (mirrors the GitRows block above).
//
// The construction rows ride the EXISTING project head-state read
// (GET project → projectStateResponse), keyed by ActivityID — the generated
// OpenAPI schema does NOT carry `constructionRows` (codegen gap, same pattern
// as gitRows above). So we hand-mirror the construction DTO here — field names
// match the Go `json:"…"` tags EXACTLY.
// ---------------------------------------------------------------------------

/** One produced artifact within a construction activity row. */
export interface ProducedArtifactRow {
  kind: string;
  title: string;
  source: string;
  produced: boolean;
  note: string;
}

/**
 * web.constructionRowDTO — the construction facts a single activity row carries.
 * Keyed by ActivityID in `ProjectStateWithGit.constructionRows`.
 */
export interface ConstructionRow {
  activityId: string;
  kind: 'service' | 'frontend' | 'testing';
  status: 'integrated' | 'in-review' | 'in-construction';
  phase: string;
  produced?: ProducedArtifactRow[];
}

/** The per-activity construction head-state map (key = ActivityID); omitted when empty. */
export type ConstructionRows = Record<string, ConstructionRow>;

/** Earned-value curve data for the construction progress view. */
export interface EvCurves {
  weeks: number[];
  earned: number[];
  planned: number[];
  spi: number;
}

/** Project-level construction progress — aggregated EV metrics. */
export interface ConstructionProgress {
  week: number;
  totalWeeks: number;
  handOffModel: string;
  supervisionCap: number;
  ev: EvCurves;
}

/** Lookup helper — undefined for not-yet-branched activities (honest-empty). */
export function gitFor(
  project: ProjectStateWithGit | undefined,
  activityId: string
): GitRow | undefined {
  if (project?.gitRows === undefined || activityId.length === 0) return undefined;
  return project.gitRows[activityId];
}

// ---------------------------------------------------------------------------
// Service Contracts — wire types (mirrors servicecontracts.go DTOs).
//
// The serviceContracts map rides the EXISTING project head-state read
// (GET project → projectStateResponse), keyed by component name — the
// generated OpenAPI schema does NOT carry `serviceContracts` (codegen gap,
// same pattern as gitRows/constructionRows above). Hand-mirrored here;
// field names match the Go `json:` tags 1:1.
// ---------------------------------------------------------------------------

/** One inbound caller or outbound callee in a service contract. */
export interface ContractParty {
  name: string;
  layer: string;
  /** Empty for inbound; how the focal component calls this callee (outbound only). */
  how?: string;
}

/** One field in a Go struct shown in the "Code/interface" view. */
export interface GoField {
  name: string;
  type: string;
  note?: string;
}

/** One Go struct (request or response) carried by an op. */
export interface ContractStruct {
  name: string;
  fields: GoField[];
}

/** One operation exposed by the contract. */
export interface ContractOp {
  signature: string;
  stereotype: string;
  note?: string;
  inputs?: ContractStruct[];
  outputs?: ContractStruct[];
}

/** One revision in the contract revision history. */
export interface ContractRevision {
  rev: string;
  at: string;
  by: string;
  byActivity?: string;
  summary?: string;
}

/** The full typed service contract for one component. */
export interface ServiceContract {
  component: string;
  layer: string;
  stereotype?: string;
  volatility?: string;
  /** "FROZEN" | "IN-DESIGN" */
  status?: string;
  inbound?: ContractParty[];
  outbound?: ContractParty[];
  ops?: ContractOp[];
  dataContracts?: string[];
  errorModel?: string;
  idempotency?: string;
  revisions?: ContractRevision[];
}

/** Per-component service contract map (key = component name). */
export type ServiceContracts = Record<string, ServiceContract>;
