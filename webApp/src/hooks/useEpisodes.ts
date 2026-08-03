/**
 * TanStack Query wrappers over the SP1 capture-seam episode read ops — one
 * `listEpisodesForActivity`/`listEpisodesForArtifact` + `getEpisodeTimeline` pair
 * per manager (construction / systemDesign / projectDesign), all riding the
 * transport-blind OpsClient (same pattern as useDesignHealth) so the SPA (REST)
 * and the MCP-hosted app (server tool calls) share one hook surface.
 *
 * `EpisodesTarget.targetRef` is:
 *  - the activityId, verbatim, for the construction manager;
 *  - the page's ArtifactKindFull (e.g. "mission", "planningAssumptions") for the
 *    systemDesign/projectDesign managers — converted to the wire's integer
 *    ArtifactKind ordinal via `artifactKindToOrdinal` (contracts/wire.ts) at the
 *    call site, since ListEpisodesForArtifact's `artifactKind` query param is the
 *    ordinal, not the app string.
 */
import { useQuery, type UseQueryResult } from '@tanstack/react-query';
import { useOpsClient } from '../api/opsContext';
import type { OpId } from '../api/ops.gen';
import { artifactKindToOrdinal, mapEpisodeRecordView, mapEpisodeTimeline } from '../contracts/wire';
import { ARTIFACT_KIND_APP_TO_ORDINAL } from '../contracts/enums.gen';
import type { components } from '../contracts/schema';
import type {
  ArtifactKindFull,
  EpisodeRecordView,
  EpisodeTimeline,
  TimelineEvent,
} from '../contracts/types';

type Schemas = components['schemas'];
type WireRecordView =
  | Schemas['ConstructionEpisodeRecordView']
  | Schemas['ProjectDesignEpisodeRecordView']
  | Schemas['SystemDesignEpisodeRecordView'];
type WireTimeline =
  | Schemas['ConstructionEpisodeTimeline']
  | Schemas['ProjectDesignEpisodeTimeline']
  | Schemas['SystemDesignEpisodeTimeline'];

export type EpisodesManager = 'construction' | 'systemDesign' | 'projectDesign';

/** One episode-capture target: a construction activity or a design artifact page. */
export interface EpisodesTarget {
  projectId: string;
  manager: EpisodesManager;
  /** activityId (construction) or the page's ArtifactKindFull slug (design). */
  targetRef: string;
}

const LIST_OP: Record<EpisodesManager, OpId> = {
  construction: 'constructionListEpisodesForActivity',
  systemDesign: 'systemDesignListEpisodesForArtifact',
  projectDesign: 'projectDesignListEpisodesForArtifact',
};

const TIMELINE_OP: Record<EpisodesManager, OpId> = {
  construction: 'constructionGetEpisodeTimeline',
  systemDesign: 'systemDesignGetEpisodeTimeline',
  projectDesign: 'projectDesignGetEpisodeTimeline',
};

// The full valid ArtifactKindFull domain (both phases share one ordinal range —
// see contracts/wire.ts's artifactKindToOrdinal doc comment), used to validate
// a design target's `targetRef` before trusting it as an ArtifactKindFull
// (2026-08-02 review minor (c): the cast below used to be unchecked).
const VALID_ARTIFACT_KINDS = new Set<string>(Object.keys(ARTIFACT_KIND_APP_TO_ORDINAL));

function isArtifactKindFull(value: string): value is ArtifactKindFull {
  return VALID_ARTIFACT_KINDS.has(value);
}

/** Whether a target is well-formed enough to fire a query for — gates `enabled`
 *  on both hooks below so an invalid design targetRef never reaches the wire. */
function isValidTarget(target: EpisodesTarget): boolean {
  if (target.projectId.length === 0 || target.targetRef.length === 0) return false;
  return target.manager === 'construction' || isArtifactKindFull(target.targetRef);
}

function listQuery(target: EpisodesTarget): Record<string, unknown> {
  if (target.manager === 'construction') return { activityID: target.targetRef };
  if (!isArtifactKindFull(target.targetRef)) {
    // Should be unreachable — `enabled: isValidTarget(target)` on the query
    // already keeps this from firing — but warn loudly rather than silently
    // querying ordinal 0 ("mission") for an unrecognized kind if it ever does.
    console.warn(
      `useEpisodes: unknown artifact kind "${target.targetRef}" for manager "${target.manager}" — episode list query skipped.`
    );
    return { artifactKind: 0 };
  }
  return { artifactKind: artifactKindToOrdinal(target.targetRef) };
}

export function episodesListKey(target: EpisodesTarget): readonly unknown[] {
  return ['episodes', target.manager, target.projectId, target.targetRef];
}

/** The episode list for one target — the panel's row data. */
export function useEpisodesList(target: EpisodesTarget): UseQueryResult<EpisodeRecordView[]> {
  const { ops } = useOpsClient();
  return useQuery<EpisodeRecordView[]>({
    queryKey: episodesListKey(target),
    queryFn: async () => {
      const data = await ops.call<WireRecordView[]>(LIST_OP[target.manager], {
        path: { projectID: target.projectId },
        query: listQuery(target),
      });
      return data.map(mapEpisodeRecordView);
    },
    enabled: isValidTarget(target),
  });
}

export function episodeTimelineKey(target: EpisodesTarget, episodeId: string): readonly unknown[] {
  return ['episodeTimeline', target.manager, target.projectId, episodeId];
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
    queryFn: async () => {
      const data = await ops.call<WireTimeline>(TIMELINE_OP[target.manager], {
        path: { projectID: target.projectId },
        query: { episodeID: episodeId ?? '' },
      });
      return mapEpisodeTimeline(data);
    },
    enabled: target.projectId.length > 0 && episodeId !== undefined && episodeId.length > 0,
  });
}

/**
 * On-demand batch timeline fetch for the Export action — there is no
 * `exportEpisodes` op (cut per the 2026-08-02 facet ruling), so the JSON/CSV
 * export assembles its `{records, traces}` payload client-side from the
 * already-fetched episode list plus one `getEpisodeTimeline` call per listed
 * episode, fired only when Export is clicked (not a standing query).
 */
export function useFetchEpisodeTimelines(
  target: EpisodesTarget
): (episodeIds: readonly string[]) => Promise<Record<string, TimelineEvent[]>> {
  const { ops } = useOpsClient();
  return async (episodeIds: readonly string[]): Promise<Record<string, TimelineEvent[]>> => {
    const entries = await Promise.all(
      episodeIds.map(async (episodeId) => {
        const data = await ops.call<WireTimeline>(TIMELINE_OP[target.manager], {
          path: { projectID: target.projectId },
          query: { episodeID: episodeId },
        });
        return [episodeId, mapEpisodeTimeline(data).events] as const;
      })
    );
    return Object.fromEntries(entries);
  };
}
