/**
 * Every READ of the delivery rail, in one module.
 *
 * Nine hooks lived in nine files because three Managers published three read
 * surfaces. `deliveryManager` publishes two reads — `QueryProjectView` (seven
 * kinds, selected by the query object) and `QueryActivityView` — so the hooks that
 * used to differ by OP now differ only by SELECTOR and by which member of the
 * returned view they unwrap.
 *
 * EVERY QUERY KEY IS VERBATIM what it was before the merge. The keys are what
 * `activityViewKey` invalidation, the plan's cascade predicate and every
 * `invalidateQueries` call in useDeliveryMutations join on; re-shaping one here
 * would be a cache break no test catches. Each hook's polling rules, retry rules,
 * staleTime and probe semantics are likewise unchanged — the behaviour of these
 * screens is not what stage 4a changes.
 *
 * `QueryProjectView` is POST with the selector in the BODY (`{ query: {...} }`),
 * not a URL query string: `OpParams.query` would become `?kind=...` on the REST
 * transport and be dropped on the floor.
 *
 * The no-session 404 still arrives as a 404: the Manager's dispatcher forwards
 * whatever the underlying rail returned, so `sessionProbeQueryFn`'s
 * absence-is-a-value contract (see sessionPolling.ts) is untouched by the merge.
 */
import {
  useQueries,
  useQuery,
  useQueryClient,
  type QueryClient,
  type UseQueryOptions,
  type UseQueryResult,
} from '@tanstack/react-query';
import { useCallback } from 'react';
import type { OpsClient } from '../api/ops.gen';
import { useOpsClient } from '../api/opsContext';
import type { OpBody, OpResult } from '../api/opTypes';
import { ApiError } from '../contracts/errors';
import {
  artifactKindToOrdinal,
  mapConstructionSession,
  mapDesignHealth,
  mapEpisodeRecordView,
  mapEpisodeTimeline,
  mapProjectSessionState,
  mapProjectState,
  mapProjectSummary,
  mapSessionState,
} from '../contracts/wire';
import { ARTIFACT_KIND_APP_TO_ORDINAL } from '../contracts/enums.gen';
import type {
  ArtifactKind,
  ArtifactKindFull,
  ConstructionSessionState,
  DesignHealth,
  EpisodeRecordView,
  EpisodeTimeline,
  ProjectArtifactKind,
  ProjectSessionState,
  ProjectStateWithGit,
  ProjectSummary,
  SessionStateResponse,
  TimelineEvent,
} from '../contracts/types';
import { PROJECT_TERMINAL_STAGES } from '../contracts/types';
import { useUser } from '../utilities/auth/UserContext';
import { activityViewPollIntervalMs } from './activityViewPolling';
import {
  DEGRADED_POLL_INTERVAL_MS,
  isNoSessionError,
  sessionPollIntervalMs,
  sessionProbeQueryFn,
} from './sessionPolling';
import {
  erroredProbeBackoffMs,
  erroredProbesFor,
  retryingProbesFor,
  sessionsByActivity,
  type SessionProbes,
} from './constructionSessions';

export type { SessionsById, SessionProbes } from './constructionSessions';

type ProjectView = OpResult<'deliveryQueryProjectView'>;
type ProjectViewQuery = OpBody<'deliveryQueryProjectView'>['query'];

/** One `QueryProjectView` call. The selector is the BODY, never a URL query. */
async function queryProjectView(ops: OpsClient, query: ProjectViewQuery): Promise<ProjectView> {
  return ops.callForBody<ProjectView>('deliveryQueryProjectView', { body: { query } });
}

/**
 * Unwrap the ONE member a view kind owes, or refuse.
 *
 * `ProjectView` is a `kind` plus eight optional members, so every member is
 * `T | undefined` to the compiler even though the kind determines exactly which
 * one is present. A `!` here would turn a server that answered the wrong kind into
 * an undefined-property crash three layers up, inside a mapper. This says which
 * kind asked and what was missing instead, and it is the only place in the SPA
 * that knows the view is a union.
 */
function member<K extends keyof ProjectView>(
  view: ProjectView,
  key: K,
  kind: string
): NonNullable<ProjectView[K]> {
  const value = view[key];
  if (value === undefined) {
    throw new ApiError(
      200,
      'empty_body',
      `the ${kind} project view carried no ${key} (kind: ${view.kind})`
    );
  }
  return value;
}

