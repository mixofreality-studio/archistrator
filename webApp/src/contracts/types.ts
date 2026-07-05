/**
 * App-facing types + constants for the SPA.
 *
 * The generated `schema.ts` is the SOLE wire contract (consumed by the api layer
 * via the openapi-fetch client). The new server OAS encodes enums as integer
 * ordinals and uses per-manager namespaced, PascalCase view shapes. The SPA's
 * screens/state-machines are written against stable lowerCamel STRING enums and
 * camelCase view shapes, so the api layer (hooks + wire.ts) maps wire→app at the
 * boundary; everything below is that app-facing contract.
 *
 * Artifact MODEL payloads stay opaque in the OAS, so their decode types live in
 * ./models and are re-exported here for the screens that import them.
 */
import type { ArtifactModelEnvelope, ProjectArtifactModelEnvelope } from './models';

// Re-export the opaque-model decode types so screens keep importing from one place.
export type {
  ArtifactModelEnvelope,
  ProjectArtifactModelEnvelope,
  Money,
  UsageAssumption,
  SettlementTerms,
  PlanningAssumptionsModel,
  ActivityItem,
  ActivityListModel,
  NetworkDependency,
  FloatBand,
  NetworkNodeCompute,
  NetworkSummary,
  NetworkMilestone,
  NetworkModel,
  SolutionModel,
  RiskRow,
  RiskModelModel,
  SdpOptionRow,
  SdpReviewModel,
} from './models';

// ---------------------------------------------------------------------------
// Phase-1 (System Design) wire-string enums.
// ---------------------------------------------------------------------------

export type ArtifactKind =
  | 'mission'
  | 'glossary'
  | 'scrubbedRequirements'
  | 'volatilities'
  | 'coreUseCases'
  | 'system'
  | 'operationalConcepts'
  | 'standardCheck';

export type ArtifactKindFull =
  | ArtifactKind
  | 'planningAssumptions'
  | 'activityList'
  | 'network'
  | 'normalSolution'
  | 'subcriticalSolution'
  | 'compressedSolution'
  | 'decompressedSolution'
  | 'riskModel'
  | 'sdpReview';

export type ReviewDecision = 'approve' | 'reject' | 'withdraw';

export type SessionStage =
  | 'drafting'
  | 'awaitingReview'
  | 'redrafting'
  | 'committed'
  | 'withdrawn'
  | 'refused'
  | 'draftFailed'
  | 'unknown';

export type Severity = 'info' | 'warning' | 'error';

/** engine.Finding — one machine-checkable validation rule violation. */
export interface Finding {
  ruleId: string;
  severity: Severity;
  message: string;
  location?: { ordinal: number; section: string };
}

/**
 * A JSONPath-anchored "send back" comment. `anchorText` is the client-supplied
 * snapshot of the anchored item's RENDERED text at post time — the durable review
 * ledger stores it so a later reader (or a redraft that moved the item) still sees
 * what the reviewer was pointing at. Free-form (unanchored) feedback rides the
 * reject `feedback.notes`, never this array, so anchored entries always carry both
 * a jsonPath and a non-empty anchorText.
 */
export interface AnchoredComment {
  jsonPath: string;
  text: string;
  anchorText: string;
}

/** Server review-ledger comment status: open → addressed (by an agent response) → optionally waived. */
export type ReviewCommentStatus = 'open' | 'addressed' | 'waived';

/**
 * One durable review-thread entry as the server exposes it on the session view.
 * Distinct from the client-side pending {@link AnchoredComment}: these have been
 * committed to the ledger, carry an author role + round, a lifecycle `status`, and
 * (once the agent redrafts) a `response`.
 */
export interface ReviewCommentView {
  id: string;
  /** JSONPath into the typed model this entry anchors to (empty for free-form). */
  anchor: string;
  /** Snapshot of the anchored item's rendered text at post time (empty for free-form). */
  anchorText: string;
  text: string;
  authorRole: string;
  round: number;
  status: ReviewCommentStatus;
  /** The agent's per-entry response committed on redraft; empty while still open. */
  response: string;
}

export interface ResearchSource {
  title: string;
  content: string;
}

export interface ResearchInput {
  sources: ResearchSource[];
}

/** ArtifactStage ordinal (head-state slot stage): 0..4. */
export type ArtifactStageOrdinal = 0 | 1 | 2 | 3 | 4;

