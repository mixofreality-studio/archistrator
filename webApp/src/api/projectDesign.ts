/**
 * UC2 project-design (Phase-2) API service — one typed function per server op over
 * the project-scoped nested routes (server/internal/client/web/projectdesign.go +
 * handlers.go). projectId is always a PATH param. The Phase-2 TWIN of
 * api/systemDesign.ts. No React / caching here — TanStack Query owns that in the
 * hooks layer.
 *
 * The generated schema leaves `ProjectSessionStateResponse.view` opaque
 * (Record<string, never>) — the Phase-2 model JSON is fully typed on the wire but
 * not enumerated in the OpenAPI spec. getProjectSessionState therefore decodes the
 * opaque view into the hand-mirrored ProjectSessionState (api/types.ts).
 */
import { apiClient, toApiError } from './client';
import type {
  ProjectArtifactKind,
  ProjectSessionState,
  ProjectSessionStateView,
  ProjectPhaseAdvanceResponse,
  ReviewDecision,
  SDPDecision,
} from './types';

/**
 * POST /project-design/artifacts/draft — request/continue a Phase-2 co-author gate
 * for one of the eight DRAFTABLE kinds (NOT sdpReview). Feedback is woven into the
 * next draft on a re-entry. 202 Accepted with the continuity sessionRef.
 */
export async function requestProjectArtifactDraft(
  projectId: string,
  artifactKind: ProjectArtifactKind,
  feedback?: string
): Promise<string> {
  const { data, error, response } = await apiClient.POST(
    '/api/v1/projects/{projectId}/project-design/artifacts/draft',
    {
      params: { path: { projectId } },
      body: { artifactKind, ...(feedback !== undefined ? { feedback } : {}) },
    }
  );
  if (error !== undefined) throw toApiError(response.status, error);
  return data.sessionRef;
}

/** POST /project-design/artifacts/review — deliver the architect's per-artifact gate decision. */
export async function submitProjectReviewDecision(
  projectId: string,
  artifactKind: ProjectArtifactKind,
  decision: ReviewDecision,
  feedback?: string
): Promise<void> {
  const { error, response } = await apiClient.POST(
    '/api/v1/projects/{projectId}/project-design/artifacts/review',
    {
      params: { path: { projectId } },
      body: { artifactKind, decision, ...(feedback !== undefined ? { feedback } : {}) },
    }
  );
  if (error !== undefined) throw toApiError(response.status, error);
}

/**
 * POST /project-design/sdp/assemble — assemble the SDP review (the UC2 spine
 * workflow that joins the three estimate Engines into the options table). 202
 * Accepted with the SDP-review sessionRef. Poll the `sdpReview` session afterwards.
 */
export async function requestSDPCommit(projectId: string): Promise<string> {
  const { data, error, response } = await apiClient.POST(
    '/api/v1/projects/{projectId}/project-design/sdp/assemble',
    { params: { path: { projectId } } }
  );
  if (error !== undefined) throw toApiError(response.status, error);
  return data.sessionRef;
}

/** Optional rationale woven into an SDP decision. */
export interface SDPDecisionDetail {
  /** Required by the Manager on "commit" — names the chosen option. */
  readonly optionId?: string;
  /** Required by the Manager on "rejectAll". */
  readonly feedback?: string;
}

/**
 * POST /project-design/sdp/decision — deliver the architect's option-commitment
 * decision. commit binds the named option + advances; rejectAll re-enters Phase 2
 * with feedback. 204 No Content once the signal is durably enqueued.
 */
export async function submitSDPDecision(
  projectId: string,
  decision: SDPDecision,
  detail: SDPDecisionDetail = {}
): Promise<void> {
  const { error, response } = await apiClient.POST(
    '/api/v1/projects/{projectId}/project-design/sdp/decision',
    {
      params: { path: { projectId } },
      body: {
        decision,
        ...(detail.optionId !== undefined ? { optionId: detail.optionId } : {}),
        ...(detail.feedback !== undefined ? { feedback: detail.feedback } : {}),
      },
    }
  );
  if (error !== undefined) throw toApiError(response.status, error);
}

/**
 * POST /project-design/advance — seal Phase 2 (gating workflow). A non-Advanced
 * result is the NORMAL "you still owe artifacts X, Y / no option bound" answer
 * (HTTP 200), not an error.
 */
export async function advanceToConstruction(
  projectId: string
): Promise<ProjectPhaseAdvanceResponse> {
  const { data, error, response } = await apiClient.POST(
    '/api/v1/projects/{projectId}/project-design/advance',
    { params: { path: { projectId } } }
  );
  if (error !== undefined) throw toApiError(response.status, error);
  return data;
}

/**
 * GET /project-design/sessions/{artifactKind} — poll one Phase-2 session's state.
 * artifactKind may be any Phase-2 kind, including "sdpReview". The opaque `view` is
 * decoded into the typed ProjectSessionStateView here (see api/types.ts).
 */
export async function getProjectSessionState(
  projectId: string,
  artifactKind: ProjectArtifactKind
): Promise<ProjectSessionState> {
  const { data, error, response } = await apiClient.GET(
    '/api/v1/projects/{projectId}/project-design/sessions/{artifactKind}',
    { params: { path: { projectId, artifactKind } } }
  );
  if (error !== undefined) throw toApiError(response.status, error);
  // The generated schema types `view` opaquely (Record<string, never>); the Go
  // wire form is fully typed. Narrow through unknown to the hand-mirrored view.
  return {
    projectId: data.projectId,
    artifactKind: data.artifactKind,
    stage: data.stage,
    view: data.view as unknown as ProjectSessionStateView,
  };
}