// ── project head-state + catalog ─────────────────────────────────────────────

export function projectKey(projectId: string): readonly unknown[] {
  return ['project', projectId];
}

/**
 * refetchInterval (ms) polls the project read — used by the Construction console to
 * animate the live pump cascade (per-activity status flips). Pass false (the
 * default) for the normal one-shot read. A function is given the latest read, for a
 * caller whose cadence depends on what the read says (the console polls while the
 * state shows work in flight, and a caller cannot know that before this hook runs).
 */
export function useProject(
  projectId: string,
  refetchInterval:
    | number
    | false
    | ((project: ProjectStateWithGit | undefined) => number | false) = false
): UseQueryResult<ProjectStateWithGit> {
  const { ops } = useOpsClient();
  const interval =
    typeof refetchInterval === 'function'
      ? (query: { state: { data: ProjectStateWithGit | undefined } }): number | false =>
          refetchInterval(query.state.data)
      : refetchInterval;
  return useQuery<ProjectStateWithGit>({
    queryKey: projectKey(projectId),
    queryFn: async () => {
      const view = await queryProjectView(ops, { kind: 'summary', projectId });
      return mapProjectState(member(view, 'summary', 'summary'));
    },
    enabled: projectId.length > 0,
    refetchInterval: interval,
  });
}

/** Base key — owner-scoped queries hang under it so invalidation by prefix works. */
export function projectsKey(): readonly unknown[] {
  return ['projects'];
}

export function useProjects(): UseQueryResult<ProjectSummary[]> {
  const owner = useUser().sub;
  const { ops } = useOpsClient();
  return useQuery<ProjectSummary[]>({
    queryKey: [...projectsKey(), owner],
    queryFn: async () => {
      const view = await queryProjectView(ops, { kind: 'projects', owner });
      // The catalog may legitimately be EMPTY (a new account), which the wire sends
      // as [] or null — so absence is a value here, unlike every other view kind.
      return (view.projects ?? []).map(mapProjectSummary);
    },
    staleTime: 30_000,
  });
}

export function designHealthKey(projectId: string): readonly unknown[] {
  return ['designHealth', projectId];
}

export function useDesignHealth(projectId: string): UseQueryResult<DesignHealth> {
  const { ops } = useOpsClient();
  return useQuery<DesignHealth>({
    queryKey: designHealthKey(projectId),
    queryFn: async () => {
      const view = await queryProjectView(ops, { kind: 'designHealth', projectId });
      return mapDesignHealth(member(view, 'designHealth', 'designHealth'));
    },
    enabled: projectId.length > 0,
  });
}

// ── Phase-1 design session probe ─────────────────────────────────────────────

export function sessionStateKey(projectId: string, kind: ArtifactKind): readonly unknown[] {
  return ['sessionState', projectId, kind];
}

/**
 * The project-scoped prefix of every per-kind session-state query. Invalidating it
 * refetches ALL of a project's session probes at once — used by the start mutation
 * and the approve decision (which auto-advances the phase workflow and auto-starts
 * the NEXT step's session server-side), neither of which knows or tracks each
 * per-kind query.
 */
export function sessionStateProjectKey(projectId: string): readonly unknown[] {
  return ['sessionState', projectId];
}

/**
 * Polls one Phase-1 co-authoring session's state. Polling runs every 2s while the
 * session is live (drafting / redrafting), watches the review gate AND the human
 * failure gates (refused / draftFailed) at the slow 8s gate cadence (awaitingReview
 * is NOT terminal — F-QA2-48; the failure gates move IN PLACE on Retry — F-QA2-50),
 * and stops only at the REST stages (committed / withdrawn). The full decision table
 * lives in sessionPolling.ts.
 *
 * The probe value: a live session view, or `null` for ESTABLISHED absence (the
 * server's deterministic no-session 404, resolved to a value by
 * sessionProbeQueryFn — see its doc for why absence must be data, not error).
 */
