/**
 * UC3 construction (Phase-3 superviseConstruction) API service — one typed
 * function per server op over the project-scoped nested routes
 * (server/internal/client/web/construction.go + handlers.go). projectId is always
 * a PATH param. The Phase-3 TWIN of api/projectDesign.ts.
 *
 * Unlike Phase-1/2, the construction routes are NOT in the generated OpenAPI
 * schema (the codegen spec stops at advanceToConstruction). So these functions
 * issue same-origin fetches directly against config.apiBaseUrl and hand-mirror the
 * Go wire DTOs below — the field names match construction.go / contract.go EXACTLY.
 *
 * IMPORTANT runtime reality: the construction pump that populates sessions is
 * gated on a live Argo cluster (R-CPR) that is not provisioned yet, so in practice
 * GetSessionState often returns a quiet/awaiting view (stage dispatching, no
 * pipeline, no reviewSet, no variance) — that is expected, not an error, and the
 * console renders it as a graceful awaiting state.
 */
import { ApiError } from './client';
import { config } from '../config';

// --- wire DTOs (hand-mirrored from construction.go / contract.go) ----------

/** The technical construction-session progress stage (contract.go ConstructionStage,
 * rendered onto the wire by constructionStageName in construction.go). */
export type ConstructionStage =
  | 'dispatching'
  | 'pipelineRunning'
  | 'reviewing'
  | 'awaitingTakeover'
  | 'paused'
  | 'exited'
  | 'unknown';

/** The construction-pipeline phase (contract.go PipelinePhase / constructionPipelinePhaseName). */
export type PipelinePhase = 'pending' | 'running' | 'succeeded' | 'failed' | 'unknown';

/** One reviewer assignment within the active change's ReviewSet (deps.go Reviewer). */
export interface ConstructionReviewer {
  role: string;
  perspective: string;
  referenceArtifact?: string;
  mayAmend: boolean;
}

/** The computed reviewer set for the active change (deps.go ReviewSet). */
export interface ConstructionReviewSet {
  reviewers?: ConstructionReviewer[];
}

/** One over-threshold variance surfaced to the operator (contract.go FlaggedVariance). */
export interface FlaggedVariance {
  projectId: string;
  activityId: string;
  summary: string;
}

/** The typed ConstructionSessionView (contract.go §3.4) — the read-only technical view. */
export interface ConstructionSessionView {
  projectId: string;
  activityId?: string;
  /** Integer ConstructionStage ordinal on the inner view (the outer response carries the string). */
  stage: number;
  pipelinePhase?: number;
  reviewSet?: ConstructionReviewSet;
  variance?: FlaggedVariance;
}

/** The construction-session response (construction.go constructionSessionStateResponse). */
export interface ConstructionSessionState {
  projectId: string;
  activityId?: string;
  /** The string-rendered stage the SPA polls + renders on (outer response). */
  stage: ConstructionStage;
  pipelinePhase?: PipelinePhase;
  view: ConstructionSessionView;
}

/** The closed set of operator override steers (contract.go OverrideKind). */
export type OverrideKind = 'takeover' | 'retry' | 'skip' | 'reassign';

// --- raw transport ---------------------------------------------------------

interface WireErrorResponse {
  error?: string;
  code?: string;
}

async function readError(response: Response): Promise<ApiError> {
  let body: WireErrorResponse | undefined;
  try {
    body = (await response.json()) as WireErrorResponse;
  } catch {
    body = undefined;
  }
  const code = body?.code ?? 'internal';
  const detail = body?.error ?? `request failed with status ${String(response.status)}`;
  return new ApiError(response.status, code, detail);
}

function url(path: string): string {
  return `${config.apiBaseUrl}${path}`;
}

// --- ops -------------------------------------------------------------------

/**
 * GET /construction/session — poll the construction session's technical view
 * (superviseConstruction{GetSessionState}). The optional activityId selects the
 * per-activity child session; absent it returns the project-level supervision
 * view. A 404 means "no session started yet" (the pump is dormant) — callers
 * surface it as an awaiting state, not an error.
 */
export async function getConstructionSession(
  projectId: string,
  activityId?: string
): Promise<ConstructionSessionState> {
  const query = activityId !== undefined && activityId.length > 0
    ? `?activityId=${encodeURIComponent(activityId)}`
    : '';
  const response = await fetch(
    url(`/api/v1/projects/${encodeURIComponent(projectId)}/construction/session${query}`),
    { credentials: 'include', headers: { Accept: 'application/json' } }
  );
  if (!response.ok) throw await readError(response);
  return (await response.json()) as ConstructionSessionState;
}

/**
 * POST /construction/begin — start the construction pump ("Begin construction").
 * The pump self-cascades over the committed network (server-side); this returns
 * 202 Accepted as soon as the tick is accepted (the cascade drains asynchronously —
 * the SPA polls the project read to watch the tracker animate).
 */
export async function beginConstruction(projectId: string): Promise<void> {
  const response = await fetch(
    url(`/api/v1/projects/${encodeURIComponent(projectId)}/construction/begin`),
    {
      method: 'POST',
      credentials: 'include',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({}),
    }
  );
  if (!response.ok) throw await readError(response);
}

/**
 * POST /construction/pause — operator pause (superviseConstruction{PauseProject}).
 * Reason is required (the Manager rejects an empty reason). 204 No Content.
 */
export async function pauseConstruction(projectId: string, reason: string): Promise<void> {
  const response = await fetch(
    url(`/api/v1/projects/${encodeURIComponent(projectId)}/construction/pause`),
    {
      method: 'POST',
      credentials: 'include',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ reason }),
    }
  );
  if (!response.ok) throw await readError(response);
}

/**
 * POST /construction/activities/{activityId}/override — operator override
 * (superviseConstruction{OverrideActivity}). Kind is one of the four steers;
 * notes is free-text. 204 No Content.
 */
export async function overrideActivity(
  projectId: string,
  activityId: string,
  kind: OverrideKind,
  notes?: string
): Promise<void> {
  const response = await fetch(
    url(
      `/api/v1/projects/${encodeURIComponent(projectId)}/construction/activities/${encodeURIComponent(activityId)}/override`
    ),
    {
      method: 'POST',
      credentials: 'include',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ kind, ...(notes !== undefined ? { notes } : {}) }),
    }
  );
  if (!response.ok) throw await readError(response);
}
