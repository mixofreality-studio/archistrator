/**
 * Wire ↔ app mapping at the generated-client boundary.
 *
 * The openapi-fetch client returns the generated (per-manager namespaced,
 * PascalCase, integer-enum) wire types. Every API hook funnels its decoded `data`
 * through the "wire → app" mappers below to produce the SPA's stable app view
 * types (camelCase, lowerCamel string enums). The `{kind, model}` draft envelope
 * IS now typed on the wire (schema.ts's oneOf over the generated `Model*` shapes,
 * appgen step-4 RC1) — mapEnvelope/mapProjectEnvelope still take it in through a
 * structural `{kind: string; model?: unknown}` shape rather than the exact
 * generated union because the SAME two mappers serve several distinct generated
 * envelope schemas (session draft, slot view, …) whose `model` oneOf members
 * differ slightly; the single `as` cast at the end is the one place that trusts
 * the server's kind/model pairing (see the mapper bodies for why a kind-keyed
 * switch there wouldn't add real safety). The "app → wire" section at the bottom
 * holds the reverse ordinal encoders write-path hooks use to build request bodies.
 *
 * Every ordinal ↔ app-string table below is sourced from the generated
 * enums.gen.ts (derived from the OAS's x-enum-varnames) for the enums where that
 * derivation is mechanical. The 7 enums where it is NOT mechanical (a casing
 * convention or a deliberate product-decision collapse — see the per-block
 * comments there) instead come from src/contracts/enumMappings.ts, a thin hand
 * mapping layer keyed by the SAME generated varname union, so a Go const rename
 * still breaks tsc here rather than drifting silently.
 */
import type { components } from './schema';
import {
  ACTIVE_ROLE_ORDINAL_TO_APP,
  type ActiveRole,
  ACTIVE_STEP_ORDINAL_TO_APP,
  type ActiveStep,
  ACTIVITY_TYPE_ORDINAL_TO_APP,
  ARTIFACT_KIND_APP_TO_ORDINAL,
  ARTIFACT_KIND_ORDINAL_TO_APP,
  AUTOSCALE_ACTION_ORDINAL_TO_APP,
  CONSTRUCTION_STAGE_ORDINAL_TO_APP,
  DESIRED_STATE_REASON_APP_TO_ORDINAL,
  EPISODE_KIND_ORDINAL_TO_APP,
  type EpisodeKind,
  EPISODE_OUTCOME_ORDINAL_TO_APP,
  type EpisodeOutcome,
  FAILURE_REASON_ORDINAL_TO_APP,
  type FailureReason,
  OVERRIDE_KIND_APP_TO_ORDINAL,
  PATCH_KIND_APP_TO_ORDINAL,
  PHASE_DECISION_APP_TO_ORDINAL,
  PROJECT_PHASE_ORDINAL_TO_APP,
  REVIEW_DECISION_APP_TO_ORDINAL,
  SDP_DECISION_APP_TO_ORDINAL,
  SESSION_STAGE_ORDINAL_TO_APP,
  type ActivityType,
} from './enums.gen.ts';
import {
  buildStatusRowFromOrdinal,
  ciStatusFromOrdinal,
  pipelinePhaseFromOrdinal,
  projectSessionStageFromOrdinal,
  runtimePhaseFromOrdinal,
  autoscalerModeFromOrdinal,
  testingVariantFromOrdinal,
} from './enumMappings.ts';
import type {
  ArtifactKind,
  ArtifactKindFull,
  ArtifactSlotView,
  CheckItem,
  ConstructionProgress,
  ConstructionRow,
  ConstructionSessionState,
  ConstructionStage,
  DesignHealth,
  EpisodeLineage,
  EpisodeRecordView,
  EpisodeTimeline,
  EpisodeUsage,
  EvPoint,
  Finding,
  GitRow,
  GitRows,
  OverrideKind,
  PhaseDecision,
  ProducedArtifactRow,
  ProjectArtifactKind,
  ProjectPhase,
  ProjectSessionState,
  ProjectState,
  ProjectStateWithGit,
  ProjectSummary,
  ResearchInput,
  ReviewCommentAddressee,
  ReviewCommentStatus,
  ReviewCommentType,
  PmCritiqueView,
  ReviewCommentView,
  ReviewDecision,
  SDPDecision,
  SessionStage,
  ServiceContract,
  ServiceContracts,
  SessionStateResponse,
  ConstructionRows,
  SubagentSpan,
  TimelineEvent,
} from './types';
import type { ArtifactModelEnvelope, Money, ProjectArtifactModelEnvelope } from './types';
import type { CostProjection, OperationsView } from './operationsTypes';
import { deriveOperating } from './operating.ts';

type Schemas = components['schemas'];

// --- wire → app: ordinal readers (mechanical — sourced from enums.gen.ts) ---