export function useSessionState(
  projectId: string,
  kind: ArtifactKind,
  enabled: boolean
): UseQueryResult<SessionStateResponse | null> {
  const { ops, transport } = useOpsClient();
  const queryClient = useQueryClient();
  const key = sessionStateKey(projectId, kind);
  return useQuery<SessionStateResponse | null>({
    queryKey: key,
    queryFn: sessionProbeQueryFn<SessionStateResponse>({
      fetch: async () => {
        const view = await queryProjectView(ops, {
          kind: 'session',
          projectId,
          artifactKind: artifactKindToOrdinal(kind),
        });
        // `session` is the PHASE-1 member. The same `session` view kind answers
        // Phase 2 under `projectSession` and construction under
        // `constructionSession` — the server routes on the selector, so asking
        // with a Phase-1 kind and reading another member would be a silent
        // mis-read of a structurally similar shape.
        return mapSessionState(member(view, 'session', 'session'));
      },
      getCached: () => queryClient.getQueryData<SessionStateResponse | null>(key),
    }),
    enabled: enabled && projectId.length > 0,
    // The no-session 404 resolves to null inside the probe (never throws), so
    // retry only ever sees real faults: one retry, no storms.
    retry: (count) => count < 1,
    // No re-probe on window-focus / remount — the poll cadence below is the single
    // refresh authority (refetchInterval overrides staleTime), so the 404 probe never
    // bursts on tab switches and a live session keeps its steady 2s poll.
    refetchOnWindowFocus: false,
    staleTime: Infinity,
    // MCP context never background-polls (spec §3.4) — an MCP host drives its own
    // refresh cadence around tool calls, so a client-side poll would just be
    // redundant traffic. On a failed refetch react-query keeps state.data (the last
    // good view) and sets state.error — this callback reads both, so a
    // stale-but-live stage keeps polling and self-heals.
    refetchInterval: (query) => {
      if (transport === 'mcp') return false;
      return sessionPollIntervalMs(query.state.data, query.state.error);
    },
  });
}

// ── Phase-2 design session probe ─────────────────────────────────────────────

export function projectSessionStateKey(
  projectId: string,
  kind: ProjectArtifactKind
): readonly unknown[] {
  return ['projectSessionState', projectId, kind];
}

const PROJECT_POLL_INTERVAL_MS = 2000;

/**
 * Polls one Phase-2 co-authoring (or SDP-review) session's state. Polling runs
 * every 2s while the session is live (drafting / assemblingSdp / awaitingReview /
 * redrafting) and stops at a terminal stage (committed / withdrawn / refused).
 *
 * The probe value: a live session view, or `null` for ESTABLISHED absence.
 */
export function useProjectSessionState(
  projectId: string,
  kind: ProjectArtifactKind,
  enabled: boolean
): UseQueryResult<ProjectSessionState | null> {
  const queryClient = useQueryClient();
  const { ops } = useOpsClient();
  const key = projectSessionStateKey(projectId, kind);
  return useQuery<ProjectSessionState | null>({
    queryKey: key,
    queryFn: sessionProbeQueryFn<ProjectSessionState>({
      fetch: async () => {
        const view = await queryProjectView(ops, {
          kind: 'session',
          projectId,
          artifactKind: artifactKindToOrdinal(kind),
        });
        // `projectSession` is the PHASE-2 member, typed
        // DeliveryProjectSessionStateView — a DIFFERENT schema from `session`'s
        // DeliverySessionStateView, whose only structural difference is an
        // optional `critique?` and whose `stage` ordinals diverge at
        // AssemblingSDP. Reading the wrong one typechecks and mis-stages the
        // gate (Task 7 finding).
        return mapProjectSessionState(member(view, 'projectSession', 'session'));
      },
      getCached: () => queryClient.getQueryData<ProjectSessionState | null>(key),
    }),
    enabled: enabled && projectId.length > 0,
    // The no-session 404 resolves to null inside the probe (never throws), so
    // retry only ever sees real faults: one retry, no storms.
    retry: (count) => count < 1,
    // Poll only while a live session exists. Established absence (null) or a
    // terminal stage stops the poll. F-QA2-28: any NON-404 error must never stop
    // the poll — one no-poll decision is permanent until a mutation invalidates,
    // so a transient fault froze a stale live view forever. Degrade to 5s instead.
    refetchInterval: (query) => {
      const { data } = query.state;
      if (data === null) return false;
      const stage = data?.stage;
      if (stage !== undefined && PROJECT_TERMINAL_STAGES.includes(stage)) return false;
      const { error } = query.state;
      if (error !== null && !isNoSessionError(error)) return DEGRADED_POLL_INTERVAL_MS;
      if (stage === undefined) return false;
      return PROJECT_POLL_INTERVAL_MS;
    },
  });
}

