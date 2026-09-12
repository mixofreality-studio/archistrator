/**
 * THE EPISODE BODY — what the AI worker actually did, with an honest caption
 * about whose work it was.
 *
 * EpisodesPanel + EpisodeTimeline are reused VERBATIM: they already render the
 * outcome chip, duration, model, worker class, the 4-way token split with its
 * "tokens (main loop)" caption, turns, cost, tool counts, the subagent count and
 * the lineage tree. Forking them to change a header would have been the
 * hand-mirror this branch has already paid for twice. The two things this body
 * adds arrive through additive optional slots on that component
 * (`caption`, `spanStrip`), so every existing mount is untouched:
 *
 *   1. THE CAPTION. The pane addresses one task attempt; the episode fetch is
 *      keyed by ACTIVITY and returns legacy records whose `TargetRef` is the
 *      bare activity id. Rendering N episodes under a `SRS · attempt 1` header
 *      would claim, by placement alone, that they are that task's. The caption
 *      states the scope this surface can actually defend — see
 *      episodeAttribution.ts, where the rule lives and is tested.
 *   2. THE GANTT. The panel reports HOW MANY subagent spans an episode had; the
 *      strip shows their SHAPE — four in parallel for twenty seconds, or one
 *      after another for two minutes each. That is the first question anyone
 *      reading a slow episode has.
 *
 * Props-only, like EpisodesPanel itself: the components layer may not reach into
 * hooks (eslint.platform.config.js), so the fetch lives in
 * containers/ConstructionEpisodeBodyContainer.tsx.
 */
import { useMemo, type ReactElement } from 'react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';

import type { EpisodeRecordView, EpisodeTimeline } from '../../../../contracts/types';
import { useTokens } from '../../../../utilities/theme/ThemeContext';
import { UI_IDENTIFIERS } from '../../../../utilities/constants/UIIdentifiers';
import { EpisodesPanel } from '../../../episodes/EpisodesPanel';
import { attributionOf, CAPTURE_DEFERRED_NOTE, episodeScopeFor } from './episodeAttribution.ts';
import { SubagentGantt } from './SubagentGantt';

export interface EpisodeBodyProps {
  episodes: EpisodeRecordView[];
  isLoading: boolean;
  error?: string | undefined;
  /**
   * The selected attempt's key (`<activityId>:<task>:<n>`), or absent when the
   * selection is activity-level. An episode is only claimed for this task when
   * its `TargetRef` equals this EXACTLY.
   */
  attemptId?: string | undefined;
  selectedEpisodeId: string | null;
  onSelectEpisode: (episodeId: string | null) => void;
  timeline: EpisodeTimeline | undefined;
  timelineLoading: boolean;
  timelineError?: string | undefined;
  onExportJson: () => void;
  onExportCsv: () => void;
  exportPending?: boolean | undefined;
  exportError?: string | undefined;
}

export function EpisodeBody({
  episodes,
  isLoading,
  error,
  attemptId,
  selectedEpisodeId,
  onSelectEpisode,
  timeline,
  timelineLoading,
  timelineError,
  onExportJson,
  onExportCsv,
  exportPending,
  exportError,
}: EpisodeBodyProps): ReactElement {
  const t = useTokens();

  const scope = useMemo(
    () =>
      episodeScopeFor(
        episodes.map((e) => e.targetRef),
        attemptId
      ),
    [episodes, attemptId]
  );

  // Only narrow the list when something genuinely matched the attempt key.
  // Filtering on a guess would hide real records; not filtering on a real match
  // would bury the one episode that IS this task's.
  const shown = useMemo(
    () =>
      scope.showing === 'attributed'
        ? episodes.filter((e) => attributionOf(e.targetRef, attemptId) === 'attributed')
        : episodes,
    [episodes, scope.showing, attemptId]
  );

  return (
    <Box
      data-testid={UI_IDENTIFIERS.Construction.DETAIL_BODY_EPISODES}
      sx={{ display: 'flex', flexDirection: 'column', gap: 1 }}
    >
      <EpisodesPanel
        caption={scope.caption}
        episodes={shown}
        error={error}
        exportError={exportError}
        exportPending={exportPending}
        isLoading={isLoading}
        selectedEpisodeId={selectedEpisodeId}
        spanStrip={(episode) => <SubagentGantt episode={episode} />}
        timeline={timeline}
        timelineError={timelineError}
        timelineLoading={timelineLoading}
        onExportCsv={onExportCsv}
        onExportJson={onExportJson}
        onSelectEpisode={onSelectEpisode}
      />

      {scope.showing === 'attributed' && scope.total > scope.attributed ? (
        <Typography
          data-testid={UI_IDENTIFIERS.Construction.DETAIL_EPISODE_CAPTION}
          sx={{ fontFamily: t.mono, fontSize: 10, color: t.muted, lineHeight: 1.5 }}
        >
          {String(scope.total - scope.attributed)} further episode
          {scope.total - scope.attributed === 1 ? ' is' : 's are'} recorded against this activity
          but carry no task key, so they are not shown here.
        </Typography>
      ) : null}

      {!isLoading && error === undefined && scope.total === 0 ? (
        <Typography
          data-testid={UI_IDENTIFIERS.Construction.DETAIL_EPISODE_CAPTION}
          sx={{ fontFamily: t.mono, fontSize: 10, color: t.muted, lineHeight: 1.5 }}
        >
          {CAPTURE_DEFERRED_NOTE}
        </Typography>
      ) : null}
    </Box>
  );
}
