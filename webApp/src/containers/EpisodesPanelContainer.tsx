/**
 * Containers-layer wiring for the SP1 capture-seam episodes panel (Task 10).
 * Owns: the episode list fetch, the on-row-click timeline fetch, and the
 * Export (JSON/CSV) assembly — none of which the pure `EpisodesPanel` /
 * `EpisodeTimeline` (components layer) may touch directly.
 *
 * Mount points: `SystemDesignContainer.tsx` (via `episodesSlot`, Phase 1),
 * `ProjectDesignExperience.tsx` (Phase 2), and `ConstructionConsole.tsx` (via
 * `ActivityLifecyclePanel`'s `episodesSlot`) — see each file's own comment for
 * why the mount is threaded as a prop rather than mounted from inside a
 * components-layer file.
 *
 * There is no `exportEpisodes` op (cut per the 2026-08-02 facet ruling): Export
 * assembles `{records, traces}` client-side from the already-fetched list plus
 * one `getEpisodeTimeline` call per listed episode (useFetchEpisodeTimelines),
 * fired only when the operator clicks Export.
 */
import { useState, type ReactNode } from 'react';
import {
  useEpisodesList,
  useEpisodeTimeline,
  useFetchEpisodeTimelines,
  type EpisodesManager,
} from '../hooks/useEpisodes';
import { EpisodesPanel } from '../components/episodes/EpisodesPanel';
import { flattenEpisodesToCsv, type EpisodeExport } from '../utilities/episodeCsv';
import { downloadTextFile } from '../utilities/download';
import type { EpisodeRecordView } from '../contracts/types';

export interface EpisodesPanelContainerProps {
  projectId: string;
  manager: EpisodesManager;
  /** activityId (construction) or the page's ArtifactKindFull slug (design). */
  targetRef: string;
  /** Render-prop slot threaded straight to EpisodesPanel (assurance/completeness
   *  badges — audit spine workstream). Renders nothing when omitted. */
  badges?: ((episode: EpisodeRecordView) => ReactNode) | undefined;
}

function exportFilenameBase(target: EpisodesPanelContainerProps): string {
  return `episodes-${target.manager}-${target.targetRef}`;
}

export function EpisodesPanelContainer({
  projectId,
  manager,
  targetRef,
  badges,
}: EpisodesPanelContainerProps): ReactNode {
  const target = { projectId, manager, targetRef };
  const list = useEpisodesList(target);
  const [selectedEpisodeId, setSelectedEpisodeId] = useState<string | null>(null);
  const timeline = useEpisodeTimeline(target, selectedEpisodeId ?? undefined);
  const fetchTimelines = useFetchEpisodeTimelines(target);
  const [exportPending, setExportPending] = useState(false);

  const episodes = list.data ?? [];

  const buildExport = async (): Promise<EpisodeExport> => {
    setExportPending(true);
    try {
      const traces = await fetchTimelines(episodes.map((e) => e.episodeId));
      return { records: episodes, traces };
    } finally {
      setExportPending(false);
    }
  };

  const onExportJson = (): void => {
    void buildExport().then((exp) => {
      downloadTextFile(
        `${exportFilenameBase({ projectId, manager, targetRef })}.json`,
        JSON.stringify(exp, null, 2),
        'application/json'
      );
    });
  };

  const onExportCsv = (): void => {
    void buildExport().then((exp) => {
      downloadTextFile(
        `${exportFilenameBase({ projectId, manager, targetRef })}.csv`,
        flattenEpisodesToCsv(exp),
        'text/csv'
      );
    });
  };

  return (
    <EpisodesPanel
      badges={badges}
      episodes={episodes}
      error={list.error?.message}
      exportPending={exportPending}
      isLoading={list.isLoading}
      selectedEpisodeId={selectedEpisodeId}
      timeline={timeline.data}
      timelineLoading={timeline.isLoading}
      onExportCsv={onExportCsv}
      onExportJson={onExportJson}
      onSelectEpisode={setSelectedEpisodeId}
    />
  );
}