/** One artifact slot of the head-state. */
export interface ArtifactSlotView {
  kind: ArtifactKindFull;
  stage: ArtifactStageOrdinal;
  model: ArtifactModelEnvelope;
  notes?: string;
}

export type ProjectPhase = 'systemDesign' | 'projectDesign' | 'construction' | 'unknown';

/** One catalog row for the landing grid. */
export interface ProjectSummary {
  projectId: string;
  name: string;
  owner: string;
  phase: ProjectPhase;
  committedCount: number;
  totalCount: number;
  updatedAt: string;
}

/** The full typed head-state of one project. */
export interface ProjectState {
  projectId: string;
  name: string;
  owner: string;
  phase: ProjectPhase;
  version: number;
  research: ResearchInput;
  slots: ArtifactSlotView[];
}

/** Point-in-time view of one Phase-1 co-authoring session. */
export interface SessionStateView {
  projectId: string;
  artifactKind: ArtifactKind;
  /** Integer SessionStage ordinal on the inner view; the SPA reads the outer string stage. */
  stage: number;
  draft: ArtifactModelEnvelope;
  findings?: Finding[];
  failureReason?: string;
  /** URL of the failed CI run, when the failure came from a job that actually ran. */
  failureRunUrl?: string;
  /** The durable review-ledger thread for this slot (open/addressed/waived entries). */
  reviewThread?: ReviewCommentView[];
}

/** The Phase-1 session-state poll result (outer string stage drives the machine). */
export interface SessionStateResponse {
  projectId: string;
  artifactKind: ArtifactKind;
  stage: SessionStage;
  view: SessionStateView;
}

export interface PhaseAdvanceResponse {
  advanced: boolean;
  missingArtifacts: ArtifactKind[];
}

export interface SessionRefResponse {
  sessionRef: string;
}

export interface ErrorResponse {
  error: string;
  code: string;
}

/** Optional rationale woven into a reject/withdraw decision. */
export interface ReviewDecisionDetail {
  feedback?: string;
  comments?: AnchoredComment[];
}

/**
 * The seven (eight slots incl. standardCheck) Phase-1 Method artifacts, in order.
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

export const REVIEWABLE_STAGE: SessionStage = 'awaitingReview';

export const TERMINAL_STAGES: readonly SessionStage[] = [
  'committed',
  'withdrawn',
  'refused',
  'draftFailed',
];

// ---------------------------------------------------------------------------
// Phase-2 (Project Design) wire-string enums.
// ---------------------------------------------------------------------------

export type ProjectArtifactKind =
  | 'planningAssumptions'
  | 'activityList'
  | 'network'
  | 'normalSolution'
  | 'decompressedSolution'
  | 'subcriticalSolution'
  | 'compressedSolution'
  | 'riskModel'
  | 'sdpReview';

export type ProjectSessionStage =
  | 'drafting'
  | 'assemblingSdp'
  | 'awaitingReview'
  | 'redrafting'
  | 'committed'
  | 'withdrawn'
  | 'refused'
  | 'draftFailed'
  | 'unknown';

export type SDPDecision = 'commit' | 'rejectAll';

/** Optional rationale woven into an SDP decision. */
export interface SDPDecisionDetail {
  optionId?: string;
  feedback?: string;
}

/** The decoded Phase-2 session-state view. */
export interface ProjectSessionStateView {
  projectId: string;
  artifactKind: ProjectArtifactKind;
  stage: number;
  draft: ProjectArtifactModelEnvelope;
  findings?: Finding[];
  failureReason?: string;
  /** The durable review-ledger thread for this slot (open/addressed/waived entries). */
  reviewThread?: ReviewCommentView[];
}

/** The decoded Phase-2 session-state poll result. */
export interface ProjectSessionState {
  projectId: string;
  artifactKind: ProjectArtifactKind;
  stage: ProjectSessionStage;
  view: ProjectSessionStateView;
}

export interface ProjectPhaseAdvanceResponse {
  advanced: boolean;
  missingArtifacts: ProjectArtifactKind[];
}

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

export const SDP_REVIEW_KIND: ProjectArtifactKind = 'sdpReview';

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

export const PROJECT_REVIEWABLE_STAGE: ProjectSessionStage = 'awaitingReview';

export const PROJECT_TERMINAL_STAGES: readonly ProjectSessionStage[] = [
  'committed',
  'withdrawn',
  'refused',
  'draftFailed',
];

