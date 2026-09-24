/**
 * The SPA container for the ACTIVITY EXPERIENCE
 * (`/project/$projectId/activity/$activityId?task=&rev=`) — one activity's whole
 * lifecycle on one full-screen surface (spec §7.2).
 *
 * It is the ONLY file in this feature that calls a hook. Everything beneath it —
 * `LifecycleGraph`, `TaskHeader`, `DispatchBody`, and Task 9's review body — is
 * pure and props-only, so the wire shape is translated ONCE (`activityViewToGraph`)
 * and the screen cannot end up with two disagreeing opinions about one task.
 *
 * ── The selection lives in the URL ──────────────────────────────────────────
 * `?task=` and `?rev=` ARE the selection. `useActivityView` polls every 2 s while
 * an agent works, and a selection held in React state beside that query is the
 * one that loses the race; in the address bar it survives every refetch, and a
 * link addresses exactly one revision of exactly one task. `selectionFor`
 * corrects what the URL asks for against what the activity actually has, so a
 * stale deep link opens the activity rather than an empty body.
 *
 * ── Two controls, one selection ─────────────────────────────────────────────
 * The graph's revision menu and the body's revision select both land in `select`.
 * A plain CLICK on another task goes through `revisionOnNavigate`, so reading
 * revision 2 of a draft and clicking its review opens revision 2 of the review —
 * the round that judged what is on screen. A PICK from either menu is honoured
 * as picked.
 *
 * ── Liveness ────────────────────────────────────────────────────────────────
 * `useActivityView` carries its own cadence (2 s while a task runs, 8 s at a
 * gate, stopped when terminal — `activityViewPolling.ts`); this file adds no
 * interval of its own. A 404 is an ERROR there, never a value, and is never
 * polled: it means the committed plan has no such activity, which the screen
 * says outright instead of spinning forever.
 */
