/**
 * The body of an AGENTIC DISPATCH task: what was asked of which agent, then the
 * ONE episode that produced the revision on screen.
 *
 * There is no episode LIST here, and that is the point. Which revision you are
 * reading is the header's revision select; the episode below it is that
 * revision's, and only that revision's. The old construction console offered a
 * list of episodes and left the reader to work out which one made the artifact
 * they were looking at — the coupling this body makes structural.
 *
 * ── Attempts are not revisions ──────────────────────────────────────────────
 * A revision may have taken several ATTEMPTS (a failure, then a retry) before it
 * reached the gate. The artifact did not change between them, so they are not
 * revisions and they do not get a control of their own: a single line says how
 * many there were, and only when there was more than one. The episode shown is
 * the one the ledger points at — the attempt that reached the gate, else the
 * latest (`ConstructionTaskRevisionView.episodeId`).
 *
 * Ported from the prototype's `DispatchBody.tsx`, minus the whole of its
 * fixture→wire fabrication: every `EpisodeRecordView` and `TimelineEvent` it
 * built by hand is now real data the container fetched. What IS kept is its
 * facts strip above the timeline (`episodeFacts.ts`) — `EpisodeTimeline` renders
 * events and nothing else, so without it the episode's duration, model, token
 * spend and turn count would be on screen nowhere.
 *
 * Pure and props-only: the container owns every fetch.
 */
import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';

import { TaskHeader } from './TaskHeader';
import {
  DISPATCH_JOB_NOTE,
  dispatchRoleLine,
  NO_EPISODE_CAPTURED,
  subAttemptsLine,
} from './activityCopy.ts';
import { episodeFactsFor } from './episodeFacts.ts';
import type { TaskFacts } from './activityViewToGraph.ts';
import type { LifecycleRevision } from './lifecycleGraphTypes.ts';
import { EpisodeTimeline as EpisodeTimelineView } from '../episodes/EpisodeTimeline';
import { GeneratingScene } from '../design/GeneratingScene';
import type { EpisodeTimeline } from '../../contracts/types';
import { useTokens } from '../../utilities/theme/ThemeContext';
import { UI_IDENTIFIERS } from '../../utilities/constants/UIIdentifiers';

export function DispatchBody({
  title,
  facts,
  revisions,
  revision,
  onRevision,
  timeline,
  timelineLoading,
  timelineError,
  running,
  attemptIds,
  /**
   * Why there is nothing to show when there is nothing to show — the container
   * knows whether the task is locked or merely not dispatched, and this body
   * does not carry the task's state to work it out for itself.
   */
  emptyNote,
}: {
  title: string;
  facts: TaskFacts;
  revisions: readonly LifecycleRevision[];
  revision: number;
  onRevision: (n: number) => void;
  /** The selected revision's episode timeline, already fetched by the container. */
  timeline: EpisodeTimeline | undefined;
  timelineLoading: boolean;
  timelineError: Error | null;
  /** Running ⇒ the generating scene instead of a timeline. */
  running: boolean;
  /** Attempt ids of the selected revision — the sub-attempt line shows only above 1. */
  attemptIds: readonly string[];
  emptyNote: string | undefined;
}): ReactNode {
  const t = useTokens();
  const subAttempts = subAttemptsLine(attemptIds.length);
  // Neither running nor fetching nor failed, and still no timeline: the revision
  // reached the gate with no episode behind it (a backfilled or reconstructed
  // row). Said out loud — an empty timeline reads as "the agent did nothing".
  const noEpisode =
    !running && !timelineLoading && timelineError === null && timeline === undefined;

  return (
    <Box
      data-testid={UI_IDENTIFIERS.Activity.DISPATCH_BODY}
      sx={{ display: 'flex', flexDirection: 'column', gap: 2.5 }}
    >
      <TaskHeader
        facts={facts}
        revision={revision}
        revisions={revisions}
        title={title}
        onRevision={onRevision}
      />

      {running ? (
        <GeneratingScene
          artifact={title}
          footerNote={DISPATCH_JOB_NOTE}
          roleLine={
            facts.workerClass !== undefined ? dispatchRoleLine(facts.workerClass, title) : undefined
          }
        />
      ) : emptyNote !== undefined ? (
        <Paper sx={{ p: 3, textAlign: 'center' }}>
          <Typography sx={{ fontFamily: t.mono, fontSize: 12, color: t.muted }}>
            {emptyNote}
          </Typography>
        </Paper>
      ) : noEpisode ? (
        <Paper sx={{ p: 3, textAlign: 'center' }}>
          <Typography sx={{ fontFamily: t.mono, fontSize: 12, color: t.muted }}>
            {NO_EPISODE_CAPTURED}
          </Typography>
        </Paper>
      ) : (
        <Paper sx={{ p: 0, overflow: 'hidden' }}>
          {timeline !== undefined ? (
            <Box
              sx={{
                px: 2.5,
                py: 1.75,
                bgcolor: t.paperAlt,
                borderBottom: `1.5px solid ${t.line}`,
                display: 'flex',
                alignItems: 'flex-start',
                gap: 3,
                flexWrap: 'wrap',
              }}
            >
              <Typography
                sx={{
                  fontFamily: t.mono,
                  fontWeight: 700,
                  fontSize: 12,
                  letterSpacing: '0.1em',
                  color: t.ink,
                }}
              >
                {`EPISODE · REVISION ${String(revision)}`}
              </Typography>
              {episodeFactsFor(timeline.record).map((f) => (
                <Box key={f.label} sx={{ minWidth: 0 }}>
                  <Typography
                    sx={{
                      fontFamily: t.mono,
                      fontSize: 9.5,
                      letterSpacing: '0.14em',
                      color: t.muted,
                    }}
                  >
                    {f.label}
                  </Typography>
                  <Typography
                    sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 13, color: t.ink }}
                  >
                    {f.value}
                  </Typography>
                </Box>
              ))}
            </Box>
          ) : null}
          <Box sx={{ p: 1.5 }}>
            <EpisodeTimelineView
              error={timelineError?.message}
              loading={timelineLoading}
              timeline={timeline}
            />
          </Box>
        </Paper>
      )}

      {subAttempts.length > 0 ? (
        <Typography
          data-testid={UI_IDENTIFIERS.Activity.SUB_ATTEMPTS}
          sx={{ fontFamily: t.mono, fontSize: 12, color: t.muted }}
        >
          {subAttempts}
        </Typography>
      ) : null}
    </Box>
  );
}
