/**
 * UC1 system-design (Phase-1) API service — one typed function per server op over
 * the project-scoped nested routes. projectId is always a PATH param (never a body
 * field). Each call returns parsed data or throws an ApiError. No React / caching
 * here — TanStack Query owns that in the hooks layer.
 */
import { apiClient, toApiError } from './client';
import type {
  AnchoredComment,
  ArtifactKind,
  PhaseAdvanceResponse,
  ResearchInput,
  ReviewDecision,
  SessionStateResponse,
} from './types';

/** Optional rationale woven into a reject/withdraw decision. */
export interface ReviewDecisionDetail {
  /** Required by the Manager on "reject"; optional on "withdraw"; ignored on "approve". */
  readonly feedback?: string;
  /** JSONPath-anchored "send back" comments; consulted only on "reject". */
  readonly comments?: AnchoredComment[];
}

/**
 * POST /system-design/research-input — record the Phase-1 research corpus so the
 * project satisfies startSystemDesign's ResearchInput-present precondition. This
 * is the step BEFORE start. 204 No Content on success (no body).
 */
export async function setResearchInput(projectId: string, research: ResearchInput): Promise<void> {
  const { error, response } = await apiClient.POST(
    '/api/v1/projects/{projectId}/system-design/research-input',
    { params: { path: { projectId } }, body: { research } }
  );
  if (error !== undefined) throw toApiError(response.status, error);
}

/** POST /system-design/start — begin the Phase-1 parent workflow. */
export async function startSystemDesign(projectId: string): Promise<string> {
  const { data, error, response } = await apiClient.POST(
    '/api/v1/projects/{projectId}/system-design/start',
    { params: { path: { projectId } } }
  );
  if (error !== undefined) throw toApiError(response.status, error);
  return data.sessionRef;
}

/** POST /system-design/artifacts/draft — request/continue a co-author gate. */
export async function requestArtifactDraft(
  projectId: string,
  artifactKind: ArtifactKind,
  feedback?: string
): Promise<string> {
  const { data, error, response } = await apiClient.POST(
    '/api/v1/projects/{projectId}/system-design/artifacts/draft',
    {
      params: { path: { projectId } },
      body: { artifactKind, ...(feedback !== undefined ? { feedback } : {}) },
    }
  );
  if (error !== undefined) throw toApiError(response.status, error);
  return data.sessionRef;
}

/** POST /system-design/artifacts/review — deliver the architect's gate decision. */
export async function submitReviewDecision(
  projectId: string,
  artifactKind: ArtifactKind,
  decision: ReviewDecision,
  detail: ReviewDecisionDetail = {}
): Promise<void> {
  const { error, response } = await apiClient.POST(
    '/api/v1/projects/{projectId}/system-design/artifacts/review',
    {
      params: { path: { projectId } },
      body: {
        artifactKind,
        decision,
        ...(detail.feedback !== undefined ? { feedback: detail.feedback } : {}),
        ...(detail.comments !== undefined ? { comments: detail.comments } : {}),
      },
    }
  );
  if (error !== undefined) throw toApiError(response.status, error);
}

/** POST /system-design/advance — seal Phase 1 (gating workflow). */
export async function advancePhase(projectId: string): Promise<PhaseAdvanceResponse> {
  const { data, error, response } = await apiClient.POST(
    '/api/v1/projects/{projectId}/system-design/advance',
    { params: { path: { projectId } } }
  );
  if (error !== undefined) throw toApiError(response.status, error);
  return data;
}

/** GET /system-design/sessions/{artifactKind} — poll one session's state. */
export async function getSessionState(
  projectId: string,
  artifactKind: ArtifactKind
): Promise<SessionStateResponse> {
  const { data, error, response } = await apiClient.GET(
    '/api/v1/projects/{projectId}/system-design/sessions/{artifactKind}',
    { params: { path: { projectId, artifactKind } } }
  );
  if (error !== undefined) throw toApiError(response.status, error);
  return data;
}
