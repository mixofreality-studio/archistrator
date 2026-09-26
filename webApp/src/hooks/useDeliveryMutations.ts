/**
 * Every WRITE of the delivery rail, in one module.
 *
 * Three Managers published forty ops; `deliveryManager` publishes ten writes. The
 * hooks that used to differ by RAIL now differ only by which member of
 * `ReviewDecisionInput` an intent fills — the rail is the server's business, read
 * off the committed activity list (`railFor`), not the client's.
 *
 * ── Addressing changed, and it is the one real shape change ──────────────────
 * The design-rail writes used to be addressed `(projectId, artifactKind)`. Every
 * delivery write is addressed `(projectId, activityId)` + a `taskID` in the body,
 * and the server resolves the artifact kind from the activity's lifecycle
 * (`artifactKindForTask`, which accepts either a dispatch task's own kind or a
 * review task's `reviews` target). So the caller passes what it is LOOKING at, and
 * nothing here converts a kind. A screen that knows only an artifact kind resolves
 * its (activityId, taskId) through `components/activity/designTaskRef.ts` — that
 * lives beside `lifecycles.gen.ts`, the data it reads, and the import boundary
 * (hooks may not import components) is why it is not in this file.
 *
 * Every invalidation key is VERBATIM what it was before the merge.
 */
import {
  useIsMutating,
  useMutation,
  useQueryClient,
  type UseMutationResult,
} from '@tanstack/react-query';
import { useOpsClient } from '../api/opsContext';
import type { OpBody, OpResult } from '../api/opTypes';
import { ApiError } from '../contracts/errors';
import { overrideKindToOrdinal } from '../contracts/wire';
import { REVIEW_DECISION_APP_TO_ORDINAL } from '../contracts/enums.gen';
import type {
  AnchoredComment,
  ArtifactKind,
  OverrideKind,
  ProjectStateWithGit,
  ReviewCommentStatus,
  ReviewPreset,
} from '../contracts/types';
import type { components } from '../contracts/schema';
import { phaseDecisionFilters, phaseDecisionMutationKey } from './phaseDecisionKey';
import {
  activityViewKey,
  activityViewsKey,
  constructionSessionKey,
  constructionSessionsKey,
  projectKey,
  projectsKey,
  sessionStateKey,
  sessionStateProjectKey,
} from './useDeliveryQueries';

type Schemas = components['schemas'];
type ReviewDecisionInput = Schemas['DeliveryReviewDecisionInput'];
type ReviewFeedback = Schemas['DeliveryReviewFeedback'];

/**
 * Every activity-addressed write names the (activity, task) it answers. A gate's
 * task is the one the reader has open; the server reads the artifact kind off it.
 */
export interface ActivityTaskRef {
  activityId: string;
  taskId: string;
}

/**
 * The ONE place that knows a design write also moves a kind-addressed session
 * query. A design gate's screens are keyed by artifact kind, so a write that
 * changed an artifact must invalidate that kind's session probe as well as the
 * activity view — and when the caller does not know the kind (a construction gate),
 * the activity view and the session prefix are the whole story.
 */
function invalidateAfterWrite(
  client: ReturnType<typeof useQueryClient>,
  projectId: string,
  ref: ActivityTaskRef,
  artifactKind: ArtifactKind | undefined
): Promise<void> {
  return Promise.all([
    client.invalidateQueries({ queryKey: projectKey(projectId) }),
    client.invalidateQueries({ queryKey: activityViewKey(projectId, ref.activityId) }),
    client.invalidateQueries({ queryKey: constructionSessionKey(projectId, ref.activityId) }),
    artifactKind === undefined
      ? Promise.resolve()
      : client.invalidateQueries({ queryKey: sessionStateKey(projectId, artifactKind) }),
  ]).then(() => undefined);
}

// ── op 1: StartProject ───────────────────────────────────────────────────────

/** WHO operates the built app — selfOperated (customer's own infra; the default) or
 * archistratorOperated (archistrator operates it on the platform, constraining the
 * deployment design to the platform palette). */
export type OperatingModel = 'selfOperated' | 'archistratorOperated';

