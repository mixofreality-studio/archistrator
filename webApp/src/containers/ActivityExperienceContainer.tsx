/**
 * The SPA container for the ACTIVITY EXPERIENCE
 * (`/project/$projectId/activity/$activityId?task=&rev=`) — one activity's whole
 * lifecycle on one full-screen surface (spec §7.2).
 *
 * It is the ONLY file in this feature that calls a hook. Everything beneath it —
 * `LifecycleGraph`, `TaskHeader`, `DispatchBody`, `ReviewBody` — is pure and
 * props-only, so the wire shape is translated ONCE (`activityViewToGraph`) and
 * the screen cannot end up with two disagreeing opinions about one task.
 *
 * ── The selection lives in the URL ──────────────────────────────────────────
 * `?task=` and `?rev=` ARE the selection. `useActivityView` polls every 2 s while
 * an agent works, and a selection held in React state beside that query is the
 * one that loses the race; in the address bar it survives every refetch.
 * `selectionFor` corrects what the URL asks for against what the activity
 * actually has, so a stale deep link opens the activity rather than an empty body.
 *
 * `?rev` is OMITTED whenever the selection is the task's LATEST revision
 * (`revisionParam`): a URL with no `rev` means "follow the head", and only a
 * deliberately picked older revision writes one. Emitting it unconditionally
 * pinned the reader to revision N, so the moment the agent finished N+1 under the
 * poll the screen they were already reading turned into a read-only history of N
 * — history nobody asked for, hiding work that had just landed.
 *
 * ── Two controls, one selection ─────────────────────────────────────────────
 * The graph's revision menu and the body's revision select both land in `select`.
 * A plain CLICK on another task goes through `revisionOnNavigate`, so reading
 * revision 2 of a draft and clicking its review opens revision 2 of the review —
 * the round that judged what is on screen. A PICK from either menu is honoured
 * as picked.
 *
 * ── TWO reads, and why the second one has no cadence of its own ─────────────
 * `useActivityView` is the LIFECYCLE (tasks, revisions, the live review set) and
 * carries its own cadence (2 s while a task runs, 8 s at a gate, stopped when
 * terminal — `activityViewPolling.ts`). `useProject` is the ARTIFACT: the
 * committed slots a design review judges, the construction row a renderer draws
 * from, the service contracts the JOIN resolves. None of that changes between
 * two ticks of an agent's turn — it changes when a gate is decided, and every
 * mutation below invalidates it — so it is read once, with NO refetchInterval.
 *
 * ── Which op each verb fires ────────────────────────────────────────────────
 * `verbsFor` (activityVerbs.ts) is the pure table; this file is the only place
 * allowed to turn one of its targets into a hook call. Three rails answer the
 * same four verbs and two of them are missing ops the third has, which is why
 * the rule is written down and tested rather than inlined as a switch nobody
 * can read.
 *
 * ── The read-only history (R1) ──────────────────────────────────────────────
 * On a non-latest revision the whole screen goes read-only: a banner over the
 * body with the one way back, a `CommentProvider` with `enabled={false}` wrapped
 * around EVERYTHING (the margin included — the chrome renders it, so a provider
 * around the body alone would not reach it), the margin's resolve/reopen omitted
 * and its resolved threads expanded, no submit bar, and the artifact under a
 * caption saying it is the CURRENT one. There is no op that reads an artifact as
 * of a ref (GAP-5); saying so is honest where a silently-current artifact is not.
 *
 * ── Liveness ────────────────────────────────────────────────────────────────
 * A 404 from the activity read is an ERROR there, never a value, and is never
 * polled: it means the committed plan has no such activity, which the screen
 * says outright instead of spinning forever.
 */
import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import { useNavigate } from '@tanstack/react-router';