/** ArtifactKind ordinal (0..16), shared Phase-1 + Phase-2 ordering. */
function artifactKindFullFromOrdinal(ordinal: number): ArtifactKindFull {
  return ARTIFACT_KIND_ORDINAL_TO_APP[ordinal] ?? 'mission';
}

/** Phase-1 narrowing — the same table, typed back to the Phase-1 union. */
export function systemArtifactKindFromOrdinal(ordinal: number): ArtifactKind {
  return artifactKindFullFromOrdinal(ordinal) as ArtifactKind;
}

/** Phase-2 narrowing — the same table, typed back to the Phase-2 union. */
export function projectArtifactKindFromOrdinal(ordinal: number): ProjectArtifactKind {
  return artifactKindFullFromOrdinal(ordinal) as ProjectArtifactKind;
}

function sessionStageFromOrdinal(ordinal: number): SessionStage {
  return SESSION_STAGE_ORDINAL_TO_APP[ordinal] ?? 'unknown';
}

/** ActiveRole (0 none,1 architect,2 productManager); old servers omit → none. */
function activeRoleFromOrdinal(ordinal: number): ActiveRole {
  return ACTIVE_ROLE_ORDINAL_TO_APP[ordinal] ?? 'none';
}

/** ActiveStep (0 none,1 drafting,2 critiquing,3 revising); old servers omit → none. */
function activeStepFromOrdinal(ordinal: number): ActiveStep {
  return ACTIVE_STEP_ORDINAL_TO_APP[ordinal] ?? 'none';
}

function projectPhaseFromOrdinal(ordinal: number): ProjectPhase {
  return PROJECT_PHASE_ORDINAL_TO_APP[ordinal] ?? 'unknown';
}

function constructionStageFromOrdinal(ordinal: number): ConstructionStage {
  return CONSTRUCTION_STAGE_ORDINAL_TO_APP[ordinal] ?? 'unknown';
}

/** ProjectActivityType (0 service,1 frontend,2 testing,3 deployment,4 documentation,
 * 5 uiDesign,6 integration). */
function activityRowKindFromOrdinal(ordinal: number): ActivityType {
  return ACTIVITY_TYPE_ORDINAL_TO_APP[ordinal] ?? 'service';
}

/** SystemDesignFailureReason (0 unknown,1 pipelineFailed,2 pipelineCancelled,
 * 3 pipelineTimedOut,4 varianceExhausted,5 escalationTimedOut,6 componentUnresolved,
 * 7 dependencyUnresolved,8 dependencyCycle). */
function failureReasonFromOrdinal(ordinal: number): FailureReason {
  return FAILURE_REASON_ORDINAL_TO_APP[ordinal] ?? 'unknown';
}

/** OperationsAutoscaleAction (0 noChange,1 scaleUp,2 scaleDown,3 pause,4 resume). */
function autoscaleActionFromOrdinal(ordinal: number): string {
  return AUTOSCALE_ACTION_ORDINAL_TO_APP[ordinal] ?? 'noChange';
}

// --- shared -----------------------------------------------------------------

export function mapMoney(w: Schemas['OperationsMoney']): Money {
  return { minorUnits: w.MinorUnits, currency: w.Currency };
}

function mapFinding(w: Schemas['SystemDesignFinding'] | Schemas['ProjectDesignFinding']): Finding {
  return {
    ruleId: w.ruleId,
    severity: w.severity,
    message: w.message,
    ...(w.location !== undefined
      ? { location: { ordinal: w.location.ordinal, section: w.location.section } }
      : {}),
  };
}

/** Wire CheckItem → app CheckItem (identical shape; status is the pass|waived|fail enum). */
function mapCheckItem(w: Schemas['SystemDesignCheckItem']): CheckItem {
  return {
    section: w.section,
    guideline: w.guideline,
    status: w.status,
    justification: w.justification,
  };
}

/** The GetDesignHealth read-model → app DesignHealth (empty arrays serialize as [], never null). */
export function mapDesignHealth(w: Schemas['SystemDesignDesignHealth']): DesignHealth {
  return {
    findings: w.findings.map(mapFinding),
    waivers: w.waivers.map(mapCheckItem),
    attestations: w.attestations.map(mapCheckItem),
    evaluatedAtRevision: w.evaluatedAtRevision,
  };
}

/** Normalize a wire review-status string into the app union (unknown → 'open'). */
function reviewStatus(s: string): ReviewCommentStatus {
  return s === 'addressed' || s === 'waived' ? s : 'open';
}

/** Normalize the wire comment type (empty/legacy → 'changeRequest'). */
function reviewType(s: string): ReviewCommentType {
  return s === 'question' ? 'question' : 'changeRequest';
}

/** Normalize the wire addressee (only meaningful for questions). */
function reviewAddressee(s: string): ReviewCommentAddressee {
  return s === 'pm' || s === 'architect' ? s : '';
}

