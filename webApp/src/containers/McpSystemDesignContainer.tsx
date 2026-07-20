/**
 * The MCP-hosted System Design container. It renders the SAME pure
 * `SystemDesignView` the SPA container does, but sources its data over the MCP
 * transport (TanStack Query rides the OpsClient → `app.callServerTool`) and swaps
 * the SPA's chat-rail / CommentContext comment substrate for the MCP-honest
 * affordances the spec §3.4 two-call ruling calls for.
 *
 * Data flow:
 *  - `toolArgs` (from the host's tool-input notification) carries the flattened
 *    path+query for `systemDesignGetSessionState`: `{ projectID, kind }`.
 *  - `seededResult` is that tool's first pushed result; we prime the session-state
 *    query cache from it in the first render so the screen paints instantly, then
 *    re-seed on every subsequent `mcp-tool-result` window event (agent re-runs).
 *  - `project` head-state and any navigated-to step's session are fetched lazily
 *    over the same MCP bridge (useProject / useSessionState are transport-blind).
 *
 * MCP omits: the chat rail, the review-thread display, and CommentContext. In their
 * place: a tiny inline composer that (a) turns a text selection into a two-call
 * "share + ask the agent to file it" comment, and (b) collects reject feedback for
 * a gate send-back (which, unlike a bare comment, has a first-class server op).
 */
import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import TextField from '@mui/material/TextField';
import Button from '@mui/material/Button';
import Typography from '@mui/material/Typography';
import type {
  App,
  McpUiDisplayMode,
  McpUiToolResultNotification,
} from '@modelcontextprotocol/ext-apps';
import { useQueryClient } from '@tanstack/react-query';

import type { ArtifactKind, ProjectState, ReviewDecision, SessionStage } from '../contracts/types';
import type { components } from '../contracts/schema';
import { slotStageFromOrdinal } from '../contracts/adapters';
import { PHASE1_ORDER, METHOD_METADATA } from '../contracts/methodMetadata';
import { mapSessionState, systemArtifactKindFromOrdinal } from '../contracts/wire';

import { useProject } from '../hooks/useProject';
import { isSessionAbsent } from '../hooks/sessionPolling';
import { useSessionState, sessionStateKey } from '../hooks/useSessionState';
import {
  useAcknowledgeStaleBasis,
  useRequestArtifactDraft,
  useSubmitReviewDecision,
} from '../hooks/useDesignMutations';

import { SystemDesignView, type SpineStep } from '../components/design/SystemDesignView';
import { DesignExperienceSkeleton } from '../components/design/DesignSkeleton';
import { gateDecisionErrorMessage } from '../components/design/gateFaultLogic';
import type { Anchor } from '../components/comments/CommentContext';

import { useTokens } from '../utilities/theme/ThemeContext';

type Schemas = components['schemas'];

const PHASE1_KINDS = PHASE1_ORDER as readonly ArtifactKind[];

/** Props every MCP view container receives from the shell (see registry.ts). */
export interface McpViewProps {
  app: App;
  /** Flattened tool arguments (path + query + body) from the host's tool-input. */
  toolArgs: Record<string, unknown>;
  /** The first tool result's structured payload, for synchronous cache seeding. */
  seededResult: Record<string, unknown> | undefined;
  displayMode: McpUiDisplayMode | undefined;
}

/** Build the spine steps from the project slots: committed / current / locked.
 *  (Copied from the SPA container — a small, stable derivation kept local rather
 *  than coupling the two containers through a shared export.) */
function buildSpine(project: ProjectState | undefined): SpineStep[] {
  const committed = new Set(
    (project?.slots ?? [])
      .filter((s) => slotStageFromOrdinal(s.stage) === 'committed')
      .map((s) => s.kind)
  );
  const stale = new Set(
    (project?.slots ?? []).filter((s) => s.staleBasis === true).map((s) => s.kind)
  );
  let priorCommitted = true;
  return PHASE1_KINDS.map((kind) => {
    const isCommitted = committed.has(kind);
    const locked = !isCommitted && !priorCommitted;
    priorCommitted = isCommitted;
    return {
      kind,
      title: METHOD_METADATA[kind].title,
      committed: isCommitted,
      locked,
      stale: stale.has(kind),
    };
  });
}