import { useEffect, useRef, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import { useNavigate } from '@tanstack/react-router';

import { ExperienceChrome } from '../components/design/ExperienceChrome';
import { DispatchBody } from '../components/activity/DispatchBody';
import { LifecycleGraph } from '../components/activity/LifecycleGraph';
import { TaskHeader } from '../components/activity/TaskHeader';
import {
  ACTIVITY_LOADING,
  ACTIVITY_NOT_IN_PLAN,
  activityReadFailed,
  eyebrowFor,
  notDispatchedYet,
  planIndexFor,
  REVIEW_BODY_NOT_YET,
} from '../components/activity/activityCopy.ts';
import { selectionFor, type ActivitySelection } from '../components/activity/activitySelection.ts';
import {
  activityViewToGraph,
  taskFactsFor,
  type ActivityViewWire,
} from '../components/activity/activityViewToGraph.ts';
import { revisionOnNavigate } from '../components/activity/lifecycleGraphTypes.ts';
import { ACTIVITY_PATH, PLAN_PATH, activitySearch, planSearch } from '../contracts/routePaths.ts';
import { useActivityView } from '../hooks/useActivityView';
import { activityEpisodesManager } from '../hooks/activityEpisodesManager.ts';
import { useEpisodeTimeline } from '../hooks/useEpisodes';
import { isNoSessionError } from '../hooks/sessionPolling';
import { useTokens } from '../utilities/theme/ThemeContext';
import { UI_IDENTIFIERS } from '../utilities/constants/UIIdentifiers';

type RevisionWire = ActivityViewWire['tasks'][number]['revisions'][number];

/**
 * The RAW wire revision behind a selection. `activityViewToGraph` deliberately
 * drops `episodeId` and `attemptIds` — they are not graph vocabulary — but the
 * dispatch body needs both, so they are read off the wire here rather than
 * widened into the graph's props for one consumer.
 */
function revisionWireFor(
  view: ActivityViewWire | undefined,
  sel: ActivitySelection | undefined
): RevisionWire | undefined {
  if (sel === undefined) return undefined;
  return view?.tasks
    .find((taskView) => taskView.id === sel.taskId)
    ?.revisions.find((r) => r.n === sel.revision);
}

export function ActivityExperienceContainer({
  projectId,
  activityId,
  task,
  rev,
}: {
  projectId: string;
  activityId: string;
  task: string | undefined;
  rev: number | undefined;
}): ReactNode {
  const t = useTokens();
  const navigate = useNavigate();
  const { data: view, error, isLoading } = useActivityView(projectId, activityId);

  const graph = view === undefined ? undefined : activityViewToGraph(view);
  const nodes = graph?.nodes ?? [];
  const sel = selectionFor(nodes, task, rev);
  const node = sel === undefined ? undefined : nodes.find((n) => n.id === sel.taskId);
  const facts = view !== undefined && sel !== undefined ? taskFactsFor(view, sel.taskId) : {};
  const revisionWire = revisionWireFor(view, sel);

  // ONE timeline: the episode of the attempt that reached the gate (else the
  // latest) for the revision on screen. A review revision carries no episodeId at
  // all, and the hook stays disabled on `undefined` rather than firing a read for
  // an empty id. The manager is NOT always `construction`: activities 1–3 are
  // design activities whose episodes live in the systemDesign / projectDesign
  // ledgers, and the timeline op differs per manager (activityEpisodesManager.ts).
  const timeline = useEpisodeTimeline(
    {
      projectId,
      manager: activityEpisodesManager(view?.type ?? ''),
      targetRef: activityId,
    },
    revisionWire?.episodeId
  );

  // Focus follows the content: opening the screen, and moving to another task,
  // lands the caret on the body region so a keyboard reader is not left behind in
  // the chrome. Deliberately NOT keyed on the revision — picking one in the
  // select would then yank focus off the control that was just used — and
  // `preventScroll` so the jump does not fight the shared scroller.
  const bodyRef = useRef<HTMLElement>(null);
  const selectedTask = sel?.taskId;
  useEffect(() => {
    if (selectedTask === undefined) return;
    bodyRef.current?.focus({ preventScroll: true });
  }, [selectedTask]);

  const go = (taskId: string, revision: number): void => {
    void navigate({
      to: ACTIVITY_PATH,
      params: { projectId, activityId },
      search: () => activitySearch(taskId, revision),
    });
  };

  // A PICK is honoured verbatim; a plain click carries the revision being read
  // across a draft↔review pair and otherwise opens the target's latest.
  const select = (nodeId: string, revision: number, picked: boolean): void => {
    if (picked || sel === undefined) {
      go(nodeId, revision);
      return;
    }
    go(nodeId, revisionOnNavigate(nodes, { nodeId: sel.taskId, revision: sel.revision }, nodeId));
  };

  const eyebrow =
    view === undefined
      ? `${activityId.toUpperCase()} · ACTIVITY`
      : eyebrowFor({
          activityId,
          type: view.type,
          variant: view.variant,
          planIndex: planIndexFor(view.type),
        });

  return (
    <ExperienceChrome
      bodyScroll="shared"
      eyebrow={eyebrow}
      eyebrowTestId={UI_IDENTIFIERS.Activity.EYEBROW}
      // An activity is not a phase — the eyebrow above says so — but the prop is
      // required for the DEFAULT eyebrow this caller never uses.
      phaseNum={0}
      phaseTitle={view?.name ?? activityId}
      projectName={projectId}
      spine={
        graph !== undefined && sel !== undefined && nodes.length > 0 ? (
          <LifecycleGraph
            nodes={nodes}
            phases={graph.phases}
            selected={{ nodeId: sel.taskId, revision: sel.revision }}
            onSelect={select}
          />
        ) : undefined
      }
      // Task 11 gives the plan a remembered lens; until then ✕ lands on the LIST.
      onClose={() =>
        void navigate({ to: PLAN_PATH, params: { projectId }, search: () => planSearch('list') })
      }
    >
      <Box
        aria-label={node?.title ?? activityId}
        component="section"
        data-testid={UI_IDENTIFIERS.Activity.SCREEN}
        ref={bodyRef}
        sx={{ flexGrow: 1, minWidth: 0, maxWidth: 1100, mx: 'auto', px: { xs: 2, md: 4 }, py: 3 }}
        tabIndex={-1}
      >
        {error !== null ? (
          <Paper data-testid={UI_IDENTIFIERS.Common.ERROR_ALERT} sx={{ p: 3 }}>
            <Typography sx={{ fontSize: 13.5, color: t.ink, lineHeight: 1.5 }}>
              {isNoSessionError(error) ? ACTIVITY_NOT_IN_PLAN : activityReadFailed(error.message)}
            </Typography>
          </Paper>
        ) : isLoading || view === undefined ? (
          <Typography sx={{ fontFamily: t.mono, fontSize: 12, color: t.muted }}>
            {ACTIVITY_LOADING}
          </Typography>
        ) : sel === undefined || node === undefined ? (
          <Typography sx={{ fontFamily: t.mono, fontSize: 12, color: t.muted }}>
            {ACTIVITY_NOT_IN_PLAN}
          </Typography>
        ) : node.kind === 'dispatch' ? (
          <DispatchBody
            attemptIds={revisionWire?.attemptIds ?? []}
            emptyNote={
              revisionWire === undefined ? notDispatchedYet(node.state === 'locked') : undefined
            }
            facts={facts}
            revision={sel.revision}
            revisions={node.revisions}
            // The SELECTED revision is being produced — not merely "the task is
            // running". Reading revision 1 of a task whose revision 2 is in
            // flight must show revision 1's episode, not a generating scene over
            // work that is not the work on screen.
            running={
              revisionWire?.outcome === 'running' ||
              (revisionWire === undefined && node.state === 'running')
            }
            timeline={timeline.data}
            timelineError={timeline.error}
            timelineLoading={timeline.isLoading}
            title={node.title}
            onRevision={(n) => {
              go(node.id, n);
            }}
          />
        ) : (
          // Task 9 replaces this with the real review body (the reviewers strip,
          // the artifact panel and the decision bar). The header above it is the
          // shipped one, so the revision select and the read-only history it
          // drives are already live for a review task.
          <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2.5 }}>
            <TaskHeader
              facts={facts}
              revision={sel.revision}
              revisions={node.revisions}
              title={node.title}
              onRevision={(n) => {
                go(node.id, n);
              }}
            />
            <Paper sx={{ p: 3 }}>
              <Typography sx={{ fontFamily: t.mono, fontSize: 12, color: t.muted }}>
                {REVIEW_BODY_NOT_YET}
              </Typography>
            </Paper>
          </Box>
        )}
      </Box>
    </ExperienceChrome>
  );
}