/** One durable review-ledger entry. The two manager shapes are structurally identical. */
function mapReviewComment(
  w: Schemas['SystemDesignReviewCommentView'] | Schemas['ProjectDesignReviewCommentView']
): ReviewCommentView {
  return {
    id: w.id,
    anchor: w.anchor,
    anchorText: w.anchorText,
    text: w.text,
    authorRole: w.authorRole,
    round: w.round,
    status: reviewStatus(w.status),
    response: w.response,
    type: reviewType(w.type),
    addressee: reviewAddressee(w.addressee),
  };
}

/**
 * Decode the {kind, model} envelope into the typed Phase-1+2 envelope.
 *
 * Both casts here are the honest boundary casts, kept deliberately (step4-task4
 * review): `kind` is a plain wire string the server derives from the closed
 * ArtifactKind enum, and `model` is already the generated oneOf union of every
 * `Model*` shape (schema.ts) — but neither is narrowable to the EXACT pairing
 * (kind='mission' implies model:MissionStatement) without a runtime validator,
 * because the wire type only says "one of the 14", not "this one because of
 * that". A 14-branch kind-keyed switch here would just re-state the same
 * trust-the-server assumption one level down (each branch would still need its
 * own cast) without adding a real runtime check, so it was rejected as churn.
 * Every real consumer narrows again on `kind` at the point of use (see
 * adapters.ts `narrow`), which is where a mismatched pairing would actually
 * surface as a wrong-shaped read.
 */
function mapEnvelope(w: { kind: string; model?: unknown }): ArtifactModelEnvelope {
  const env: ArtifactModelEnvelope = { kind: w.kind as ArtifactKindFull };
  if (w.model !== undefined && w.model !== null) {
    env.model = w.model as NonNullable<ArtifactModelEnvelope['model']>;
  }
  return env;
}

/** Decode the {kind, model} envelope into the typed Phase-2 envelope — same honest
 *  boundary casts as {@link mapEnvelope}, narrowed to the Phase-2 kind/model unions. */
function mapProjectEnvelope(w: { kind: string; model?: unknown }): ProjectArtifactModelEnvelope {
  const env: ProjectArtifactModelEnvelope = { kind: w.kind as ProjectArtifactKind };
  if (w.model !== undefined && w.model !== null) {
    env.model = w.model as NonNullable<ProjectArtifactModelEnvelope['model']>;
  }
  return env;
}

// --- project catalog + head-state ------------------------------------------

export function mapProjectSummary(w: Schemas['SystemDesignProjectSummary']): ProjectSummary {
  return {
    projectId: w.ProjectID,
    name: w.Name,
    owner: w.Owner,
    phase: projectPhaseFromOrdinal(w.Phase),
    committedCount: w.CommittedCount,
    totalCount: w.TotalCount,
    updatedAt: w.UpdatedAt,
    // Operating (Task 14, finish-construction): the catalog's derived construction-
    // complete signal (Task 13's isConstructionComplete, mirrored client-side by
    // deriveOperating — see operating.ts). Omitted (nil) unless true, mirroring
    // the wire field's own "omitempty" convention.
    ...(w.ConstructionComplete === true ? { constructionComplete: true } : {}),
  };
}

function mapResearchInput(w: Schemas['SystemDesignResearchInput']): ResearchInput {
  return { sources: (w.sources ?? []).map((s) => ({ title: s.title, content: s.content })) };
}

function mapSlot(w: Schemas['SystemDesignArtifactSlotView']): ArtifactSlotView {
  // PM-P1-2: the server records why a committed slot went stale as
  // `staleBasisCause: {upstreamKind, upstreamRevision}` (omitempty — absent when
  // not stale or when the slot went stale before cause recording existed).
  // Compose the operator-facing string here; absent cause falls back to the
  // popover's generic copy.
  const rawCause = (
    w as { staleBasisCause?: { upstreamKind?: unknown; upstreamRevision?: unknown } }
  ).staleBasisCause;
  const staleCause =
    rawCause && typeof rawCause.upstreamKind === 'string' && rawCause.upstreamKind.length > 0
      ? typeof rawCause.upstreamRevision === 'number'
        ? `${rawCause.upstreamKind} (rev ${String(rawCause.upstreamRevision)})`
        : rawCause.upstreamKind
      : undefined;
  // PM-P2-4: the server records commit provenance as
  // `provenance: {committedAt, approvedBy, draftedBy}` (each field omitempty; the whole
  // object absent on never-committed / pre-provenance slots).
  const rawProv = (
    w as { provenance?: { committedAt?: unknown; approvedBy?: unknown; draftedBy?: unknown } }
  ).provenance;
  const provenance = rawProv
    ? {
        ...(typeof rawProv.committedAt === 'string' && rawProv.committedAt.length > 0
          ? { committedAt: rawProv.committedAt }
          : {}),
        ...(typeof rawProv.approvedBy === 'string' && rawProv.approvedBy.length > 0
          ? { approvedBy: rawProv.approvedBy }
          : {}),
        ...(typeof rawProv.draftedBy === 'string' && rawProv.draftedBy.length > 0
          ? { draftedBy: rawProv.draftedBy }
          : {}),
      }
    : undefined;
  return {
    // Same honest boundary cast as mapEnvelope's `kind` — see its doc comment.
    kind: w.kind as ArtifactKindFull,
    stage: w.stage,
    model: mapEnvelope(w.model),
    ...(w.notes !== undefined && w.notes !== null ? { notes: w.notes } : {}),
    ...(w.revisions !== undefined ? { revisions: w.revisions } : {}),
    ...(w.staleBasis === true ? { staleBasis: true } : {}),
    ...(typeof staleCause === 'string' && staleCause.length > 0 ? { staleCause } : {}),
    ...(provenance && Object.keys(provenance).length > 0 ? { provenance } : {}),
  };
}