/** Read the initial artifact kind out of the tool's flattened query arg (an ordinal). */
function kindFromToolArgs(toolArgs: Record<string, unknown>): ArtifactKind {
  const raw = toolArgs['kind'];
  return typeof raw === 'number' ? systemArtifactKindFromOrdinal(raw) : 'mission';
}

function projectIdFromToolArgs(toolArgs: Record<string, unknown>): string {
  const raw = toolArgs['projectID'];
  return typeof raw === 'string' ? raw : '';
}

/** Ambient model-context text: what the architect is currently looking at. Sent on
 *  view/stage/selection/lastFiledComment change so the agent's next turn is
 *  grounded in the screen.
 *
 *  `lastFiledComment`, when present, is folded into this text rather than left to
 *  a one-shot post: `submitSelectionComment` stages the comment as model context
 *  and then sends a user turn asking the agent to file it, but closing the
 *  composer afterward flips `composer` back to null, which re-runs THIS effect
 *  (it depends on `composer`) and would otherwise immediately overwrite that
 *  staged text with plain view text before the agent's next turn ever sees it.
 *  Folding (vs. a one-shot suppress-next-post ref) survives any number of
 *  subsequent ambient re-fires — e.g. a stage change landing in the same tick as
 *  the composer close — because the fold lives in state, not in a flag that only
 *  protects a single post. It's cleared on step navigation (see onSelectStep). */
function viewContextText(
  projectName: string | undefined,
  kind: ArtifactKind,
  stage: SessionStage | undefined,
  anchor: Anchor | null,
  lastFiledComment: string | null
): string {
  const lines = [
    `The architect is viewing the "${METHOD_METADATA[kind].title}" system-design artifact` +
      (projectName !== undefined ? ` of project "${projectName}"` : '') +
      '.',
    stage !== undefined
      ? `Session stage: ${stage}.`
      : 'No live co-authoring session for this artifact.',
  ];
  if (anchor !== null) {
    lines.push(`They have selected: "${anchor.anchorText ?? anchor.label}" (${anchor.jsonPath}).`);
  }
  if (lastFiledComment !== null) {
    lines.push('', `They just filed this comment: ${lastFiledComment}`);
  }
  return lines.join('\n');
}

/** Model-context text for a filed comment: anchor + view state + the comment body. */
function commentContextText(
  projectName: string | undefined,
  kind: ArtifactKind,
  stage: SessionStage | undefined,
  anchor: Anchor,
  text: string
): string {
  return [
    viewContextText(projectName, kind, stage, anchor, null),
    '',
    `The architect wants to file this comment on the selected element:`,
    text,
  ].join('\n');
}