import { ExperienceChrome } from '../components/design/ExperienceChrome';
import { CommentMargin } from '../components/design/CommentMargin';
import { CommentProvider, useComments } from '../components/comments/CommentContext';
import { DispatchBody } from '../components/activity/DispatchBody';
import { HistoryBanner } from '../components/activity/HistoryBanner';
import { LifecycleGraph } from '../components/activity/LifecycleGraph';
import { ReviewBody } from '../components/activity/ReviewBody';
import {
  ACTIVITY_LOADING,
  ACTIVITY_NOT_IN_PLAN,
  activityReadFailed,
  eyebrowFor,
  HISTORY_ARTIFACT_CAPTION,
  notDispatchedYet,
  planIndexFor,
  RECONCILE_RATIONALE,
} from '../components/activity/activityCopy.ts';
import { activityCommentKey } from '../components/activity/pendingCommentKey.ts';
import { lastPlanLens } from '../components/activity/planLensMemory.ts';
import {
  isHistorical,
  revisionParam,
  selectionFor,
  type ActivitySelection,
} from '../components/activity/activitySelection.ts';
import {
  activityViewToGraph,
  taskFactsFor,
  type ActivityViewWire,
} from '../components/activity/activityViewToGraph.ts';
import { latestRevision, revisionOnNavigate } from '../components/activity/lifecycleGraphTypes.ts';
import { openThreadCount, toReviewThread } from '../components/activity/threadAdapter.ts';
import { foldCommentsIntoNotes } from '../components/comments/reviewBatch.ts';
import {
  ARCHITECTURE_ACTIVITY_ID,
  taskArtifactFor,
} from '../components/activity/taskArtifactFor.ts';
import { verbsFor, type VerbTarget } from './activityVerbs.ts';
import { toC4View } from '../contracts/adapters';
import { narrowProject } from '../contracts/projectAdapters';
import { contractJoinFor } from '../contracts/serviceContracts';
import type { ProjectArtifactModelEnvelope } from '../contracts/types';
import { ApiError } from '../contracts/errors';
import { SDP_REVIEW_KIND } from '../contracts/types';
import { ACTIVITY_PATH, PLAN_PATH, activitySearch, planSearch } from '../contracts/routePaths.ts';
import { useActivityView } from '../hooks/useActivityView';
import { activityEpisodesManager } from '../hooks/activityEpisodesManager.ts';
import { useEpisodeTimeline } from '../hooks/useEpisodes';
import { useProject } from '../hooks/useProject';
import { isNoSessionError } from '../hooks/sessionPolling';
import { useOverrideActivity, useSubmitPhaseDecision } from '../hooks/useConstructionMutations';
import {
  useAcknowledgeStaleBasis,
  useAskQuestions,
  useRequestArtifactDraft,
  useSetReviewCommentStatus,
  useSubmitReviewDecision,
} from '../hooks/useDesignMutations';
import {
  useAcknowledgeProjectStaleBasis,
  useAdvanceToConstruction,
  useProjectAskQuestions,
  useSetProjectReviewCommentStatus,
  useSubmitSDPDecision,
} from '../hooks/useProjectDesignMutations';
import { useTokens } from '../utilities/theme/ThemeContext';
import { UI_IDENTIFIERS } from '../utilities/constants/UIIdentifiers';

type RevisionWire = ActivityViewWire['tasks'][number]['revisions'][number];