function mapGitRow(w: Schemas['SystemDesignActivityGitStatus']): GitRow {
  return {
    branchName: w.BranchName,
    ...(w.PrNumber > 0 ? { prNumber: w.PrNumber } : {}),
    ...(w.PrURL.length > 0 ? { prUrl: w.PrURL } : {}),
    ciStatus: ciStatusFromOrdinal(w.CICheck),
    architectureApproved: w.ArchApproved,
    merged: w.Merged,
    ...(w.CRLabel.length > 0 ? { crLabel: w.CRLabel } : {}),
    ...(w.IsRevert ? { isRevert: w.IsRevert } : {}),
    updatedAt: w.UpdatedAt,
  };
}

function mapProducedArtifact(w: Schemas['SystemDesignProducedArtifact']): ProducedArtifactRow {
  return { kind: w.Kind, title: w.Title, source: w.Source, produced: w.Produced, note: w.Note };
}

function mapConstructionRow(w: Schemas['SystemDesignActivityConstructionStatus']): ConstructionRow {
  // kind is sourced from the derived Type (the view model computes it from the
  // activity id); variant is only meaningful for testing activities.
  const kind = activityRowKindFromOrdinal(w.Type);
  const variant = kind === 'testing' ? testingVariantFromOrdinal(w.Variant) : undefined;
  // FailureReason/FailureDetail are only meaningful on a terminal-fail row: every
  // other row carries the zero-value reason (`unknown`) and an empty detail, so
  // they are dropped rather than decorating healthy rows with a phantom failure.
  const failureReason = failureReasonFromOrdinal(w.FailureReason);
  const carriesFailure = failureReason !== 'unknown';
  return {
    activityId: w.ActivityID,
    kind,
    ...(variant !== undefined ? { variant } : {}),
    status: buildStatusRowFromOrdinal(w.BuildStatus),
    phase: w.CurrentPhase,
    ...(w.Produced !== null ? { produced: w.Produced.map(mapProducedArtifact) } : {}),
    ...(carriesFailure ? { failureReason } : {}),
    ...(carriesFailure && w.FailureDetail.length > 0 ? { failureDetail: w.FailureDetail } : {}),
  };
}

function mapServiceContract(w: Schemas['SystemDesignServiceContract']): ServiceContract {
  return {
    component: w.Component,
    layer: w.Layer,
    stereotype: w.Stereotype,
    volatility: w.Volatility,
    status: w.Status,
    errorModel: w.ErrorModel,
    idempotency: w.Idempotency,
    ...(w.DataContracts !== null ? { dataContracts: w.DataContracts } : {}),
    ...(w.Inbound !== null
      ? { inbound: w.Inbound.map((p) => ({ name: p.Name, layer: p.Layer, how: p.How })) }
      : {}),
    ...(w.Outbound !== null
      ? { outbound: w.Outbound.map((p) => ({ name: p.Name, layer: p.Layer, how: p.How })) }
      : {}),
    ...(w.Ops !== null
      ? {
          ops: w.Ops.map((o) => ({
            signature: o.Signature,
            stereotype: o.Stereotype,
            note: o.Note,
            ...(o.Inputs !== null
              ? {
                  inputs: o.Inputs.map((s) => ({
                    name: s.Name,
                    fields: (s.Fields ?? []).map((f) => ({
                      name: f.Name,
                      type: f.Type,
                      note: f.Note,
                    })),
                  })),
                }
              : {}),
            ...(o.Outputs !== null
              ? {
                  outputs: o.Outputs.map((s) => ({
                    name: s.Name,
                    fields: (s.Fields ?? []).map((f) => ({
                      name: f.Name,
                      type: f.Type,
                      note: f.Note,
                    })),
                  })),
                }
              : {}),
          })),
        }
      : {}),
    ...(w.Revisions !== null
      ? {
          revisions: w.Revisions.map((r) => ({
            rev: r.Rev,
            at: r.At,
            by: r.By,
            byActivity: r.ByActivity,
            summary: r.Summary,
          })),
        }
      : {}),
  };
}