/** Human-readable labels for the four solution options, keyed by slot kind. */
export const SOLUTION_LABELS: Partial<Record<ProjectArtifactKind, string>> = {
  normalSolution: 'Normal',
  decompressedSolution: 'Decompressed-normal',
  subcriticalSolution: 'Subcritical',
  compressedSolution: 'Compressed',
};

// ---------------------------------------------------------------------------
// Git head-state (C-CW-GIT) — the GIT-FORWARD per-activity row.
// `prNumber`/`prUrl` are SERVER-side read-time projections (the SPA never builds
// the url). CI status NEVER gates any Approve control — it only displays.
// ---------------------------------------------------------------------------

export type CiStatus = 'in_progress' | 'failed' | 'success';

export interface GitRow {
  branchName: string;
  prNumber?: number;
  prUrl?: string;
  ciStatus: CiStatus;
  architectureApproved: boolean;
  merged: boolean;
  crLabel?: string;
  isRevert?: boolean;
  updatedAt: string;
}

export type GitRows = Record<string, GitRow>;

// ---------------------------------------------------------------------------
// Construction head-state (rides the project read, keyed by ActivityID).
// ---------------------------------------------------------------------------

export interface ProducedArtifactRow {
  kind: string;
  title: string;
  source: string;
  produced: boolean;
  note: string;
}

export type TestingVariantName =
  | 'plan'
  | 'harness'
  | 'perf'
  | 'systemTest'
  | 'qaProcess';

export interface ConstructionRow {
  activityId: string;
  kind: 'service' | 'frontend' | 'testing' | 'deployment' | 'documentation';
  /** Testing sub-type; present only when kind === 'testing'. */
  variant?: TestingVariantName;
  status: 'integrated' | 'in-review' | 'in-construction';
  phase: string;
  produced?: ProducedArtifactRow[];
}

export type ConstructionRows = Record<string, ConstructionRow>;

export interface EvCurves {
  weeks: number[];
  earned: number[];
  planned: number[];
  spi: number;
}

// EvPoint is one recorded weekly earned-value observation (ground truth), as
// captured by the-method-project-tracking into .constructionProgress.points.
// Distinct from EvCurves, which is the estimator-derived projection.
export interface EvPoint {
  week: number;
  earnedPct: number;
  plannedPct: number;
  note: string;
  acPct?: number;
}

export interface ConstructionProgress {
  week: number;
  totalWeeks: number;
  handOffModel: string;
  supervisionCap: number;
  ev: EvCurves;
  points?: EvPoint[];
}

// ---------------------------------------------------------------------------
// Service Contracts — per-component map riding the project read.
// ---------------------------------------------------------------------------

export interface ContractParty {
  name: string;
  layer: string;
  how?: string;
}

export interface GoField {
  name: string;
  type: string;
  note?: string;
}

export interface ContractStruct {
  name: string;
  fields: GoField[];
}

export interface ContractOp {
  signature: string;
  stereotype: string;
  note?: string;
  inputs?: ContractStruct[];
  outputs?: ContractStruct[];
}

export interface ContractRevision {
  rev: string;
  at: string;
  by: string;
  byActivity?: string;
  summary?: string;
}

export interface ServiceContract {
  component: string;
  layer: string;
  stereotype?: string;
  volatility?: string;
  status?: string;
  inbound?: ContractParty[];
  outbound?: ContractParty[];
  ops?: ContractOp[];
  dataContracts?: string[];
  errorModel?: string;
  idempotency?: string;
  revisions?: ContractRevision[];
}

export type ServiceContracts = Record<string, ServiceContract>;

/**
 * The committed review-gate policy for a project: which (activityType, phase)
 * pairs require a human approval signal before the construction loop advances.
 * Keyed by ActivityType wire name ("service" | "frontend" | "testing") → list
 * of canonical ActivityMethodPhase strings ("detailed_design", "integration",
 * "test_plan", etc.). Absent from the read when no policy has been configured.
 */
export interface ReviewPolicyView {
  gatedPhasesByType: Record<string, string[]>;
}

export interface TestRunView {
  id: string;
  passed: number;
  failed: number;
  note: string;
}

export interface DefectView {
  id: string;
  title: string;
  severity: string;
  note: string;
}