export function McpSystemDesignContainer({
  app,
  toolArgs,
  seededResult,
  displayMode,
}: McpViewProps): ReactNode {
  const t = useTokens();
  const queryClient = useQueryClient();

  const projectId = useMemo(() => projectIdFromToolArgs(toolArgs), [toolArgs]);
  const initialKind = useMemo(() => kindFromToolArgs(toolArgs), [toolArgs]);

  // Re-seed the session-state cache on every subsequent tool result (an agent
  // re-invocation), keyed by the result's OWN artifact kind so a background push
  // can't overwrite a step the architect has since navigated to.
  useEffect(() => {
    const handler = (event: Event): void => {
      const detail = (event as CustomEvent<McpUiToolResultNotification['params']>).detail;
      const structured = detail.structuredContent;
      if (structured === undefined) return;
      const mapped = mapSessionState(structured as Schemas['SystemDesignSessionStateView']);
      queryClient.setQueryData(sessionStateKey(projectId, mapped.artifactKind), mapped);
    };
    window.addEventListener('mcp-tool-result', handler);
    return (): void => {
      window.removeEventListener('mcp-tool-result', handler);
    };
  }, [queryClient, projectId]);

  const { data: project } = useProject(projectId);
  const spine = useMemo(() => buildSpine(project), [project]);

  // Seed the session-state cache from the FIRST pushed result in this state
  // initializer — it runs exactly once, before useSessionState below subscribes —
  // so the initial screen paints without a round-trip.
  const [activeIndex, setActiveIndex] = useState(() => {
    if (seededResult !== undefined) {
      queryClient.setQueryData(
        sessionStateKey(projectId, initialKind),
        mapSessionState(seededResult as Schemas['SystemDesignSessionStateView'])
      );
    }
    return Math.max(0, PHASE1_KINDS.indexOf(initialKind));
  });
  const safeIndex = Math.min(activeIndex, PHASE1_KINDS.length - 1);
  const activeKind: ArtifactKind = PHASE1_KINDS[safeIndex] ?? 'mission';

  const session = useSessionState(projectId, activeKind, projectId.length > 0);
  const requestDraft = useRequestArtifactDraft(projectId);
  const submitReview = useSubmitReviewDecision(projectId);
  const acknowledgeStale = useAcknowledgeStaleBasis(projectId);

  const [gateError, setGateError] = useState<string | undefined>(undefined);
  const [composer, setComposer] = useState<{
    mode: 'comment' | 'reject';
    anchor: Anchor | null;
  } | null>(null);
  const [composerText, setComposerText] = useState('');
  const [sendFallbackHint, setSendFallbackHint] = useState(false);
  // Folded into the ambient view-context text (see viewContextText's doc comment)
  // so the composer-close ambient re-post doesn't clobber a just-filed comment.
  const [lastFiledComment, setLastFiledComment] = useState<string | null>(null);

  // QA 2026-07-19: absence is only authoritative when NO session view is cached
  // (see isSessionAbsent) — a 404 refetch while a view is held must not reset the wizard.
  const sessionMissing = isSessionAbsent(session.data !== undefined, session.error);
  const stage = session.data?.stage;
  const projectName = project?.name;

  // Ambient view-state sync (overwrite semantics are the point — only the last
  // update reaches the model, and it does so on the NEXT user turn).
  useEffect(() => {
    void app.updateModelContext({
      content: [
        {
          type: 'text',
          text: viewContextText(
            projectName,
            activeKind,
            stage,
            composer?.anchor ?? null,
            lastFiledComment
          ),
        },
      ],
    });
  }, [app, projectName, activeKind, stage, composer, lastFiledComment]);

  // The two-call comment path (spec §3.4): stage the anchor + comment as model
  // context, THEN post a brief user turn asking the agent to file it. A comment has
  // no standalone server op, so the agent is the honest filer of record.
  const submitSelectionComment = useCallback(
    async (anchor: Anchor, text: string): Promise<void> => {
      await app.updateModelContext({
        content: [
          { type: 'text', text: commentContextText(projectName, activeKind, stage, anchor, text) },
        ],
      });
      const res = await app.sendMessage({
        role: 'user',
        content: [{ type: 'text', text: 'File my comment on the selected element.' }],
      });
      if (res.isError === true) setSendFallbackHint(true);
    },
    [app, projectName, activeKind, stage]
  );

  const onSelectStep = (i: number): void => {
    setGateError(undefined);
    setComposer(null);
    setLastFiledComment(null);
    setActiveIndex(i);
  };

  const onRequestDraft = (feedback?: string): void => {
    requestDraft.mutate(
      feedback !== undefined ? { kind: activeKind, feedback } : { kind: activeKind }
    );
  };

  const onSubmitReview = (decision: ReviewDecision): void => {
    setGateError(undefined);
    if (decision === 'reject') {
      // Send-back needs authored feedback; collect it in the composer (a first-class
      // server op backs this, so it stays a direct mutation, unlike a comment).
      setComposer({ mode: 'reject', anchor: null });
      setComposerText('');
      return;
    }
    submitReview.mutate(
      { kind: activeKind, decision },
      {
        onSuccess: () => {
          if (decision === 'approve') {
            setActiveIndex((i) => Math.min(i + 1, PHASE1_KINDS.length - 1));
          }
        },
        onError: (err) => {
          // Precise message for a definite refusal, cause-neutral copy for an
          // indeterminate transport fault (F-QA2-47) — MCP has no background poll,
          // so refetch is the only way the lost-response case renders truth.
          setGateError(gateDecisionErrorMessage(err));
          void session.refetch();
        },
      }
    );
  };

  const closeComposer = (): void => {
    setComposer(null);
    setComposerText('');
  };

  const submitComposer = async (): Promise<void> => {
    if (composer === null) return;
    const text = composerText.trim();
    if (composer.mode === 'reject') {
      if (text.length === 0) return;
      submitReview.mutate(
        { kind: activeKind, decision: 'reject', detail: { feedback: text } },
        {
          onSuccess: closeComposer,
          onError: (err) => {
            // The composer's send-back text is retained (composer stays open) —
            // F-QA2-47: cause-neutral copy for an indeterminate fault + refetch.
            setGateError(gateDecisionErrorMessage(err));
            void session.refetch();
          },
        }
      );
      return;
    }
    if (composer.anchor !== null) {
      // The composer's "Comment" submit is disabled below while `text` is empty
      // (a filed comment always carries the architect's own words), so `text` is
      // guaranteed non-empty here.
      await submitSelectionComment(composer.anchor, text);
      setLastFiledComment(text);
      closeComposer();
    }
  };

  // Project head-state is still loading — render the themed skeleton rather than
  // chrome that would guess each step's committed/locked status and then contradict
  // itself once the data lands (mirrors the SPA container's gate).
  if (project === undefined) {
    return (
      <DesignExperienceSkeleton
        phaseNum={1}
        phaseTitle="System Design"
        steps={PHASE1_KINDS.length}
        onClose={() => void app.requestTeardown()}
      />
    );
  }

  return (
    <>
      <SystemDesignView
        allowEmptySendBack
        {...(displayMode !== undefined ? { displayMode } : {})}
        acknowledgeStaleError={acknowledgeStale.error?.message}
        acknowledgeStalePending={acknowledgeStale.isPending}
        activeIndex={safeIndex}
        amendPending={requestDraft.isPending}
        beginPending={requestDraft.isPending}
        commentSurface={{
          enabled: true,
          commentCount: 0,
          setAnchor: (anchor) => {
            if (anchor !== null) {
              setComposer({ mode: 'comment', anchor });
              setComposerText('');
              setSendFallbackHint(false);
            }
          },
        }}
        decisionPending={submitReview.isPending}
        gateError={gateError}
        needsResearch={false}
        project={project}
        researchPending={false}
        retryPending={requestDraft.isPending}
        session={session.data}
        sessionLoading={session.isLoading}
        sessionMissing={sessionMissing}
        spine={spine}
        onAcknowledgeStale={(note) => {
          acknowledgeStale.mutate({ kind: activeKind, note });
        }}
        onClose={() => void app.requestTeardown()}
        onRequestDraft={onRequestDraft}
        onRetry={() => {
          requestDraft.mutate({ kind: activeKind });
        }}
        onSelectStep={onSelectStep}
        onSubmitResearch={() => undefined}
        onSubmitReview={onSubmitReview}
        onSubmitSelectionComment={(anchor, text) => void submitSelectionComment(anchor, text)}
      />
      {composer !== null ? (
        <Box
          sx={{
            position: 'fixed',
            inset: 0,
            zIndex: 1500,
            display: 'flex',
            alignItems: 'flex-end',
            justifyContent: 'center',
            bgcolor: 'rgba(0,0,0,0.35)',
          }}
        >
          <Paper
            sx={{
              m: 2,
              p: 2,
              width: '100%',
              maxWidth: 560,
              display: 'flex',
              flexDirection: 'column',
              gap: 1.5,
            }}
          >
            <Typography sx={{ fontFamily: t.mono, fontWeight: 700, color: t.ink }}>
              {composer.mode === 'reject'
                ? 'Send back for revision'
                : `Comment on “${composer.anchor?.label ?? 'selection'}”`}
            </Typography>
            <TextField
              fullWidth
              multiline
              minRows={3}
              placeholder={
                composer.mode === 'reject'
                  ? 'What needs to change before this can be approved?'
                  : 'Your comment…'
              }
              value={composerText}
              onChange={(e) => {
                setComposerText(e.target.value);
              }}
            />
            {sendFallbackHint ? (
              <Typography sx={{ fontSize: 12, color: t.muted }}>
                Shared with the conversation — ask the agent to file it.
              </Typography>
            ) : null}
            <Box sx={{ display: 'flex', gap: 1, justifyContent: 'flex-end' }}>
              <Button color="inherit" onClick={closeComposer}>
                Cancel
              </Button>
              <Button
                disabled={composerText.trim().length === 0}
                variant="contained"
                onClick={() => void submitComposer()}
              >
                {composer.mode === 'reject' ? 'Send back' : 'Comment'}
              </Button>
            </Box>
          </Paper>
        </Box>
      ) : null}
    </>
  );
}