function mapEvPoint(w: Schemas['SystemDesignEvPoint']): EvPoint {
  return {
    week: w.week,
    earnedPct: w.earnedPct,
    plannedPct: w.plannedPct,
    note: w.note,
    ...(w.acPct !== undefined ? { acPct: w.acPct } : {}),
  };
}

function mapConstructionProgress(
  w: Schemas['SystemDesignConstructionProgress']
): ConstructionProgress {
  // Go nil slices serialize as JSON `null` (not omitted), so guard null too.
  const points = w.points ?? undefined;
  return {
    week: w.Week,
    totalWeeks: w.TotalWeeks,
    handOffModel: w.HandOffModel,
    supervisionCap: w.SupervisionCap,
    ev: {
      weeks: w.EV.weeks ?? [],
      earned: w.EV.earned ?? [],
      planned: w.EV.planned ?? [],
      spi: w.EV.spi,
    },
    ...(points !== undefined && points.length > 0 ? { points: points.map(mapEvPoint) } : {}),
  };
}

function mapRecord<W, A>(
  m: Record<string, W> | null | undefined,
  f: (w: W) => A
): Record<string, A> | undefined {
  // Go nil maps serialize as JSON `null` (not omitted), so guard null too
  // (mirrors the findings/failureReason null-handling elsewhere in this file).
  if (m === undefined || m === null) return undefined;
  const keys = Object.keys(m);
  if (keys.length === 0) return undefined;
  const out: Record<string, A> = {};
  for (const k of keys) out[k] = f(m[k] as W);
  return out;
}

export function mapProjectState(w: Schemas['SystemDesignProjectState']): ProjectStateWithGit {
  const base: ProjectState = {
    projectId: w.ProjectID,
    name: w.Name,
    owner: w.Owner,
    phase: projectPhaseFromOrdinal(w.Phase),
    version: w.Version,
    research: mapResearchInput(w.Research),
    slots: (w.Slots ?? []).map(mapSlot),
  };
  const gitRows = mapRecord<Schemas['SystemDesignActivityGitStatus'], GitRow>(
    w.GitRows,
    mapGitRow
  ) as GitRows | undefined;
  const constructionRows = mapRecord<
    Schemas['SystemDesignActivityConstructionStatus'],
    ConstructionRow
  >(w.ActivityConstruction, mapConstructionRow) as ConstructionRows | undefined;
  const serviceContracts = mapRecord<Schemas['SystemDesignServiceContract'], ServiceContract>(
    w.ServiceContracts,
    mapServiceContract
  ) as ServiceContracts | undefined;
  // Operating (Task 14, finish-construction): the ONE deriveOperating call site,
  // fed the raw {Phase, BuildStatus} ordinals off the wire record BEFORE
  // mapConstructionRow collapses them into the display-oriented ConstructionRow
  // shape (which drops the coarse Phase ordinal entirely). Every render site
  // (catalog chip via ProjectSummary.ConstructionComplete, AppShell chip, HomeBase
  // construction card, the Tracker completion panel, the Console begin/resume
  // button) consumes this ONE precomputed boolean rather than re-deriving it.
  // Routed through mapRecord (rather than a bare `w.ActivityConstruction ?? {}`)
  // for its null-guard: schema.ts's generated type omits `| null`, but Go nil
  // maps serialize as JSON `null` on the wire — the same drift mapRecord's own
  // doc comment above notes and guards for every other head-state map.
  const operatingRows = mapRecord(w.ActivityConstruction, (cs) => ({
    phase: cs.Phase,
    buildStatus: cs.BuildStatus,
  }));
  const operating = deriveOperating(Object.values(operatingRows ?? {}), base.phase);
  return {
    ...base,
    ...(gitRows !== undefined ? { gitRows } : {}),
    ...(constructionRows !== undefined ? { constructionRows } : {}),
    ...(serviceContracts !== undefined ? { serviceContracts } : {}),
    ...(w.constructionProgress !== undefined
      ? { constructionProgress: mapConstructionProgress(w.constructionProgress) }
      : {}),
    ...(w.reviewPolicy !== undefined ? { reviewPolicy: w.reviewPolicy } : {}),
    ...(w.testingState !== undefined ? { testingState: w.testingState } : {}),
    ...(operating ? { operating: true } : {}),
  };
}

// --- system-design session -------------------------------------------------

/**
 * The surfaced PM-critique conclusion (F-QA2-7). An unknown wire verdict (a
 * future server) is dropped entirely — rendering a made-up verdict badge would
 * be dishonest, and absence already means "no PM conclusion to show".
 */
function mapCritique(
  w: Schemas['SystemDesignCritiqueView'] | null | undefined
): PmCritiqueView | undefined {
  if (w === undefined || w === null) return undefined;
  if (w.verdict !== 'approve' && w.verdict !== 'revise') return undefined;
  return { role: w.role, verdict: w.verdict, summary: w.summary, round: w.round };
}