/**
 * The RAW wire revision behind a selection. `activityViewToGraph` deliberately
 * drops `episodeId`, `attemptIds`, `thread`, `reviewers` and `verdicts` — none of
 * them is graph vocabulary — but the two bodies need all five, so they are read
 * off the wire here rather than widened into the graph's props for one consumer.
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
  const {
    comments,
    pendingQuestions,
    reset,
    toWire,
    freeformNotes,
    requestId,
    setAnchor,
    setActiveKey,
  } = useComments();
  const { data: view, error, isLoading } = useActivityView(projectId, activityId);
  // The ARTIFACT read. No refetchInterval: see the file header.
  const { data: project } = useProject(projectId);

  const graph = view === undefined ? undefined : activityViewToGraph(view);
  const nodes = graph?.nodes ?? [];
  const sel = selectionFor(nodes, task, rev);
  const node = sel === undefined ? undefined : nodes.find((n) => n.id === sel.taskId);
  const facts = view !== undefined && sel !== undefined ? taskFactsFor(view, sel.taskId) : {};
  const revisionWire = revisionWireFor(view, sel);
  // Reading HISTORY: everything that would change something goes away below, and
  // the one navigation back goes up. `latest` is what the banner counts against.
  const latest = node === undefined ? 0 : latestRevision(node);
  const historical = sel !== undefined && isHistorical(nodes, sel);

  const slots = project?.slots ?? [];
  const systemEnvelope = slots.find((s) => s.kind === 'system')?.model;
  const row = project?.constructionRows?.[activityId];
  // `ArtifactRendererProps.vm` requires a row. A planned activity that has never
  // been dispatched has none, and the artifact panel degrades to its honest
  // panel rather than handing a renderer an empty frame to draw.
  const vm = row === undefined ? undefined : { activityId, name: view?.name ?? activityId, row };

  // The activity → component → contractKey → contract JOIN, which is what puts
  // the real ServiceContractView (and with it ContractCodeFlow) under a service
  // activity's design review. The committed activity list's `componentId` is the
  // first hop; there is deliberately no name heuristic anywhere on this path.
  const activityListEnvelope = slots.find((s) => s.kind === 'activityList')?.model as unknown as
    | ProjectArtifactModelEnvelope
    | undefined;
  const serviceContracts = project?.serviceContracts;
  const contractJoin = useMemo(
    () =>
      contractJoinFor(
        {
          activities: narrowProject(activityListEnvelope, 'activityList')?.activities ?? undefined,
          components: toC4View(systemEnvelope).components,
          contracts: serviceContracts,
        },
        activityId
      ),
    [activityListEnvelope, systemEnvelope, serviceContracts, activityId]
  );

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

  // --- write ops, one per rail (activityVerbs.ts names which) ---------------
  const submitPhase = useSubmitPhaseDecision(projectId);
  const submitDesign = useSubmitReviewDecision(projectId);
  const submitSdp = useSubmitSDPDecision(projectId);
  const advance = useAdvanceToConstruction(projectId);
  const setDesignCommentStatus = useSetReviewCommentStatus(projectId);
  const setProjectCommentStatus = useSetProjectReviewCommentStatus(projectId);
  const askQuestionsMut = useAskQuestions(projectId);
  const askProjectMut = useProjectAskQuestions(projectId);
  const requestDraft = useRequestArtifactDraft(projectId);
  const overrideActivity = useOverrideActivity(projectId);
  const acknowledgeStale = useAcknowledgeStaleBasis(projectId);
  const acknowledgeProjectStale = useAcknowledgeProjectStaleBasis(projectId);

  // The option the M0 bar will commit. `SdpReviewView` reports its standing
  // choice (its own recommendation, until the reader picks another) — an approve
  // that did not name an option would be committing a plan nobody chose.
  const [sdpOption, setSdpOption] = useState('');

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

  // An anchor armed on one task must never bleed onto the next: it would file
  // the comment against a location the reader is no longer looking at.
  useEffect(() => {
    setAnchor(null);
  }, [selectedTask, setAnchor]);

  // Bind the pending-comment accumulator to this (project, activity, task,
  // REVISION) slot (R10) so unsent notes survive a reload, swap when the reader
  // changes task, and — because the revision is in the key — do not follow the
  // reader from the draft they were written about onto its successor. The
  // head-state Version is the incarnation stamp that invalidates drafts left by
  // a previous incarnation of the same project (pendingCommentsStore.ts).
  // `setActiveKey` short-circuits on an unchanged (key, version), so this is safe
  // on every poll tick.
  const projectVersion = project?.version;
  const selectedRevision = sel?.revision;
  useEffect(() => {
    if (projectVersion === undefined || selectedTask === undefined) return;
    setActiveKey(
      activityCommentKey({
        projectId,
        activityId,
        taskId: selectedTask,
        revision: selectedRevision ?? 0,
      }),
      projectVersion
    );
  }, [projectId, activityId, selectedTask, selectedRevision, projectVersion, setActiveKey]);

  // The margin auto-opens whenever an anchor is armed (requestId bumps); a manual
  // collapse records the requestId it happened at, and a newer anchor re-opens it.
  const [closedAt, setClosedAt] = useState<number | null>(null);
  const marginOpen = closedAt === null || requestId > closedAt;

  // `revisionParam` is what decides whether the URL carries `rev` at all: the
  // latest writes none (the address then means "follow the head"), an older one
  // writes itself. Every navigation on this screen — the graph, both revision
  // selects and Back to latest — goes through here, so there is one rule.
  const go = (taskId: string, revision: number): void => {
    void navigate({
      to: ACTIVITY_PATH,
      params: { projectId, activityId },
      search: () => activitySearch(taskId, revisionParam(nodes, taskId, revision)),
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

  const verbs = verbsFor({
    type: view?.type ?? '',
    variant: view?.variant,
    taskId: sel?.taskId ?? '',
    lifecyclePhase: node?.phase ?? '',
    artifactKind: facts.artifactKind,
  });
  const artifact = taskArtifactFor({
    type: view?.type ?? '',
    variant: view?.variant,
    taskId: sel?.taskId ?? '',
    componentId: view?.componentId,
    artifactKind: facts.artifactKind,
  });

  const thread = toReviewThread(revisionWire?.thread);
  const openThreads = openThreadCount(thread);
  const questionCount = pendingQuestions().length;
  const changeRequestCount = comments.length - questionCount;
  const canSetCommentStatus = verbs.commentStatus.kind !== 'none';
  // This rail has a question op at all. Both design phases do; construction does
  // not (R2/GAP-6). It gates BOTH the composer's Question toggle and the bar's
  // Ask verb, so a question can never be staged where pressing Ask would dispatch
  // nothing — and the bar can never lose Approve/Send back to an Ask that does.
  const canAsk = verbs.ask.kind !== 'none';

  /** Approve or send back, on whichever rail this activity's type names. */
  const decide = (approve: boolean): void => {
    const target: VerbTarget = approve ? verbs.approve : verbs.sendBack;
    const notes = freeformNotes();
    const wireComments = toWire();
    switch (target.kind) {
      case 'constructionPhaseDecision':
        submitPhase.mutate(
          {
            activityId,
            phase: target.lifecyclePhase,
            decision: approve ? 'approve' : 'sendBack',
            feedback: {
              notes: notes.length > 0 ? notes : wireComments.map((c) => c.text).join('\n'),
              comments: wireComments.map((c) => ({
                jsonPath: c.jsonPath,
                replyTo: c.replyTo,
                text: c.text,
              })),
            },
          },
          {
            onSuccess: () => {
              reset();
            },
          }
        );
        return;
      case 'designReviewDecision':
        submitDesign.mutate(
          {
            kind: target.artifactKind,
            decision: approve ? 'approve' : 'reject',
            // The Manager requires non-empty reject feedback; when the reviewer
            // only anchored comments, the notes are synthesized from them so the
            // redraft always carries actionable guidance (SystemDesignContainer
            // keeps the same rule).
            detail: {
              feedback: notes.length > 0 ? notes : wireComments.map((c) => c.text).join('\n'),
              comments: wireComments,
            },
          },
          {
            onSuccess: () => {
              reset();
            },
          }
        );
        return;
      case 'sdpDecision':
        // Approve IS commit-then-advance (spec §6): the option binds the plan of
        // record, and the advance is what unlocks construction.
        //
        // `SubmitSDPDecision`'s body carries ONE feedback field (`notes`) and no
        // `comments` array, so the anchored comments are FOLDED into it. Without
        // that fold, every comment a reviewer pinned to an option or an activity
        // row was dropped on the floor by the `reset()` below — staged, counted on
        // the bar, and then gone, with the approval recording none of it.
        submitSdp.mutate(
          {
            decision: 'commit',
            detail: {
              optionId: sdpOption,
              feedback: foldCommentsIntoNotes(notes, wireComments),
            },
          },
          {
            onSuccess: () => {
              reset();
              advance.mutate(false);
            },
          }
        );
        return;
      case 'projectAsk':
      case 'none':
        return;
    }
  };

  /** Re-run the work this gate judges — a redraft, or another construction attempt. */
  const rerun = (): void => {
    switch (verbs.rerun.kind) {
      case 'designReviewDecision':
        requestDraft.mutate({ kind: verbs.rerun.artifactKind });
        return;
      case 'constructionPhaseDecision':
        overrideActivity.mutate({ activityId, kind: 'retry' });
        return;
      case 'sdpDecision':
      case 'projectAsk':
      case 'none':
        return;
    }
  };

  /**
   * Send the staged questions, grouped by addressee — one batch per role, because
   * the op addresses a whole batch to one role. BOTH design phases have the op
   * (Phase 1 `systemDesignAskQuestions`, Phase 2 `projectDesignAskQuestions`);
   * construction has neither, and `verbs.ask` is `none` there, which is also what
   * takes the Ask verb off its bar and the Question toggle out of its composer.
   */
  const askQuestions = (): void => {
    const target = verbs.ask;
    if (target.kind !== 'designReviewDecision' && target.kind !== 'projectAsk') return;
    const pending = pendingQuestions();
    if (pending.length === 0) return;
    const byAddressee = new Map<'pm' | 'architect', typeof pending>();
    for (const q of pending) {
      const key: 'pm' | 'architect' = q.addressee === 'architect' ? 'architect' : 'pm';
      byAddressee.set(key, [...(byAddressee.get(key) ?? []), q]);
    }
    for (const [addressee, group] of byAddressee) {
      const questions = group.map((q) => ({
        jsonPath: q.jsonPath,
        text: q.text,
        anchorText: q.anchorText,
        replyTo: q.replyTo,
      }));
      if (target.kind === 'projectAsk') {
        askProjectMut.mutate({ kind: target.artifactKind, addressee, questions });
        continue;
      }
      askQuestionsMut.mutate({ kind: target.artifactKind, addressee, questions });
    }
  };

  const setCommentStatus = (commentID: string, status: 'open' | 'resolved'): void => {
    const target = verbs.commentStatus;
    if (target.kind === 'designReviewDecision') {
      setDesignCommentStatus.mutate({ kind: target.artifactKind, commentID, status });
      return;
    }
    if (target.kind === 'sdpDecision') {
      setProjectCommentStatus.mutate({ kind: SDP_REVIEW_KIND, commentID, status });
    }
  };

  const statusPending = setDesignCommentStatus.isPending || setProjectCommentStatus.isPending;
  const decisionPending = submitPhase.isPending || submitDesign.isPending || submitSdp.isPending;

  // ── The stale basis of the committed slot this gate judges ────────────────
  // Only a SLOT can be stale: `staleBasis` is an `ArtifactSlotView` flag, so this
  // reaches the design rails' gates and nothing else. It is advisory and never
  // blocks — both exits below are offered, neither is required.
  const staleSlot =
    artifact.kind === 'slot' ? slots.find((s) => s.kind === artifact.artifactKind) : undefined;

  /** Reconcile by AMENDING: a redraft on the design rails, the Architecture on M0. */
  const reconcileStale = (): void => {
    switch (verbs.approve.kind) {
      case 'designReviewDecision':
        requestDraft.mutate({ kind: verbs.approve.artifactKind, feedback: RECONCILE_RATIONALE });
        return;
      case 'sdpDecision':
        // The M0 plan is DERIVED (spec §6/R7): it is reconciled by amending what
        // it derives from, which is the same navigation the gate's own link makes.
        void navigate({
          to: ACTIVITY_PATH,
          params: { projectId, activityId: ARCHITECTURE_ACTIVITY_ID },
          search: () => ({}),
        });
        return;
      case 'constructionPhaseDecision':
      case 'projectAsk':
      case 'none':
        return;
    }
  };

  /** The other exit: reviewed — unaffected. Clears StaleBasis with an audit note. */
  const acknowledgeStaleBasis = (staleNote: string): void => {
    switch (verbs.approve.kind) {
      case 'designReviewDecision':
        acknowledgeStale.mutate({ kind: verbs.approve.artifactKind, note: staleNote });
        return;
      case 'sdpDecision':
        acknowledgeProjectStale.mutate({ kind: SDP_REVIEW_KIND, note: staleNote });
        return;
      case 'constructionPhaseDecision':
      case 'projectAsk':
      case 'none':
        return;
    }
  };

  // ── The M0 advance, when it fails on its own ──────────────────────────────
  // Approve is commit-then-advance. A failed advance leaves the SDP committed and
  // construction NOT started, and by then the gate is decided so the bar is gone:
  // without this the screen would show nothing at all and read as success.
  const advanceError = advance.error;
  const advanceSurface =
    advanceError === null
      ? undefined
      : {
          error: advanceError.message,
          // The F55 refusal: committed slots drifted since they were sealed. The
          // Project Design experience answers it with "advance anyway", and this
          // gate offers the identical acknowledge-and-seal.
          stale: advanceError instanceof ApiError && advanceError.code === 'failed_precondition',
          pending: advance.isPending,
          onRetry: (): void => {
            advance.mutate(false);
          },
          onAdvanceAnyway: (): void => {
            advance.mutate(true);
          },
        };

  // The margin is a FACTORY, not a node: the scroll container its anchor offsets
  // are measured against is owned by the chrome. Mounted for a REVIEW task only —
  // a dispatch body has no artifact to anchor a comment on.
  const margin =
    node?.kind === 'review' && marginOpen
      ? (scrollRoot: HTMLElement | null): ReactNode => (
          <CommentMargin
            // A question cannot be STAGED where it could never be SENT: the
            // construction rail has no AskQuestions op (R2/GAP-6), so its composer
            // renders no Question toggle at all. The bar's own `allowAsk` is the
            // other half of the same fact.
            allowQuestions={canAsk}
            // On a read-only history a DECIDED thread is the point of the history,
            // not noise in it, so resolved cards stay open instead of collapsing
            // to a one-liner nobody can act on anyway.
            expandResolved={historical}
            scrollRoot={scrollRoot}
            statusPending={statusPending}
            thread={thread}
            onCollapse={() => {
              setClosedAt(requestId);
            }}
            // Omitted where no op exists (R2: the construction rail has no
            // comment-status op) and on a read-only history, where resolving a
            // past round's thread is not a thing a reader may do — the documented
            // posture for a surface with no mutation, rather than buttons that
            // would fail.
            {...(canSetCommentStatus && !historical
              ? {
                  onReopen: (id: string): void => {
                    setCommentStatus(id, 'open');
                  },
                  onResolve: (id: string): void => {
                    setCommentStatus(id, 'resolved');
                  },
                }
              : {})}
          />
        )
      : undefined;

  const screen = (
    <ExperienceChrome
      bodyScroll="shared"
      eyebrow={eyebrow}
      eyebrowTestId={UI_IDENTIFIERS.Activity.EYEBROW}
      margin={margin}
      marginOpen={marginOpen}
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
      // ✕ returns to the plan AS THE READER LEFT IT (spec §7.3): the lens comes
      // from the plan's own module memory, not from this screen's search — the
      // activity URL carries `task`/`rev` and nothing else, and adding a `lens`
      // it never reads would put a second, stale copy of the plan's state in
      // every activity link.
      onClose={() =>
        void navigate({
          to: PLAN_PATH,
          params: { projectId },
          search: () => planSearch(lastPlanLens()),
        })
      }
      onOpenMargin={() => {
        setClosedAt(null);
      }}
    >
      <Box
        aria-label={node?.title ?? activityId}
        component="section"
        data-testid={UI_IDENTIFIERS.Activity.SCREEN}
        ref={bodyRef}
        sx={{
          flexGrow: 1,
          minWidth: 0,
          maxWidth: 1100,
          mx: 'auto',
          px: { xs: 2, md: 4 },
          py: 3,
          display: 'flex',
          flexDirection: 'column',
          gap: 2.5,
        }}
        tabIndex={-1}
      >
        {/* Above BOTH bodies: which revision you are on is a fact about the
            selection, not about the task kind. `historical` already carries
            `sel !== undefined` (there is no history without a selection), which
            is why it narrows `sel` here on its own. */}
        {historical ? (
          <HistoryBanner
            latest={latest}
            revision={sel.revision}
            onBackToLatest={() => {
              go(sel.taskId, latest);
            }}
          />
        ) : null}

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
          <ReviewBody
            advance={advanceSurface}
            allowAsk={canAsk}
            allowSendBack={verbs.allowSendBack}
            approveCopy={verbs.approveCopy}
            artifact={artifact}
            askPending={askQuestionsMut.isPending || askProjectMut.isPending}
            contractJoin={contractJoin}
            decisionPending={decisionPending}
            facts={facts}
            // What is rendered is the CURRENT artifact, not the one this revision
            // judged: there is no op that reads an artifact as of `subjectRef.ref`
            // (GAP-5). The caption says so, and its presence is what puts the
            // panel in read-only mode.
            historyCaption={historical ? HISTORY_ARTIFACT_CAPTION : undefined}
            // A decision is owed on THIS revision — not merely "the task is at a
            // gate" — AND this rail can take one. An earlier, decided round
            // offers no bar; neither does a gate whose artifact kind would not
            // resolve, because every button on it would be a no-op.
            live={
              !historical &&
              revisionWire?.outcome === 'awaitingHuman' &&
              verbs.approve.kind !== 'none'
            }
            note={revisionWire?.note}
            openThreads={openThreads}
            project={project}
            // The LIVE proposal belongs to the gate the activity is waiting at
            // now, which is not the round on screen when the round on screen is
            // past. The roster and the verdicts below ARE that round's, and stay.
            reviewSet={historical ? undefined : view.reviewSet}
            reviewSetError={historical ? undefined : view.reviewSetError}
            revision={sel.revision}
            revisions={node.revisions}
            roster={revisionWire?.reviewers}
            sdp={historical ? undefined : { onChoose: setSdpOption }}
            slots={slots}
            stagedChangeRequests={changeRequestCount}
            stagedQuestions={questionCount}
            // Advisory, non-blocking, and only where a mutation is possible: a
            // past round is not the place to reconcile the current slot.
            stale={
              staleSlot?.staleBasis === true && !historical
                ? {
                    cause: staleSlot.staleCause,
                    ackPending: acknowledgeStale.isPending || acknowledgeProjectStale.isPending,
                    ackError:
                      acknowledgeStale.error?.message ?? acknowledgeProjectStale.error?.message,
                    onAcknowledge: acknowledgeStaleBasis,
                    onReconcile: reconcileStale,
                  }
                : undefined
            }
            systemEnvelope={systemEnvelope}
            // The history banner already says the whole surface is read-only;
            // repeating it per thread would be noise.
            threadReadOnly={!historical && !canSetCommentStatus}
            title={node.title}
            verdicts={revisionWire?.verdicts}
            vm={vm}
            onApprove={() => {
              decide(true);
            }}
            onAsk={askQuestions}
            onNavigate={(path, params) => {
              void navigate({ to: path, params: { projectId, ...params }, search: () => ({}) });
            }}
            onRetry={verbs.rerun.kind === 'none' ? undefined : rerun}
            onRevision={(n) => {
              go(node.id, n);
            }}
            onSendBack={() => {
              decide(false);
            }}
          />
        )}
      </Box>
    </ExperienceChrome>
  );

  // A read-only history suppresses the WHOLE comment affordance set — the
  // commentable rows' buttons, the armed-anchor probe, the margin's composer —
  // by shadowing the route's provider with a disabled one. It wraps the chrome,
  // not just the body, because the chrome is what renders the margin: a provider
  // around the body alone would leave the margin live. The container's own
  // `useComments()` above still reads the route's provider, which is what keeps
  // the pending-comment slot bound while the reader is away in the past.
  return historical ? <CommentProvider enabled={false}>{screen}</CommentProvider> : screen;
}