/** One concrete input argument to a step's operation call. */
export interface TestArgView {
  name: string;
  /** concrete value as JSON/text (so a harness can emit runnable code). */
  value: string;
  /** contract param type name ($def), optional. */
  schemaRef?: string;
}

/** The expected outcome of a step: a result value, or an expected error. */
export interface TestExpectView {
  /** expected result value/shape (empty when an error is expected). */
  result?: string;
  errorExpected: boolean;
  /** expected error code / type. */
  errorCode?: string;
}

/** One black-box step: a transport-agnostic manager-operation call with concrete I/O. */
export interface TestStepView {
  seq: number;
  component: string;
  operation: string;
  /** last-run result: '' (unrun) | 'red' (failing) | 'green' (passing). */
  status?: string;
  inputs: TestArgView[] | null;
  expect: TestExpectView;
  assertion?: string;
}

/** One falsification attempt within a scenario: happy / negative / boundary. */
export interface TestCaseView {
  id: string;
  /** 'happy' | 'negative' | 'boundary'. */
  kind: string;
  title: string;
  /** what this case proves — the failure mode it exposes. */
  proves?: string;
  /** overall success, or the specific expected failure. */
  expectedOutcome?: string;
  steps: TestStepView[] | null;
}

/** One black-box system-test scenario: a core use case and its test cases. */
export interface TestScenarioView {
  id: string;
  useCase: string;
  title: string;
  /** what this scenario proves and why it matters (the failure mode it exposes). */
  description?: string;
  cases: TestCaseView[] | null;
}

/** The system test plan — the renderable black-box operation-sequence scenarios. */
export interface SystemTestPlanView {
  scenarios: TestScenarioView[] | null;
}

/** Project-level testing artifacts produced by N-* activities. */
export interface TestingStateView {
  testRuns: TestRunView[] | null;
  defects: DefectView[] | null;
  systemTestPlan?: SystemTestPlanView;
}

/**
 * The project head-state as the SPA consumes it: the typed head-state PLUS the
 * per-activity git / construction maps + service contracts + construction progress.
 * All optional — omitted (honest-empty) when the project carries no such state.
 */
export type ProjectStateWithGit = ProjectState & {
  gitRows?: GitRows;
  constructionRows?: ConstructionRows;
  constructionProgress?: ConstructionProgress;
  serviceContracts?: ServiceContracts;
  /** Persisted review-gate policy — absent when no policy has been saved. */
  reviewPolicy?: ReviewPolicyView;
  /** Project-level testing artifacts — absent until an N-* activity produces output. */
  testingState?: TestingStateView;
};

/** Lookup helper — undefined for not-yet-branched activities (honest-empty). */
export function gitFor(
  project: ProjectStateWithGit | undefined,
  activityId: string
): GitRow | undefined {
  if (project?.gitRows === undefined || activityId.length === 0) return undefined;
  return project.gitRows[activityId];
}

// ---------------------------------------------------------------------------
// Construction session (Phase-3 superviseConstruction) app types.
// ---------------------------------------------------------------------------

export type ConstructionStage =
  | 'dispatching'
  | 'pipelineRunning'
  | 'reviewing'
  | 'awaitingTakeover'
  | 'awaitingApproval'
  | 'paused'
  | 'exited'
  | 'unknown';

export type PipelinePhase = 'pending' | 'running' | 'succeeded' | 'failed' | 'unknown';

export type OverrideKind = 'takeover' | 'retry' | 'skip' | 'reassign';

/** Phase-gate approval decision (maps to PhaseDecision iota: approve=1, sendBack=2). */
export type PhaseDecision = 'approve' | 'sendBack';

export interface ConstructionReviewer {
  role: string;
  perspective: string;
  referenceArtifact?: string;
  mayAmend: boolean;
}

export interface ConstructionReviewSet {
  reviewers?: ConstructionReviewer[];
}

export interface FlaggedVariance {
  projectId: string;
  activityId: string;
  summary: string;
}

export interface ConstructionSessionView {
  projectId: string;
  activityId?: string;
  /** Integer ConstructionStage ordinal on the inner view. */
  stage: number;
  pipelinePhase?: number;
  reviewSet?: ConstructionReviewSet;
  variance?: FlaggedVariance;
}

export interface ConstructionSessionState {
  projectId: string;
  activityId?: string;
  stage: ConstructionStage;
  pipelinePhase?: PipelinePhase;
  view: ConstructionSessionView;
}