export function mapSessionState(w: Schemas['SystemDesignSessionStateView']): SessionStateResponse {
  const artifactKind = systemArtifactKindFromOrdinal(w.artifactKind);
  const critique = mapCritique(w.critique);
  return {
    projectId: w.projectId,
    artifactKind,
    stage: sessionStageFromOrdinal(w.stage),
    view: {
      projectId: w.projectId,
      artifactKind,
      stage: w.stage,
      activeRole: activeRoleFromOrdinal(w.activeRole),
      activeStep: activeStepFromOrdinal(w.activeStep),
      round: w.round,
      draft: mapEnvelope(w.draft),
      ...(w.findings !== undefined && w.findings !== null
        ? { findings: w.findings.map(mapFinding) }
        : {}),
      ...(w.failureReason !== undefined && w.failureReason !== null
        ? { failureReason: w.failureReason }
        : {}),
      ...(w.failureRunUrl !== undefined && w.failureRunUrl !== null
        ? { failureRunUrl: w.failureRunUrl }
        : {}),
      ...(w.runUrl !== undefined && w.runUrl !== null ? { runUrl: w.runUrl } : {}),
      ...(w.reviewThread !== undefined && w.reviewThread !== null
        ? { reviewThread: w.reviewThread.map(mapReviewComment) }
        : {}),
      ...(critique !== undefined ? { critique } : {}),
    },
  };
}

// --- project-design session ------------------------------------------------

export function mapProjectSessionState(
  w: Schemas['ProjectDesignSessionStateView']
): ProjectSessionState {
  const artifactKind = projectArtifactKindFromOrdinal(w.artifactKind);
  return {
    projectId: w.projectId,
    artifactKind,
    stage: projectSessionStageFromOrdinal(w.stage),
    view: {
      projectId: w.projectId,
      artifactKind,
      stage: w.stage,
      activeRole: activeRoleFromOrdinal(w.activeRole),
      activeStep: activeStepFromOrdinal(w.activeStep),
      round: w.round,
      draft: mapProjectEnvelope(w.draft),
      ...(w.findings !== undefined && w.findings !== null
        ? { findings: w.findings.map(mapFinding) }
        : {}),
      ...(w.failureReason !== undefined && w.failureReason !== null
        ? { failureReason: w.failureReason }
        : {}),
      ...(w.reviewThread !== undefined && w.reviewThread !== null
        ? { reviewThread: w.reviewThread.map(mapReviewComment) }
        : {}),
    },
  };
}

// --- construction session --------------------------------------------------

export function mapConstructionSession(
  w: Schemas['ConstructionConstructionSessionView']
): ConstructionSessionState {
  return {
    projectId: w.projectId,
    ...(w.activityId !== undefined ? { activityId: w.activityId } : {}),
    stage: constructionStageFromOrdinal(w.stage),
    ...(w.pipelinePhase !== undefined
      ? { pipelinePhase: pipelinePhaseFromOrdinal(w.pipelinePhase) }
      : {}),
    view: {
      projectId: w.projectId,
      ...(w.activityId !== undefined ? { activityId: w.activityId } : {}),
      stage: w.stage,
      ...(w.pipelinePhase !== undefined ? { pipelinePhase: w.pipelinePhase } : {}),
      ...(w.reviewSet !== undefined
        ? {
            reviewSet: {
              ...(w.reviewSet.reviewers !== undefined && w.reviewSet.reviewers !== null
                ? {
                    reviewers: w.reviewSet.reviewers.map((r) => ({
                      role: r.role,
                      perspective: r.perspective,
                      ...(r.referenceArtifact !== undefined && r.referenceArtifact !== null
                        ? { referenceArtifact: r.referenceArtifact }
                        : {}),
                      mayAmend: r.mayAmend,
                    })),
                  }
                : {}),
            },
          }
        : {}),
      ...(w.variance !== undefined
        ? {
            variance: {
              projectId: w.variance.projectId,
              activityId: w.variance.activityId,
              summary: w.variance.summary,
            },
          }
        : {}),
    },
  };
}

// --- operations ------------------------------------------------------------

export function mapOperationsView(w: Schemas['OperationsOperatedSystemView']): OperationsView {
  return {
    operatedAppId: w.OperatedAppID,
    phase: runtimePhaseFromOrdinal(w.Phase),
    inFlight: w.InFlight,
    health: {
      sloMet: w.Health.SloMet,
      detail: w.Health.Detail,
      phase: runtimePhaseFromOrdinal(w.Health.Phase),
    },
    slos: (w.Slos ?? []).map((s) => ({
      component: s.Component,
      objective: s.Objective,
      sloMet: s.SloMet,
      healthy: s.Healthy,
    })),
    recentEvents: (w.RecentEvents ?? []).map((e) => ({
      at: e.At,
      from: runtimePhaseFromOrdinal(e.From),
      to: runtimePhaseFromOrdinal(e.To),
      note: e.Note,
    })),
    autoscaler: {
      mode: autoscalerModeFromOrdinal(w.Autoscaler.Mode),
      decisions: (w.Autoscaler.Decisions ?? []).map((d) => ({
        at: d.At,
        action: autoscaleActionFromOrdinal(d.Action),
        reason: d.Reason,
        published: d.Published,
      })),
    },
    currentRunRate: mapMoney(w.CurrentRunRate),
  };
}

