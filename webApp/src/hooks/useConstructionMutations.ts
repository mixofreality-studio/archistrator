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
import {
  useIsMutating,
  useMutation,
  useQueryClient,
  type UseMutationResult,
} from '@tanstack/react-query';
import { apiClient } from '../api/client';
// The status decides success, never the parsed body: an empty-body 5xx comes back
// with `error: undefined` (throwUnlessOk, fix-D review I2).
import { throwUnlessOk } from '../contracts/errors';
import { overrideKindToOrdinal, phaseDecisionToOrdinal } from '../contracts/wire';
import type {
  OverrideKind,
  PhaseDecision,
  ProjectStateWithGit,
  ReviewPreset,
} from '../contracts/types';
import type { components } from '../contracts/schema';
import { constructionSessionKey, constructionSessionsKey } from './useConstructionSession';
import { projectKey } from './useProject';

/** The mutation-cache key of one project's Begin dispatches. */
export function beginConstructionKey(projectId: string): readonly unknown[] {
  return ['beginConstruction', projectId];
}

/**
 * Whether a Begin dispatch for this project is in flight, from ANY mount of the
 * console (fix-E review I2). A remount builds a new mutation observer whose own
 * `isPending` is false while the first dispatch is still unanswered, so the
 * remounted console would offer Begin again. The mutation cache knows better.
 */
export function useBeginConstructionPending(projectId: string): boolean {
  return useIsMutating({ mutationKey: beginConstructionKey(projectId) }) > 0;
}

/**
 * Dispatch the next activity. The caller supplies the tickID, minted ONCE per
 * confirm-dialog opening (fix-A review I1). It is a correlation id, not the guard
 * against a second pump — the server runs one pump workflow per project (architect
 * I1 ruling); the confirm dialog's press-once and the console's in-flight ref are
 * the client's UX debouncing.
 */
export function useBeginConstruction(
  projectId: string,
  options?: {
    /**
     * Runs at the MUTATION level, so it still runs when the answer lands after the
     * console unmounted (a callback passed to `mutate` would not). The console
     * records the failure in module memory from here (fix-E review I2), with what
     * the project read on screen said at that moment (fix-G review I1): only a
     * change from "not started" can count as pump evidence.
     */
    onError?: (error: Error, atFailure: { constructionStarted: boolean | undefined }) => void;
  }
): UseMutationResult<undefined, Error, string> {
  const onFailure = options?.onError;
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
    mutationKey: beginConstructionKey(projectId),
    mutationFn: async (tickID) => {
      const { error, response } = await apiClient.POST(
        '/api/v1/construction/execute-next-activity/{projectID}',
        { params: { path: { projectID: projectId } }, body: { tickID } }
      );
      throwUnlessOk(response, error);
      return undefined;
    },
    onSuccess: refresh,
    // A FAILED dispatch refreshes exactly as a successful one does (fix-C review):
    // the server starts the pump before it answers and can still answer 5xx after
    // that, or the response can be dropped on the way. Only a fresh read can say
    // whether construction started, so the console never guesses from the error.
    onError: async (error) => {
      const shown = client.getQueryData<ProjectStateWithGit>(projectKey(projectId));
      onFailure?.(error, { constructionStarted: shown?.constructionStarted });
      await refresh();
    },
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
      throwUnlessOk(response, error);
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
      throwUnlessOk(response, error);
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
}

export function useSubmitPhaseDecision(
  projectId: string
): UseMutationResult<undefined, Error, SubmitPhaseDecisionVars> {
  const client = useQueryClient();
  return useMutation<undefined, Error, SubmitPhaseDecisionVars>({
    mutationFn: async (vars) => {
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
      throwUnlessOk(response, error);
      return undefined;
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
      throwUnlessOk(response, error);
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
      throwUnlessOk(response, error);
      return undefined;
    },
    onSuccess: () => client.invalidateQueries({ queryKey: projectKey(projectId) }),
  });
}
