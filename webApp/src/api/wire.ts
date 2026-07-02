/**
 * Wire → app mapping at the generated-client boundary.
 *
 * The openapi-fetch client returns the generated (per-manager namespaced,
 * PascalCase, integer-enum) wire types. Every API hook funnels its decoded `data`
 * through these pure mappers to produce the SPA's stable app view types (camelCase,
 * lowerCamel string enums). Opaque `model` payloads are decoded through `unknown`
 * into the ./models decode types — the OAS leaves them untyped by design.
 */
import type { components } from './schema';
import {
  systemArtifactKindFromOrdinal,
  projectArtifactKindFromOrdinal,
  sessionStageFromOrdinal,
  projectSessionStageFromOrdinal,
  projectPhaseFromOrdinal,
  constructionStageFromOrdinal,
  pipelinePhaseFromOrdinal,
  ciStatusFromOrdinal,
  activityRowKindFromOrdinal,
  buildStatusRowFromOrdinal,
  runtimePhaseFromOrdinal,
  autoscalerModeFromOrdinal,
  autoscaleActionFromOrdinal,
} from './enums';
import type {
  ArtifactKindFull,
  ArtifactSlotView,
  ConstructionProgress,
  ConstructionRow,
  ConstructionSessionState,
  Finding,
  GitRow,
  GitRows,
  ProducedArtifactRow,
  ProjectArtifactKind,
  ProjectSessionState,
  ProjectState,
  ProjectStateWithGit,
  ProjectSummary,
  ResearchInput,
  ServiceContract,
  ServiceContracts,
  SessionStateResponse,
  ConstructionRows,
} from './types';
import type { ArtifactModelEnvelope, Money, ProjectArtifactModelEnvelope } from './models';
import type { CostProjection, OperationsView } from './operationsTypes';

type Schemas = components['schemas'];

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

/** Decode the opaque {kind, model} envelope into the typed Phase-1+2 envelope. */
function mapEnvelope(w: { kind: string; model?: unknown }): ArtifactModelEnvelope {
  const env: ArtifactModelEnvelope = { kind: w.kind as ArtifactKindFull };
  if (w.model !== undefined && w.model !== null) {
    env.model = w.model as NonNullable<ArtifactModelEnvelope['model']>;
  }
  return env;
}

/** Decode the opaque {kind, model} envelope into the typed Phase-2 envelope. */
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
  };
}

function mapResearchInput(w: Schemas['SystemDesignResearchInput']): ResearchInput {
  return { sources: (w.sources ?? []).map((s) => ({ title: s.title, content: s.content })) };
}

function mapSlot(w: Schemas['SystemDesignArtifactSlotView']): ArtifactSlotView {
  return {
    kind: w.kind as ArtifactKindFull,
    stage: w.stage,
    model: mapEnvelope(w.model),
    ...(w.notes !== undefined && w.notes !== null ? { notes: w.notes } : {}),
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
  return {
    activityId: w.ActivityID,
    kind: activityRowKindFromOrdinal(w.Kind),
    status: buildStatusRowFromOrdinal(w.BuildStatus),
    phase: w.CurrentPhase,
    ...(w.Produced !== null ? { produced: w.Produced.map(mapProducedArtifact) } : {}),
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
                    fields: (s.Fields ?? []).map((f) => ({ name: f.Name, type: f.Type, note: f.Note })),
                  })),
                }
              : {}),
            ...(o.Outputs !== null
              ? {
                  outputs: o.Outputs.map((s) => ({
                    name: s.Name,
                    fields: (s.Fields ?? []).map((f) => ({ name: f.Name, type: f.Type, note: f.Note })),
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

function mapConstructionProgress(
  w: Schemas['SystemDesignConstructionProgress']
): ConstructionProgress {
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
  const gitRows = mapRecord<Schemas['SystemDesignActivityGitStatus'], GitRow>(w.GitRows, mapGitRow) as
    | GitRows
    | undefined;
  const constructionRows = mapRecord<
    Schemas['SystemDesignActivityConstructionStatus'],
    ConstructionRow
  >(w.ActivityConstruction, mapConstructionRow) as ConstructionRows | undefined;
  const serviceContracts = mapRecord<Schemas['SystemDesignServiceContract'], ServiceContract>(
    w.ServiceContracts,
    mapServiceContract
  ) as ServiceContracts | undefined;
  return {
    ...base,
    ...(gitRows !== undefined ? { gitRows } : {}),
    ...(constructionRows !== undefined ? { constructionRows } : {}),
    ...(serviceContracts !== undefined ? { serviceContracts } : {}),
    ...(w.constructionProgress !== undefined
      ? { constructionProgress: mapConstructionProgress(w.constructionProgress) }
      : {}),
    ...(w.reviewPolicy !== undefined ? { reviewPolicy: w.reviewPolicy } : {}),
  };
}

// --- system-design session -------------------------------------------------

export function mapSessionState(w: Schemas['SystemDesignSessionStateView']): SessionStateResponse {
  const artifactKind = systemArtifactKindFromOrdinal(w.artifactKind);
  return {
    projectId: w.projectId,
    artifactKind,
    stage: sessionStageFromOrdinal(w.stage),
    view: {
      projectId: w.projectId,
      artifactKind,
      stage: w.stage,
      draft: mapEnvelope(w.draft),
      ...(w.findings !== undefined && w.findings !== null
        ? { findings: w.findings.map(mapFinding) }
        : {}),
      ...(w.failureReason !== undefined && w.failureReason !== null
        ? { failureReason: w.failureReason }
        : {}),
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
      draft: mapProjectEnvelope(w.draft),
      ...(w.findings !== undefined && w.findings !== null
        ? { findings: w.findings.map(mapFinding) }
        : {}),
      ...(w.failureReason !== undefined && w.failureReason !== null
        ? { failureReason: w.failureReason }
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

export function mapOperationsView(
  w: Schemas['OperationsOperatedSystemView']
): OperationsView {
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

// --- app → wire ------------------------------------------------------------

export function toResearchInputWire(app: ResearchInput): Schemas['SystemDesignResearchInput'] {
  return { sources: app.sources.map((s) => ({ title: s.title, content: s.content })) };
}