// ── construction session probe (single + fan-out) ────────────────────────────

const CONSTRUCTION_POLL_INTERVAL_MS = 3000;

/** Stages at which no further pump activity occurs for the session. */
const TERMINAL_STAGES = new Set(['exited', 'paused']);

export function constructionSessionKey(projectId: string, activityId?: string): readonly unknown[] {
  return ['constructionSession', projectId, activityId ?? null];
}

/** The prefix every per-activity session probe of one project shares — what a
 *  Begin invalidates, so an open activity's session re-reads after a dispatch. */
export function constructionSessionsKey(projectId: string): readonly unknown[] {
  return ['constructionSession', projectId];
}

/** One per-activity session probe (useConstructionSession, and — fanned out over
 *  the activities in flight — useConstructionSessions; both share these keys, so a
 *  probe the two ask for is fetched once). */
export function constructionSessionQueryOptions(
  queryClient: QueryClient,
  /** The transport the probe rides (useOpsClient().ops). */
  ops: OpsClient,
  projectId: string,
  activityId: string | undefined,
  enabled: boolean,
  /** How often to re-ask after the dormant 404. The single pane probe stops (a
   *  Begin invalidates it); the TASKS fan-out re-asks on the freshness cadence, so a
   *  workflow that starts later — the sweep, another tab, MCP — is found (review I3). */
  absentPollMs: number | false = false
): UseQueryOptions<ConstructionSessionState | null, Error, ConstructionSessionState | null> {
  const hasActivity = activityId !== undefined && activityId.length > 0;
  const key = constructionSessionKey(projectId, activityId);
  return {
    queryKey: key,
    queryFn: sessionProbeQueryFn<ConstructionSessionState>({
      fetch: async () => {
        const view = await queryProjectView(ops, {
          kind: 'session',
          projectId,
          activityId: activityId ?? '',
        });
        return mapConstructionSession(member(view, 'constructionSession', 'session'));
      },
      getCached: () => queryClient.getQueryData<ConstructionSessionState | null>(key),
    }),
    enabled: enabled && projectId.length > 0 && hasActivity,
    // The dormant-pump 404 resolves to null inside the probe (never throws), so
    // retry only ever sees real faults: one retry, no storms.
    retry: (count) => count < 1,
    refetchInterval: (query): number | false => {
      // A probe that has only ever failed backs off (designer re-check B1): the 3s
      // live cadence kept a failing endpoint busy and the TASKS lens blinking.
      if (query.state.data === undefined && query.state.errorUpdateCount > 0) {
        return erroredProbeBackoffMs(query.state.errorUpdateCount);
      }
      // Dormant pump (absence established as null): stop polling so the console
      // does not spam a 3s 404 storm. The project-read cascade poll drives the
      // tracker meanwhile; a Begin mutation invalidates this query.
      if (query.state.data === null) return absentPollMs;
      const stage = query.state.data?.stage;
      if (stage !== undefined && TERMINAL_STAGES.has(stage)) return false;
      return CONSTRUCTION_POLL_INTERVAL_MS;
    },
  };
}

/**
 * The probe value: a live session view, or `null` for ESTABLISHED absence (the
 * dormant-pump 404 resolved to a value).
 */
export function useConstructionSession(
  projectId: string,
  activityId?: string,
  enabled = true
): UseQueryResult<ConstructionSessionState | null> {
  const queryClient = useQueryClient();
  const { ops } = useOpsClient();
  return useQuery(
    constructionSessionQueryOptions(queryClient, ops, projectId, activityId, enabled)
  );
}

/**
 * The owed set's freshness cadence (review I3). The probe candidates come from the
 * project read, which otherwise polls only during a Begin cascade — so a gate on an
 * activity started later (by the sweep, another tab or MCP) stayed invisible, and a
 * probe that met the dormant 404 never asked again.
 */
export const TASKS_FRESHNESS_MS = 10_000;

