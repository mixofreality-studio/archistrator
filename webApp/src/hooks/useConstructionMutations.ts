/**
 * Phase-3 construction supervision mutations: dispatch the next activity (the
 * "Begin construction" tick), pause the project's construction, and override one
 * in-flight activity. Each invalidates the construction-session / project queries
 * so the console re-reads fresh server state (never setQueryData).
 *
 * "Begin construction" now maps onto construction/execute-next-activity: the pump
 * dispatches the next eligible activity. The supplied tickID correlates the request;
 * the server itself runs one pump workflow per project (architect I1 ruling).
 */
import { useMutation, useQueryClient, type UseMutationResult } from '@tanstack/react-query';
import { apiClient } from '../api/client';
import { ApiError, toApiError } from '../contracts/errors';
import { overrideKindToOrdinal, phaseDecisionToOrdinal } from '../contracts/wire';
import type { OverrideKind, PhaseDecision, ReviewPreset } from '../contracts/types';
import type { components } from '../contracts/schema';
import { constructionSessionKey, constructionSessionsKey } from './useConstructionSession';
import { phaseDecisionMutationKey } from './phaseDecisionKey';
import { projectKey } from './useProject';

/**
 * Dispatch the next activity. The caller supplies the tickID, minted ONCE per
 * confirm-dialog opening (fix-A review I1). It is a correlation id, not the guard
 * against a second pump — the server runs one pump workflow per project (architect
 * I1 ruling); the confirm dialog's press-once and the console's in-flight ref are
 * the client's UX debouncing.
 */
export function useBeginConstruction(
  projectId: string
): UseMutationResult<undefined, Error, string> {
  const client = useQueryClient();
  // Refresh the project read so the just-dispatched activity (flipping to
  // in-construction) shows up; the console's cascade poll keeps it fresh. The
  // session probes are re-read too: a dispatch may have just created a session
  // they had settled as absent.
  const refresh = async (): Promise<void> => {
    await Promise.all([
      client.invalidateQueries({ queryKey: projectKey(projectId) }),
      client.invalidateQueries({ queryKey: constructionSessionsKey(projectId) }),
    ]);
  };
  return useMutation<undefined, Error, string>({
    mutationFn: async (tickID) => {
      const { error, response } = await apiClient.POST(
        '/api/v1/construction/execute-next-activity/{projectID}',
        { params: { path: { projectID: projectId } }, body: { tickID } }
      );
      if (error !== undefined) throw toApiError(response.status, error);
      return undefined;
    },
    onSuccess: refresh,
    // A FAILED dispatch refreshes exactly as a successful one does (fix-C review):
    // the server starts the pump before it answers and can still answer 5xx after
    // that, or the response can be dropped on the way. Only a fresh read can say
    // whether construction started, so the console never guesses from the error.
    onError: refresh,
  });
}

export function usePauseConstruction(
  projectId: string
): UseMutationResult<undefined, Error, string> {
  const client = useQueryClient();
  return useMutation<undefined, Error, string>({
    mutationFn: async (reason) => {
      const { error, response } = await apiClient.POST(
        '/api/v1/construction/pause-project/{projectID}',
        { params: { path: { projectID: projectId } }, body: { reason } }
      );
      if (error !== undefined) throw toApiError(response.status, error);
      return undefined;
    },
    onSuccess: () => client.invalidateQueries({ queryKey: ['constructionSession', projectId] }),
  });
}

export interface OverrideActivityVars {
  activityId: string;
  kind: OverrideKind;
  notes?: string;
  /** Anchored comments accumulated for this steer, ride alongside the notes. */
  comments?: components['schemas']['ConstructionAnchoredComment'][];
}

export function useOverrideActivity(
  projectId: string
): UseMutationResult<undefined, Error, OverrideActivityVars> {
  const client = useQueryClient();
  return useMutation<undefined, Error, OverrideActivityVars>({
    mutationFn: async (vars) => {
      const { error, response } = await apiClient.POST(
        '/api/v1/construction/override-activity/{projectID}/{activityID}',
        {
          params: { path: { projectID: projectId, activityID: vars.activityId } },
          body: {
            override: {
              kind: overrideKindToOrdinal(vars.kind),
              notes: vars.notes ?? '',
              ...(vars.comments && vars.comments.length > 0 ? { comments: vars.comments } : {}),
            },
          },
        }
      );
      if (error !== undefined) throw toApiError(response.status, error);
      return undefined;
    },
    onSuccess: (_data, vars) =>
      client.invalidateQueries({
        queryKey: constructionSessionKey(projectId, vars.activityId),
      }),
  });
}