export interface StartProjectVars {
  /**
   * ABSENT creates the project — the Manager mints the id (`projectID == nil` →
   * `sd.CreateProject`). PRESENT adopts or continues that project instead.
   *
   * It is a BODY field, not a path segment. The route was
   * `start-project/{projectID}` for one commit, which made create unreachable over
   * REST (a required segment Go's mux will not match empty, and a handler that
   * always passed a non-nil pointer); the route dropped the segment in f1a07067.
   * That is why no caller mints an id client-side: absence is how create is said.
   */
  projectId?: string | undefined;
  name: string;
  owner: string;
  operatingModel?: OperatingModel;
  research?: Schemas['DeliveryResearchInput'];
  /** Begin the first design activity as part of the same call. */
  start: boolean;
}

export type StartProjectResult = OpResult<'deliveryStartProject'>;

/**
 * Create a project, attach its operating model and research corpus, and start it —
 * one op where there were four (`CreateProject`, `SetOperatingModel`,
 * `SetResearchInput`, `StartSystemDesign`), so the create-and-choose atomicity the
 * old hook faked with a follow-up call is now the server's.
 */
export function useStartProject(): UseMutationResult<StartProjectResult, Error, StartProjectVars> {
  const client = useQueryClient();
  const { ops } = useOpsClient();
  return useMutation<StartProjectResult, Error, StartProjectVars>({
    mutationFn: async (vars) => {
      return ops.callForBody<StartProjectResult>('deliveryStartProject', {
        body: {
          name: vars.name,
          owner: vars.owner,
          start: vars.start,
          // OMITTED, not empty-string: absence is what asks the server to mint an id.
          ...(vars.projectId !== undefined ? { projectID: vars.projectId } : {}),
          ...(vars.operatingModel !== undefined ? { model: vars.operatingModel } : {}),
          ...(vars.research !== undefined ? { research: vars.research } : {}),
        } satisfies OpBody<'deliveryStartProject'>,
      });
    },
    onSuccess: async (result) => {
      await Promise.all([
        client.invalidateQueries({ queryKey: projectsKey() }),
        client.invalidateQueries({ queryKey: projectKey(result.projectId) }),
        client.invalidateQueries({ queryKey: sessionStateProjectKey(result.projectId) }),
      ]);
    },
  });
}

// ── op 2: ExecuteNextActivity ────────────────────────────────────────────────

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
 * Dispatch the next activity — the "Begin construction" tick. The caller supplies
 * the tickID, minted ONCE per confirm-dialog opening (fix-A review I1). It is a
 * correlation id, not the guard against a second pump: the server runs one pump
 * workflow per project (architect I1 ruling).
 */