export function useConstructionSessions(
  projectId: string,
  activityIds: readonly string[]
): SessionProbes & { retryErrored: () => void } {
  const queryClient = useQueryClient();
  const { ops } = useOpsClient();
  // `combine` re-runs whenever its reference changes, so it is keyed on the id
  // LIST's content: the route rebuilds the array on every 1.5s poll, and a fresh
  // reference each time would hand the owed-set derivation a new record per render.
  const idsKey = activityIds.join(' ');
  const combine = useCallback(
    (results: UseQueryResult<ConstructionSessionState | null>[]): SessionProbes => {
      const ids = idsKey.length > 0 ? idsKey.split(' ') : [];
      return {
        sessions: sessionsByActivity(ids, results),
        errored: erroredProbesFor(ids, results),
        retrying: retryingProbesFor(ids, results),
      };
    },
    [idsKey]
  );
  const probes = useQueries({
    queries: activityIds.map((id) =>
      constructionSessionQueryOptions(queryClient, ops, projectId, id, true, TASKS_FRESHNESS_MS)
    ),
    combine,
  });
  const erroredKey = probes.errored.join(' ');
  // Refetch exactly the probes that failed — the lens's "Couldn't check N…" Retry.
  const retryErrored = useCallback((): void => {
    for (const id of erroredKey.length > 0 ? erroredKey.split(' ') : []) {
      void queryClient.refetchQueries({
        queryKey: constructionSessionKey(projectId, id),
        exact: true,
      });
    }
  }, [queryClient, projectId, erroredKey]);
  return { ...probes, retryErrored };
}

// ── the activity experience's single read ────────────────────────────────────

export type ActivityView = OpResult<'deliveryQueryActivityView'>;

export function activityViewKey(projectId: string, activityId: string): readonly unknown[] {
  return ['activityView', projectId, activityId];
}

/** The prefix every activity view of one project shares — what a decision, an
 *  override or a Begin invalidates. */
export function activityViewsKey(projectId: string): readonly unknown[] {
  return ['activityView', projectId];
}

export function activityViewQueryOptions(
  /** The transport the read rides (useOpsClient().ops). */
  ops: OpsClient,
  projectId: string,
  activityId: string | undefined,
  enabled: boolean,
  notStartedPollMs: number | false = false
): UseQueryOptions<ActivityView, Error, ActivityView> {
  const id = activityId ?? '';
  return {
    queryKey: activityViewKey(projectId, id),
    queryFn: () =>
      ops.callForBody<ActivityView>('deliveryQueryActivityView', {
        path: { projectID: projectId, activityID: id },
      }),
    enabled: enabled && projectId.length > 0 && id.length > 0,
    // Unlike the session probe, absence is an ERROR here, not a value: a 404 means
    // the activity is not in the committed plan, which the screen must say, and it
    // never flips back on its own — so it is neither retried nor polled.
    retry: (count, error) => !isNoSessionError(error) && count < 1,
    refetchInterval: (query): number | false =>
      activityViewPollIntervalMs(query.state.data, query.state.error, notStartedPollMs),
  };
}

export function useActivityView(
  projectId: string,
  activityId?: string,
  enabled = true
): UseQueryResult<ActivityView> {
  const { ops } = useOpsClient();
  return useQuery(activityViewQueryOptions(ops, projectId, activityId, enabled));
}

// ── episodes (SP1 capture seam) ──────────────────────────────────────────────

/**
 * One episode-capture target: a construction activity, or a design artifact page.
 *
 * `manager` is GONE. It existed because three Managers each published their own
 * `listEpisodesFor*` pair and the client had to pick one (activityEpisodesManager).
 * `QueryProjectView(episodes)` routes on the SELECTOR instead — an artifactKind
 * (whose phase the server reads) or an activityId — so what survives is that one
 * real decision, in `episodesSelector` below.
 *
 * `targetRef` is the activityId for a construction target, or the page's
 * ArtifactKindFull slug for a design one.
 */
export interface EpisodesTarget {
  projectId: string;
  /** True for a construction activity; false for a design artifact page. */
  byActivity: boolean;
  targetRef: string;
}

// The full valid ArtifactKindFull domain (both phases share one ordinal range —
// see contracts/wire.ts's artifactKindToOrdinal doc comment), used to validate a
// design target's `targetRef` before trusting it as an ArtifactKindFull.
const VALID_ARTIFACT_KINDS = new Set<string>(Object.keys(ARTIFACT_KIND_APP_TO_ORDINAL));

function isArtifactKindFull(value: string): value is ArtifactKindFull {
  return VALID_ARTIFACT_KINDS.has(value);
}

/** Whether a target is well-formed enough to fire a query for — gates `enabled`
 *  on both hooks below so an invalid design targetRef never reaches the wire. */