export function mapCostProjection(
  operatedAppId: string,
  w: Schemas['OperationsCostProjectionSeam']
): CostProjection {
  return {
    operatedAppId,
    currentRunRate: mapMoney(w.CurrentRunRate),
    projectedMonthlyCost: mapMoney(w.ProjectedMonthlyCost),
    scaleWhatIfCurve: (w.ScaleWhatIfCurve.Points ?? []).map((p) => ({
      replicas: p.Replicas,
      projectedMonthlyCost: mapMoney(p.ProjectedMonthlyCost),
    })),
  };
}

// --- episodes (SP1 capture seam) --------------------------------------------
//
// ConstructionEpisodeRecordView / ProjectDesignEpisodeRecordView /
// SystemDesignEpisodeRecordView (and their Usage/Lineage/SubagentSpan/
// TimelineEvent/Timeline siblings) are byte-identical across the three manager
// namespaces (verified against the OAS at Task 10 authorship time) — one set of
// mappers serves all three via a union parameter, same pattern as mapFinding /
// mapReviewComment above.

function episodeKindFromOrdinal(ordinal: number): EpisodeKind {
  return EPISODE_KIND_ORDINAL_TO_APP[ordinal] ?? 'design';
}

function episodeOutcomeFromOrdinal(ordinal: number): EpisodeOutcome {
  return EPISODE_OUTCOME_ORDINAL_TO_APP[ordinal] ?? 'gap';
}

function mapEpisodeUsage(
  w:
    | Schemas['ConstructionEpisodeUsage']
    | Schemas['ProjectDesignEpisodeUsage']
    | Schemas['SystemDesignEpisodeUsage']
): EpisodeUsage {
  return { in: w.in, out: w.out, cacheRead: w.cacheRead, cacheCreate: w.cacheCreate };
}

function mapEpisodeLineage(
  w:
    | Schemas['ConstructionEpisodeLineage']
    | Schemas['ProjectDesignEpisodeLineage']
    | Schemas['SystemDesignEpisodeLineage']
): EpisodeLineage {
  return {
    workflowId: w.workflowId,
    runId: w.runId,
    ...(w.activityId !== undefined ? { activityId: w.activityId } : {}),
  };
}

function mapSubagentSpan(
  w:
    | Schemas['ConstructionSubagentSpan']
    | Schemas['ProjectDesignSubagentSpan']
    | Schemas['SystemDesignSubagentSpan']
): SubagentSpan {
  return {
    toolUseId: w.toolUseId,
    ...(w.startedAt !== undefined ? { startedAt: w.startedAt } : {}),
    ...(w.endedAt !== undefined ? { endedAt: w.endedAt } : {}),
  };
}

export function mapEpisodeRecordView(
  w:
    | Schemas['ConstructionEpisodeRecordView']
    | Schemas['ProjectDesignEpisodeRecordView']
    | Schemas['SystemDesignEpisodeRecordView']
): EpisodeRecordView {
  return {
    episodeId: w.episodeId,
    kind: episodeKindFromOrdinal(w.kind),
    targetRef: w.targetRef,
    outcome: episodeOutcomeFromOrdinal(w.outcome),
    usage: mapEpisodeUsage(w.usage),
    startedAt: w.startedAt,
    endedAt: w.endedAt,
    ...(w.lineage !== undefined ? { lineage: mapEpisodeLineage(w.lineage) } : {}),
    ...(w.gapReason !== undefined ? { gapReason: w.gapReason } : {}),
    ...(w.model !== undefined ? { model: w.model } : {}),
    ...(w.workerClass !== undefined ? { workerClass: w.workerClass } : {}),
    ...(w.streamedUsage !== undefined ? { streamedUsage: mapEpisodeUsage(w.streamedUsage) } : {}),
    ...(w.toolCallCounts !== undefined ? { toolCallCounts: w.toolCallCounts } : {}),
    ...(w.subagentSpans !== undefined
      ? { subagentSpans: w.subagentSpans.map(mapSubagentSpan) }
      : {}),
    ...(w.numTurns !== undefined ? { numTurns: w.numTurns } : {}),
    ...(w.costUsd !== undefined ? { costUsd: w.costUsd } : {}),
    ...(w.tracePath !== undefined ? { tracePath: w.tracePath } : {}),
  };
}