export function useExecuteNextActivity(
  projectId: string,
  options?: {
    /** Runs at the MUTATION level, so it still runs when the answer lands after the
     *  console unmounted, with what the project read on screen said at that moment
     *  (fix-G review I1): only a change from "not started" can count as evidence. */
    onError?: (error: Error, atFailure: { constructionStarted: boolean | undefined }) => void;
    /** Runs at the MUTATION level too, BEFORE the refresh is requested (fix H). It
     *  gets what the pump said: `dispatched: false` is a quiet tick (fix I). */
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
      client.invalidateQueries({ queryKey: activityViewsKey(projectId) }),
    ]);
  };
  return useMutation<BeginResult, Error, string>({
    mutationKey: beginConstructionKey(projectId),
    mutationFn: async (tickID) => {
      const data = await ops.call<OpResult<'deliveryExecuteNextActivity'> | undefined>(
        'deliveryExecuteNextActivity',
        {
          path: { projectID: projectId },
          body: { tickID } satisfies OpBody<'deliveryExecuteNextActivity'>,
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

// ── op 3: DispatchActivityTask ───────────────────────────────────────────────

export interface DispatchTaskVars extends ActivityTaskRef {
  /** Redraft guidance, when this dispatch is a re-run answering a review. */
  feedback?: string | undefined;
  /** The artifact kind this task produces, when the caller knows it — only so the
   *  kind-addressed session probe is invalidated too. Never sent. */
  artifactKind?: ArtifactKind | undefined;
}

/**
 * Run (or re-run) one task of an activity: a design draft, a redraft, or the M0
 * plan's re-derivation. Replaces `RequestArtifactDraft` on both design rails and
 * `RequestSDPCommit`; the construction rail has no run verb before stage 4b and the
 * Manager says so.
 */
export function useDispatchActivityTask(
  projectId: string
): UseMutationResult<string, Error, DispatchTaskVars> {
  const client = useQueryClient();
  const { ops } = useOpsClient();
  return useMutation<string, Error, DispatchTaskVars>({
    mutationFn: async (vars) => {
      return ops.callForBody<string>('deliveryDispatchActivityTask', {
        path: { projectID: projectId, activityID: vars.activityId },
        body: {
          taskID: vars.taskId,
          ...(vars.feedback !== undefined ? { feedback: { notes: vars.feedback } } : {}),
        } satisfies OpBody<'deliveryDispatchActivityTask'>,
      });
    },
    onSuccess: (_data, vars) => invalidateAfterWrite(client, projectId, vars, vars.artifactKind),
    // A refused request (409 failed_precondition: "a draft is already generating…")
    // means the SPA's no-session view was STALE — a session is running server-side
    // (e.g. auto-started by the phase advance). Refetch so the UI flips to the
    // truthful generating scene instead of a dead "Request draft" card.
    onError: (_error, vars) => invalidateAfterWrite(client, projectId, vars, vars.artifactKind),
  });
}

// ── op 4: SubmitReviewDecision ───────────────────────────────────────────────

/** When the server answered a decision that came back clean. */
export interface DecisionAnswer {
  answeredAt: number;
}

/** A decision that did not come back clean: the HTTP status where there was one
 *  (absent for a network failure), and when the console learned of it. */
export class PhaseDecisionFailure extends Error {
  readonly status: number | undefined;
  /** The wire's error code, where one arrived — `failed_precondition` is the F55
   *  refusal the M0 advance answers with "advance anyway", so the surface that
   *  offers that exit needs it and not only the status. */
  readonly code: string | undefined;
  readonly answeredAt: number;

  constructor(cause: unknown, answeredAt: number) {
    super(cause instanceof Error ? cause.message : String(cause));
    this.name = 'PhaseDecisionFailure';
    this.status = cause instanceof ApiError ? cause.status : undefined;
    this.code = cause instanceof ApiError ? cause.code : undefined;
    this.answeredAt = answeredAt;
  }
}

/** The HTTP status a failed mutation carries, or `undefined` when no response arrived. */
export function failureStatusOf(err: unknown): number | undefined {
  return err instanceof ApiError ? err.status : undefined;
}

/**
 * Every phase decision of one project shares this mutation key, so the console
 * reads what is in flight — and what each one answered — from the QueryClient's
 * mutation cache (useMutationState) rather than from component state. A pending
 * decision therefore survives a remount of the console, and so does its evidence.
 */
export { phaseDecisionFilters, phaseDecisionMutationKey };

export interface SubmitDecisionVars extends ActivityTaskRef {
  decision: ReviewDecisionInput;
  feedback?: ReviewFeedback | undefined;
  /** The artifact kind this gate judges, when the caller knows it — invalidation
   *  only, never sent. */
  artifactKind?: ArtifactKind | undefined;
  /** Client bookkeeping, never sent: which gate occurrence this decision answers
   *  and what the console needs to show for it after a remount. The console reads
   *  it back from the mutation cache (tasks/decisionRecords.ts). */
  occurrence?: { key: string; epoch: number; snapshot?: unknown } | undefined;
}

/**
 * THE gate write. One op behind every decision in the product: a design approve or
 * send-back, a construction phase approve or send-back, the M0 commit, a
 * comment-status flip, and the phase advance. Which one it is, is which members of
 * `ReviewDecisionInput` are filled — `containers/activityVerbs.ts` builds that and
 * is where the intent→members table lives.
 *
 * NOT every (rail, decision) pair is legal: the construction rail refuses
 * `ReviewSetCommentStatus` and `ReviewWithdraw` until stage 4b (the Manager answers
 * ContractMisuse), which is why `activityVerbs` still withholds those verbs there.
 */
export function useSubmitReviewDecision(
  projectId: string
): UseMutationResult<DecisionAnswer, PhaseDecisionFailure, SubmitDecisionVars> {
  const client = useQueryClient();
  const { ops } = useOpsClient();
  return useMutation<DecisionAnswer, PhaseDecisionFailure, SubmitDecisionVars>({
    mutationKey: phaseDecisionMutationKey(projectId),
    mutationFn: async (vars) => {
      try {
        await ops.call('deliverySubmitReviewDecision', {
          path: { projectID: projectId, activityID: vars.activityId },
          body: {
            taskID: vars.taskId,
            decision: vars.decision,
            ...(vars.feedback !== undefined ? { feedback: vars.feedback } : {}),
          } satisfies OpBody<'deliverySubmitReviewDecision'>,
        });
        return { answeredAt: Date.now() };
      } catch (e) {
        throw new PhaseDecisionFailure(e, Date.now());
      }
    },
    // An APPROVE auto-advances the phase workflow, which AUTO-STARTS the next step's
    // co-author session server-side (QA incident 2026-07-15) — so an approve
    // invalidates the whole project's session probes, not just this artifact's:
    // the next step's cached no-session 404 must refetch and discover it.
    onSuccess: async (_data, vars) => {
      await invalidateAfterWrite(client, projectId, vars, vars.artifactKind);
      // An approve or an advance is what auto-starts the NEXT step's session
      // server-side, so both refresh the whole project's probes. Compared against
      // the generated ordinals, not the app union — which has no `advance` member.
      if (
        vars.decision.decision === REVIEW_DECISION_APP_TO_ORDINAL.approve ||
        vars.decision.decision === REVIEW_DECISION_APP_TO_ORDINAL.advance
      ) {
        await client.invalidateQueries({ queryKey: sessionStateProjectKey(projectId) });
      }
    },
  });
}

// ── op 5: AskQuestions ───────────────────────────────────────────────────────

export interface AskQuestionsVars extends ActivityTaskRef {
  /** The role every question in this batch is addressed to. */
  addressee: string;
  questions: AnchoredComment[];
  artifactKind?: ArtifactKind | undefined;
}

/**
 * Ask clarifying QUESTIONS about a task's artifact WITHOUT sending it back for a
 * redraft. The questions are appended to the review ledger as question-type entries
 * and a lightweight answer job is dispatched; open questions do NOT block approve.
 *
 * Both design rails have it. The CONSTRUCTION rail does not, until stage 4b.
 */
export function useAskQuestions(
  projectId: string
): UseMutationResult<undefined, Error, AskQuestionsVars> {
  const client = useQueryClient();
  const { ops } = useOpsClient();
  return useMutation<undefined, Error, AskQuestionsVars>({
    mutationFn: async (vars) => {
      await ops.call('deliveryAskQuestions', {
        path: { projectID: projectId, activityID: vars.activityId },
        body: {
          taskID: vars.taskId,
          addressee: vars.addressee,
          questions: vars.questions,
        } satisfies OpBody<'deliveryAskQuestions'>,
      });
      return undefined;
    },
    onSuccess: (_data, vars) => invalidateAfterWrite(client, projectId, vars, vars.artifactKind),
  });
}

// ── op 6: AcknowledgeStaleBasis ──────────────────────────────────────────────

export interface AcknowledgeStaleVars extends ActivityTaskRef {
  /** The audit note recorded on the staleAck ledger entry. The Manager requires it. */
  note: string;
  artifactKind?: ArtifactKind | undefined;
}

/**
 * Mark a stale committed artifact "reviewed — unaffected" (F45): clears its
 * StaleBasis WITHOUT a redraft, recording the note as a durable staleAck audit
 * entry. Both design rails; not construction before stage 4b.
 */
export function useAcknowledgeStaleBasis(
  projectId: string
): UseMutationResult<undefined, Error, AcknowledgeStaleVars> {
  const client = useQueryClient();
  const { ops } = useOpsClient();
  return useMutation<undefined, Error, AcknowledgeStaleVars>({
    mutationFn: async (vars) => {
      await ops.call('deliveryAcknowledgeStaleBasis', {
        path: { projectID: projectId, activityID: vars.activityId },
        body: {
          taskID: vars.taskId,
          note: vars.note,
        } satisfies OpBody<'deliveryAcknowledgeStaleBasis'>,
      });
      return undefined;
    },
    onSuccess: (_data, vars) => invalidateAfterWrite(client, projectId, vars, vars.artifactKind),
  });
}

// ── op 7: SetProjectRunState ─────────────────────────────────────────────────

export interface SetRunStateVars {
  runState: Schemas['DeliveryProjectRunState'];
  /** Why — recorded on the pause. The resume sends ''. */
  reason: string;
}

/**
 * Pause or resume the project's construction, as one op over an enum. Replaces
 * `PauseProject` and `ResumeProject`.
 *
 * The PAUSE control itself is still unbuilt (its Interventions tab was retired in
 * Task 13 and B1 rebuilt only the way back); that remains an earmark, not a new
 * feature of this task. Resume has a caller.
 */
export function useSetProjectRunState(
  projectId: string
): UseMutationResult<undefined, Error, SetRunStateVars> {
  const client = useQueryClient();
  const { ops } = useOpsClient();
  return useMutation<undefined, Error, SetRunStateVars>({
    mutationFn: async (vars) => {
      await ops.call('deliverySetProjectRunState', {
        path: { projectID: projectId },
        body: {
          runState: vars.runState,
          reason: vars.reason,
        } satisfies OpBody<'deliverySetProjectRunState'>,
      });
      return undefined;
    },
    onSuccess: async () => {
      await Promise.all([
        client.invalidateQueries({ queryKey: projectKey(projectId) }),
        client.invalidateQueries({ queryKey: constructionSessionsKey(projectId) }),
        client.invalidateQueries({ queryKey: activityViewsKey(projectId) }),
      ]);
    },
  });
}

// ── op 8: OverrideActivity ───────────────────────────────────────────────────

export interface OverrideActivityVars {
  activityId: string;
  kind: OverrideKind;
  notes?: string;
  /** Anchored comments accumulated for this steer, ride alongside the notes. */
  comments?: Schemas['DeliveryAnchoredComment'][];
}

/** Steer one activity: retry or skip, with notes. */
export function useOverrideActivity(
  projectId: string
): UseMutationResult<undefined, Error, OverrideActivityVars> {
  const client = useQueryClient();
  const { ops } = useOpsClient();
  return useMutation<undefined, Error, OverrideActivityVars>({
    mutationFn: async (vars) => {
      await ops.call('deliveryOverrideActivity', {
        path: { projectID: projectId, activityID: vars.activityId },
        body: {
          override: {
            kind: overrideKindToOrdinal(vars.kind),
            notes: vars.notes ?? '',
            ...(vars.comments && vars.comments.length > 0 ? { comments: vars.comments } : {}),
          },
        } satisfies OpBody<'deliveryOverrideActivity'>,
      });
      return undefined;
    },
    onSuccess: (_data, vars) =>
      Promise.all([
        client.invalidateQueries({
          queryKey: constructionSessionKey(projectId, vars.activityId),
        }),
        client.invalidateQueries({ queryKey: activityViewKey(projectId, vars.activityId) }),
      ]),
  });
}

// ── op 9: ReplanProject ──────────────────────────────────────────────────────

/**
 * Run the re-plan sweep that detects scope or variance drift and re-derives the
 * project network. No caller today — the variance surface that will press it is
 * unbuilt — but it is one of the twelve and this is its client.
 */
export function useReplanProject(
  projectId: string
): UseMutationResult<OpResult<'deliveryReplanProject'>, Error, string> {
  const client = useQueryClient();
  const { ops } = useOpsClient();
  return useMutation<OpResult<'deliveryReplanProject'>, Error, string>({
    mutationFn: async (tickID) => {
      return ops.callForBody<OpResult<'deliveryReplanProject'>>('deliveryReplanProject', {
        path: { projectID: projectId },
        body: { tickID } satisfies OpBody<'deliveryReplanProject'>,
      });
    },
    onSuccess: () => client.invalidateQueries({ queryKey: projectKey(projectId) }),
  });
}

// ── op 10: SetProjectExecutionPolicy ─────────────────────────────────────────

/**
 * Set the project's review-policy PRESET (the sophistication dial: vibes /
 * checkpoints / full). The other half of the same server-side ReviewPolicy, the
 * explicit per-type gate map, has no client since PolicyPanel was deleted — it
 * rides the same op's optional `policy.gatedPhasesByType`.
 *
 * Invalidates the project read so the home page's control reflects the committed
 * value (reviewPolicy.preset), never a local echo.
 */
export function useSetProjectExecutionPolicy(
  projectId: string
): UseMutationResult<undefined, Error, ReviewPreset> {
  const client = useQueryClient();
  const { ops } = useOpsClient();
  return useMutation<undefined, Error, ReviewPreset>({
    mutationFn: async (preset) => {
      await ops.call('deliverySetProjectExecutionPolicy', {
        path: { projectID: projectId },
        body: { policy: { preset } } satisfies OpBody<'deliverySetProjectExecutionPolicy'>,
      });
      return undefined;
    },
    onSuccess: () => client.invalidateQueries({ queryKey: projectKey(projectId) }),
  });
}

/** Re-exported for the comment-status callers, which name the two legal values. */
export type CommentStatus = Extract<ReviewCommentStatus, 'open' | 'resolved'>;