export function isValidEpisodesTarget(target: EpisodesTarget): boolean {
  if (target.projectId.length === 0 || target.targetRef.length === 0) return false;
  return target.byActivity || isArtifactKindFull(target.targetRef);
}

/**
 * The selector one episode target names: an activityId, or an artifactKind ordinal.
 * This is all that is left of the manager dispatch table — the decision is still
 * real, it just chooses a FIELD now instead of an op.
 */
export function episodesSelector(target: EpisodesTarget): Partial<ProjectViewQuery> {
  if (target.byActivity) return { activityId: target.targetRef };
  if (!isArtifactKindFull(target.targetRef)) {
    // Unreachable — `enabled: isValidEpisodesTarget(target)` already keeps this
    // from firing — but warn loudly rather than silently querying ordinal 0
    // ("mission") for an unrecognized kind if it ever does.
    console.warn(
      `useEpisodesList: unknown artifact kind "${target.targetRef}" — episode list query skipped.`
    );
    return { artifactKind: 0 };
  }
  return { artifactKind: artifactKindToOrdinal(target.targetRef) };
}

/**
 * The cache key keeps its FOUR segments so nothing that invalidates by prefix
 * changes meaning; the third one is now the target's shape ('activity' | 'artifact')
 * rather than the manager that used to serve it. Two targets that differ only by
 * rail cannot collide, because a design page's slug is never an activityId.
 */
function targetSegment(target: EpisodesTarget): string {
  return target.byActivity ? 'activity' : 'artifact';
}

export function episodesListKey(target: EpisodesTarget): readonly unknown[] {
  return ['episodes', targetSegment(target), target.projectId, target.targetRef];
}

/** The episode list for one target — the panel's row data. */
export function useEpisodesList(target: EpisodesTarget): UseQueryResult<EpisodeRecordView[]> {
  const { ops } = useOpsClient();
  return useQuery<EpisodeRecordView[]>({
    queryKey: episodesListKey(target),
    queryFn: async () => {
      const view = await queryProjectView(ops, {
        kind: 'episodes',
        projectId: target.projectId,
        ...episodesSelector(target),
      });
      // An episode list is legitimately EMPTY before anything has been captured,
      // which the wire sends as [] or null.
      return (view.episodes ?? []).map(mapEpisodeRecordView);
    },
    enabled: isValidEpisodesTarget(target),
  });
}

export function episodeTimelineKey(target: EpisodesTarget, episodeId: string): readonly unknown[] {
  return ['episodeTimeline', targetSegment(target), target.projectId, episodeId];
}

async function fetchTimeline(
  ops: OpsClient,
  projectId: string,
  episodeId: string
): Promise<EpisodeTimeline> {
  const view = await queryProjectView(ops, { kind: 'timeline', projectId, episodeId });
  return mapEpisodeTimeline(member(view, 'timeline', 'timeline'));
}

/** The per-turn timeline for one expanded episode row. Disabled until an episode
 *  is selected (row click). */
export function useEpisodeTimeline(
  target: EpisodesTarget,
  episodeId: string | undefined
): UseQueryResult<EpisodeTimeline> {
  const { ops } = useOpsClient();
  return useQuery<EpisodeTimeline>({
    queryKey: episodeTimelineKey(target, episodeId ?? ''),
    queryFn: () => fetchTimeline(ops, target.projectId, episodeId ?? ''),
    enabled: target.projectId.length > 0 && episodeId !== undefined && episodeId.length > 0,
  });
}

/**
 * On-demand batch timeline fetch for the Export action — there is no
 * `exportEpisodes` op (cut per the 2026-08-02 facet ruling), so the JSON/CSV
 * export assembles its `{records, traces}` payload client-side from the
 * already-fetched episode list plus one timeline read per listed episode, fired
 * only when Export is clicked (not a standing query).
 */
export function useFetchEpisodeTimelines(
  target: EpisodesTarget
): (episodeIds: readonly string[]) => Promise<Record<string, TimelineEvent[]>> {
  const { ops } = useOpsClient();
  return async (episodeIds: readonly string[]): Promise<Record<string, TimelineEvent[]>> => {
    const entries = await Promise.all(
      episodeIds.map(async (episodeId) => {
        const timeline = await fetchTimeline(ops, target.projectId, episodeId);
        return [episodeId, timeline.events] as const;
      })
    );
    return Object.fromEntries(entries);
  };
}