/**
 * TimelineEvent.raw is server-typed as `*json.RawMessage` (Go) — `json.RawMessage`
 * has no OAS shape, so the generator emits `type: null` for it and schema.ts's
 * `raw` field is typed `null` only. CORRECTED 2026-08-02 review (C1): the field is
 * NOT a JSON-encoded string at runtime. `*json.RawMessage.MarshalJSON` writes its
 * bytes VERBATIM into the parent JSON, so when the server's own encoding/json
 * marshals a TimelineEvent, `raw` embeds as a nested JSON OBJECT —
 * `{"seq":1,"eventType":"assistant","raw":{"type":"assistant","message":{...}}}` —
 * never as an escaped string. `omitempty` on the pointer means the key is simply
 * ABSENT (not present-as-null) when there is no payload. See
 * server/internal/manager/construction/constructionmanager.go's
 * `episodeTimelineEvents` (Raw: &raw[i], one full stream-json trace line per
 * event) and contract.gen.go's `Raw *json.RawMessage \`json:"raw,omitempty"\`` for
 * the server-side ground truth, and utilities/episodeRawEvent.ts's
 * `parseRawEvent` (which accepts EITHER an object or, defensively, a string) for
 * the consumer side.
 *
 * The app type widens `raw` to `unknown` (no assumed shape at all — see
 * contracts/types.ts) rather than encoding a specific runtime type here, since
 * the honest wire type is "whatever JSON value the pointer held", not a string.
 */
function mapTimelineEvent(
  w:
    | Schemas['ConstructionTimelineEvent']
    | Schemas['ProjectDesignTimelineEvent']
    | Schemas['SystemDesignTimelineEvent']
): TimelineEvent {
  // `w.raw`'s STATIC type is `null | undefined` (schema.ts's placeholder for the
  // unrepresentable json.RawMessage — see the doc comment above), but its REAL
  // runtime value is an arbitrary JSON value whenever present. Explicitly
  // annotating the local as `unknown` erases that too-narrow static type for
  // every downstream read, rather than lying with a same-shape `as` cast.
  const raw: unknown = w.raw;
  return {
    seq: w.seq,
    eventType: w.eventType,
    ...(raw !== undefined && raw !== null ? { raw } : {}),
  };
}

export function mapEpisodeTimeline(
  w:
    | Schemas['ConstructionEpisodeTimeline']
    | Schemas['ProjectDesignEpisodeTimeline']
    | Schemas['SystemDesignEpisodeTimeline']
): EpisodeTimeline {
  return { record: mapEpisodeRecordView(w.record), events: w.events.map(mapTimelineEvent) };
}

// --- app → wire ------------------------------------------------------------

export function toResearchInputWire(app: ResearchInput): Schemas['SystemDesignResearchInput'] {
  return { sources: app.sources.map((s) => ({ title: s.title, content: s.content })) };
}

// --- app → wire: ordinal encoders (mechanical — sourced from enums.gen.ts) --

export function artifactKindToOrdinal(kind: ArtifactKindFull): Schemas['SystemDesignArtifactKind'] {
  return ARTIFACT_KIND_APP_TO_ORDINAL[kind] as Schemas['SystemDesignArtifactKind'];
}

export function reviewDecisionToOrdinal(
  decision: ReviewDecision
): Schemas['SystemDesignReviewDecision'] {
  return REVIEW_DECISION_APP_TO_ORDINAL[decision] as Schemas['SystemDesignReviewDecision'];
}

export function sdpDecisionToOrdinal(decision: SDPDecision): Schemas['ProjectDesignSDPDecision'] {
  return SDP_DECISION_APP_TO_ORDINAL[decision] as Schemas['ProjectDesignSDPDecision'];
}

export function overrideKindToOrdinal(kind: OverrideKind): Schemas['ConstructionOverrideKind'] {
  return OVERRIDE_KIND_APP_TO_ORDINAL[kind] as Schemas['ConstructionOverrideKind'];
}

export function phaseDecisionToOrdinal(
  decision: PhaseDecision
): Schemas['ConstructionPhaseDecision'] {
  return PHASE_DECISION_APP_TO_ORDINAL[decision] as Schemas['ConstructionPhaseDecision'];
}

/** OperationsDesiredStateReason ordinals. */
export const REASON_DEPLOY_AFTER_CONSTRUCTION =
  DESIRED_STATE_REASON_APP_TO_ORDINAL.deployAfterConstruction as Schemas['OperationsDesiredStateReason'];
export const REASON_OPERATOR =
  DESIRED_STATE_REASON_APP_TO_ORDINAL.operator as Schemas['OperationsDesiredStateReason'];

/** OperationsPatchKind ordinals. */
export const PATCH_FULL_BUNDLE =
  PATCH_KIND_APP_TO_ORDINAL.fullBundle as Schemas['OperationsPatchKind'];
export const PATCH_SCALE = PATCH_KIND_APP_TO_ORDINAL.scale as Schemas['OperationsPatchKind'];
export const PATCH_POLICY = PATCH_KIND_APP_TO_ORDINAL.policy as Schemas['OperationsPatchKind'];