export interface SubmitPhaseDecisionVars {
  activityId: string;
  phase: string;
  decision: PhaseDecision;
  feedback?: components['schemas']['ConstructionReviewFeedback'];
  /** Client bookkeeping, never sent: which gate occurrence this decision answers
   *  and what the console needs to show for it after a remount. The console reads
   *  it back from the mutation cache (tasks/decisionRecords.ts). */
  occurrence?: { key: string; epoch: number; snapshot?: unknown };
}

/**
 * Every phase decision of one project shares this mutation key, so the console
 * reads what is in flight — and what each one answered — from the QueryClient's
 * mutation cache (useMutationState) rather than from component state. A pending
 * decision therefore survives a remount of the console (navigating home and back),
 * and so does its evidence (tasks-lens review C1). The key lives in a pure module so
 * node:test can pin that it is per project (phaseDecisionKey.test.ts).
 */
export { phaseDecisionMutationKey };

/** When the server answered a decision that came back clean. */
export interface PhaseDecisionAnswer {
  answeredAt: number;
}

/** A decision that did not come back clean: the HTTP status where there was one
 *  (absent for a network failure), and when the console learned of it. */
export class PhaseDecisionFailure extends Error {
  readonly status: number | undefined;
  readonly answeredAt: number;

  constructor(cause: unknown, answeredAt: number) {
    super(cause instanceof Error ? cause.message : String(cause));
    this.name = 'PhaseDecisionFailure';
    this.status = cause instanceof ApiError ? cause.status : undefined;
    this.answeredAt = answeredAt;
  }
}

export function useSubmitPhaseDecision(
  projectId: string
): UseMutationResult<PhaseDecisionAnswer, PhaseDecisionFailure, SubmitPhaseDecisionVars> {
  const client = useQueryClient();
  return useMutation<PhaseDecisionAnswer, PhaseDecisionFailure, SubmitPhaseDecisionVars>({
    mutationKey: phaseDecisionMutationKey(projectId),
    mutationFn: async (vars) => {
      try {
        const { error, response } = await apiClient.POST(
          '/api/v1/construction/submit-phase-decision/{projectID}/{activityID}',
          {
            params: { path: { projectID: projectId, activityID: vars.activityId } },
            body: {
              phase: vars.phase,
              decision: phaseDecisionToOrdinal(vars.decision),
              ...(vars.feedback !== undefined ? { feedback: vars.feedback } : {}),
            },
          }
        );
        if (error !== undefined) throw toApiError(response.status, error);
        return { answeredAt: Date.now() };
      } catch (e) {
        throw new PhaseDecisionFailure(e, Date.now());
      }
    },
    onSuccess: (_data, vars) =>
      client.invalidateQueries({
        queryKey: constructionSessionKey(projectId, vars.activityId),
      }),
  });
}

/**
 * Set the project's review-policy PRESET (the sophistication dial: vibes /
 * checkpoints / full) via the construction SetReviewPolicy op. Distinct from
 * useUpdateReviewPolicy below, which replaces the explicit per-type gate map —
 * the two ops own disjoint halves of the same server-side ReviewPolicy.
 * Invalidates the project read so the home page's control reflects the
 * committed value (reviewPolicy.preset), never a local echo.
 */
export function useSetReviewPolicy(
  projectId: string
): UseMutationResult<undefined, Error, ReviewPreset> {
  const client = useQueryClient();
  return useMutation<undefined, Error, ReviewPreset>({
    mutationFn: async (preset) => {
      const { error, response } = await apiClient.POST(
        '/api/v1/construction/set-review-policy/{projectID}',
        { params: { path: { projectID: projectId } }, body: { preset } }
      );
      if (error !== undefined) throw toApiError(response.status, error);
      return undefined;
    },
    onSuccess: () => client.invalidateQueries({ queryKey: projectKey(projectId) }),
  });
}

export interface UpdateReviewPolicyVars {
  gatedPhasesByType: Record<string, string[]>;
}

export function useUpdateReviewPolicy(
  projectId: string
): UseMutationResult<undefined, Error, UpdateReviewPolicyVars> {
  const client = useQueryClient();
  return useMutation<undefined, Error, UpdateReviewPolicyVars>({
    mutationFn: async (vars) => {
      const { error, response } = await apiClient.POST(
        '/api/v1/construction/update-review-policy/{projectID}',
        {
          params: { path: { projectID: projectId } },
          body: { policy: { gatedPhasesByType: vars.gatedPhasesByType } },
        }
      );
      if (error !== undefined) throw toApiError(response.status, error);
      return undefined;
    },
    onSuccess: () => client.invalidateQueries({ queryKey: projectKey(projectId) }),
  });
}
