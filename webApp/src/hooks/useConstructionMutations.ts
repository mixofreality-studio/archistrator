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
// Every call rides the OpsClient (preview P1b). The status decides success, never
// the parsed body: an empty-body 5xx comes back with `error: undefined`, and the
// REST transport's `call` applies throwUnlessOk (fix-D review I2).
import { useOpsClient } from '../api/opsContext';
import type { OpBody, OpResult } from '../api/opTypes';
import { ApiError } from '../contracts/errors';
import { overrideKindToOrdinal, phaseDecisionToOrdinal } from '../contracts/wire';
import type {
  OverrideKind,
  PhaseDecision,
  ProjectStateWithGit,
  ReviewPreset,
} from '../contracts/types';
import type { components } from '../contracts/schema';
import { constructionSessionKey, constructionSessionsKey } from './useConstructionSession';
import { phaseDecisionFilters, phaseDecisionMutationKey } from './phaseDecisionKey';
import { projectKey } from './useProject';

/** What a successful Begin dispatch said: whether the pump started an activity
 *  (`undefined` where the body did not say). */
export interface BeginResult {
  dispatched: boolean | undefined;
}

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
    /**
     * Runs at the MUTATION level too, for the same reason, and BEFORE the refresh
     * is requested: the console records the success in module memory from here,
     * and only a read requested after that record may count as the pickup (fix H).
     * It gets what the pump said: `dispatched: false` is a quiet tick that started
     * nothing (fix I).
     */
    onSuccess?: (result: BeginResult) => void;
  }
): UseMutationResult<BeginResult, Error, string> {
  const onFailure = options?.onError;
  const onDispatched = options?.onSuccess;
  const client = useQueryClient();
  const { ops } = useOpsClient();
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
  return useMutation<BeginResult, Error, string>({
    mutationKey: beginConstructionKey(projectId),
    mutationFn: async (tickID) => {
      const data = await ops.call<OpResult<'constructionExecuteNextActivity'> | undefined>(
        'constructionExecuteNextActivity',
        {
          path: { projectID: projectId },
          body: { tickID } satisfies OpBody<'constructionExecuteNextActivity'>,
        }
      );
      // The status decided success; the body only says whether the pump started
      // anything. Read as optional: a 200 whose body omits it is not a "false".
      const said: { dispatched?: boolean } | undefined = data;
      return { dispatched: said?.dispatched };
    },
    onSuccess: async (result) => {
      onDispatched?.(result);
      await refresh();
    },
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

/**
 * Pause the project's construction (pause-project). NO CALLER TODAY, KEPT ON
 * PURPOSE: the Interventions tab that pressed it was retired (Task 13), and its
 * rebuild is B1's (plan-B1-B2.md; plan-B1-B2-amendment.md §B: "if we have pause
 * we should have resume"). This is the natural client for that control, and B1's
 * resume-project hook belongs beside it. Delete it only if B1 lands without a
 * pause control.
 */
export function usePauseConstruction(
  projectId: string
): UseMutationResult<undefined, Error, string> {
  const client = useQueryClient();
  const { ops } = useOpsClient();
  return useMutation<undefined, Error, string>({
    mutationFn: async (reason) => {
      await ops.call('constructionPauseProject', {
        path: { projectID: projectId },
        body: { reason } satisfies OpBody<'constructionPauseProject'>,
      });
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

/**
 * Steer one activity (override-activity). NO CALLER TODAY, KEPT ON PURPOSE:
 * InterventionDrawer, its old caller, is deleted, and B1's awaitingTakeover row
 * rebuilds the steer as `OverrideActivity(Retry|Skip, notes)` (plan-B1-B2.md §UI)
 * on this same endpoint. This hook is that row's natural client; B1 extends it
 * (the optional `awaitingSince` token, B-plan open question 4) rather than
 * re-adding it.
 */
export function useOverrideActivity(
  projectId: string
): UseMutationResult<undefined, Error, OverrideActivityVars> {
  const client = useQueryClient();
  const { ops } = useOpsClient();
  return useMutation<undefined, Error, OverrideActivityVars>({
    mutationFn: async (vars) => {
      await ops.call('constructionOverrideActivity', {
        path: { projectID: projectId, activityID: vars.activityId },
        body: {
          override: {
            kind: overrideKindToOrdinal(vars.kind),
            notes: vars.notes ?? '',
            ...(vars.comments && vars.comments.length > 0 ? { comments: vars.comments } : {}),
          },
        } satisfies OpBody<'constructionOverrideActivity'>,
      });
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
export { phaseDecisionFilters, phaseDecisionMutationKey };

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
  const { ops } = useOpsClient();
  return useMutation<PhaseDecisionAnswer, PhaseDecisionFailure, SubmitPhaseDecisionVars>({
    mutationKey: phaseDecisionMutationKey(projectId),
    mutationFn: async (vars) => {
      try {
        await ops.call('constructionSubmitPhaseDecision', {
          path: { projectID: projectId, activityID: vars.activityId },
          body: {
            phase: vars.phase,
            decision: phaseDecisionToOrdinal(vars.decision),
            ...(vars.feedback !== undefined ? { feedback: vars.feedback } : {}),
          } satisfies OpBody<'constructionSubmitPhaseDecision'>,
        });
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
 * checkpoints / full) via the construction SetReviewPolicy op. The other half of
 * the same server-side ReviewPolicy, the explicit per-type gate map
 * (update-review-policy), has no client since PolicyPanel was deleted.
 * Invalidates the project read so the home page's control reflects the
 * committed value (reviewPolicy.preset), never a local echo.
 */
export function useSetReviewPolicy(
  projectId: string
): UseMutationResult<undefined, Error, ReviewPreset> {
  const client = useQueryClient();
  const { ops } = useOpsClient();
  return useMutation<undefined, Error, ReviewPreset>({
    mutationFn: async (preset) => {
      await ops.call('constructionSetReviewPolicy', {
        path: { projectID: projectId },
        body: { preset } satisfies OpBody<'constructionSetReviewPolicy'>,
      });
      return undefined;
    },
    onSuccess: () => client.invalidateQueries({ queryKey: projectKey(projectId) }),
  });
}
