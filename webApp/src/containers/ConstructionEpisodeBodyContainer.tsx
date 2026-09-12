/**
 * Containers-layer wiring for the construction detail pane's EPISODE body
 * (Stage B Task 9).
 *
 * The sibling of EpisodesPanelContainer, and deliberately not a reuse of it: the
 * pane's body is `EpisodeBody`, which adds the attribution caption and the
 * subagent gantt over the same panel, and threading a "which component do you
 * want?" switch through the existing container would have made one file serve
 * two surfaces with a flag.
 *
 * It exists at all because the components layer may not reach into hooks
 * (eslint.platform.config.js): `EpisodeBody` is props-only, exactly like
 * `EpisodesPanel`, and the fetch happens here.
 *
 * `targetRef` is the ACTIVITY id, never the attempt key. The server matches an
 * activity's episodes whether their stored TargetRef is the bare id or the
 * composite attempt key, so asking by activity is what actually returns
 * everything; narrowing to the selected attempt is then done in the body, where
 * it can be CAPTIONED rather than silently applied.
 */
import { useState, type ReactNode } from 'react';
import {
  useEpisodesList,
  useEpisodeTimeline,
  useFetchEpisodeTimelines,
} from '../hooks/useEpisodes';
import { EpisodeBody } from '../components/construction/detail/bodies/EpisodeBody';
import { flattenEpisodesToCsv, type EpisodeExport } from '../utilities/episodeCsv';
import { downloadTextFile } from '../utilities/download';

export interface ConstructionEpisodeBodyContainerProps {
  projectId: string;
  activityId: string;
  /** The selected attempt key, when the pane's selection reaches a task attempt. */
  attemptId?: string | undefined;
}

export function ConstructionEpisodeBodyContainer({
  projectId,
  activityId,
  attemptId,
}: ConstructionEpisodeBodyContainerProps): ReactNode {
  const target = { projectId, manager: 'construction' as const, targetRef: activityId };
  const list = useEpisodesList(target);
  const [selectedEpisodeId, setSelectedEpisodeId] = useState<string | null>(null);
  const timeline = useEpisodeTimeline(target, selectedEpisodeId ?? undefined);
  const fetchTimelines = useFetchEpisodeTimelines(target);
  const [exportPending, setExportPending] = useState(false);
  const [exportError, setExportError] = useState<string | undefined>(undefined);

  const episodes = list.data ?? [];

  const buildExport = async (): Promise<EpisodeExport> => {
    setExportPending(true);
    setExportError(undefined);
    try {
      const traces = await fetchTimelines(episodes.map((e) => e.episodeId));
      return { records: episodes, traces };
    } finally {
      setExportPending(false);
    }
  };

  const onExport = (extension: 'json' | 'csv'): void => {
    void buildExport()
      .then((exp) => {
        downloadTextFile(
          `episodes-construction-${activityId}.${extension}`,
          extension === 'json' ? JSON.stringify(exp, null, 2) : flattenEpisodesToCsv(exp),
          extension === 'json' ? 'application/json' : 'text/csv'
        );
      })
      .catch((err: unknown) => {
        setExportError(err instanceof Error ? err.message : 'export failed');
      });
  };

  return (
    <EpisodeBody
      attemptId={attemptId}
      episodes={episodes}
      error={list.error?.message}
      exportError={exportError}
      exportPending={exportPending}
      isLoading={list.isLoading}
      selectedEpisodeId={selectedEpisodeId}
      timeline={timeline.data}
      timelineError={timeline.error?.message}
      timelineLoading={timeline.isLoading}
      onExportCsv={() => {
        onExport('csv');
      }}
      onExportJson={() => {
        onExport('json');
      }}
      onSelectEpisode={setSelectedEpisodeId}
    />
  );
}
