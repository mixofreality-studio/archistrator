/**
 * The pure System Design (Phase-1) screen. ALL data and callbacks arrive via
 * props — no hooks that reach into `api`/`hooks` (fetching, mutations) or into
 * ambient CommentContext. Extracted from `routes/DesignExperience.tsx`'s former
 * `SystemDesignBody`/`StepBody` pair (Task 8) so the SPA container
 * (`containers/SystemDesignContainer.tsx`) and, later, an MCP host (Task 9) can
 * both compose this same screen against two different data-and-comment substrates.
 *
 * Composition: ExperienceChrome (chrome + optional chat rail + SelectionPopover)
 * → SlimSpine (progress rail) → artifact header → StepBody (the active step's
 * content: research CTA / generating scene / committed panel / draft + gate).
 *
 * ── SPA-only optional surfaces ───────────────────────────────────────────────
 * `chat` is an opaque, pre-built ReactNode (the SPA container wires its own
 * ChatRail against CommentContext + review mutations); omitted, ExperienceChrome
 * renders no chat affordance at all. `commentSurface` carries the minimal bit of
 * CommentContext state this pure screen itself needs (the local pending-comment
 * count that gates "Send back", and the anchor-arming callback SelectionPopover
 * uses); omitted, both default to "no local comment surface" (SelectionPopover
 * still falls back to ambient CommentContext when a Provider happens to wrap the
 * tree — see CommentContext.useComments — so nested per-item commentable
 * affordances deep in ArtifactRenderer keep working via context regardless).
 * `onSubmitSelectionComment` is a forward-compat hook for Task 9's MCP
 * comment-submission flow; SelectionPopover only ARMS an anchor today (the actual
 * text composition happens in ChatRail, which lives entirely inside the opaque
 * `chat` slot), so this screen does not yet wire it to anything — Task 9 owns
 * designing the MCP submit path.
 */
import { useState, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import Chip from '@mui/material/Chip';
import Button from '@mui/material/Button';
import Alert from '@mui/material/Alert';
import AutoAwesomeIcon from '@mui/icons-material/AutoAwesome';
import WarningAmberIcon from '@mui/icons-material/WarningAmber';

import type {
  ArtifactKind,
  ArtifactModelEnvelope,
  ArtifactProvenance,
  Finding,
  ProjectStateWithGit,
  ResearchInput,
  ReviewDecision,
  SessionStateResponse,
} from '../../contracts/types';
import { METHOD_METADATA } from '../../contracts/methodMetadata';
import type { Anchor } from '../comments/CommentContext';
import type { SelectionCommentSurface } from '../comments/SelectionPopover';

import { ArtifactRenderer } from '../ArtifactRenderer';
import { ArtifactIntro, ArtifactInfoButton } from './ArtifactIntro';
import { StageChip } from '../StageChip';
import { headerChipStage } from './headerChipStage';
import { ExperienceChrome } from './ExperienceChrome';
import { SlimSpine, type SpineStep } from './SlimSpine';
import { GeneratingScene } from './GeneratingScene';
import { DraftFailedPanel } from './DraftFailedPanel';
import { GatePanel } from './GatePanel';
import { CommittedArtifactPanel } from './CommittedArtifactPanel';
import { StaleBasisHeaderChip } from './StaleBasisChip';
import { ResearchInputPanel } from './ResearchInputPanel';
import { CommentProvider } from '../comments/CommentContext';
import { SkeletonContentCard } from './DesignSkeleton';

import { useTokens } from '../../utilities/theme/ThemeContext';
import type { Tokens } from '../../utilities/theme/themes';
import { UI_IDENTIFIERS } from '../../utilities/constants/UIIdentifiers';

// Prose (markdown) artifact kinds get a paper surface in the full-screen design
// experience; diagram kinds (volatilities/system/coreUseCases/operationalConcepts)
// render on their own bordered canvases, so they stay unwrapped.
const PROSE_ARTIFACT_KINDS = new Set<string>(['mission', 'scrubbedRequirements', 'standardCheck']);

/** Seed rationale for a reconcile-via-amendment fired from the stale banner (F45). */
const RECONCILE_RATIONALE = 'Reconcile with amended upstream basis.';

// Re-exported so containers (SPA today, an MCP host later) can build a `spine`
// prop without reaching into `./SlimSpine` directly.
export type { SpineStep };

function proseSurface(kind: string | undefined, node: ReactNode): ReactNode {
  return kind !== undefined && PROSE_ARTIFACT_KINDS.has(kind) ? (
    <Paper sx={{ p: { xs: 2.5, md: 4 } }}>{node}</Paper>
  ) : (
    node
  );
}

/**
 * The pure screen's own local comment-surface needs — deliberately a small
 * subset of CommentContext's `CommentCtx`, not the whole thing: the accumulated,
 * not-yet-sent comment count (gates GatePanel's "Send back") and the
 * anchor-arming callback SelectionPopover commits into. SPA-only; the container
 * builds this from its own `useComments()` call (it renders inside
 * CommentProvider). Omitted in MCP.
 */
export interface CommentSurfaceProps extends SelectionCommentSurface {
  /** Count of accumulated, not-yet-sent comments this gate cycle. */
  commentCount: number;
}

export interface SystemDesignViewProps {
  project: ProjectStateWithGit;
  /**
   * The active step's co-authoring session. `undefined` while EITHER the session
   * is still loading OR no session exists yet (404) — disambiguated by
   * `sessionLoading`/`sessionMissing` below, since `SessionStateResponse` alone
   * cannot carry a "no session" signal.
   */
  session: SessionStateResponse | undefined;
  spine: SpineStep[];
  /** Index into `spine` of the step currently shown. */
  activeIndex: number;
  /** Navigate to a different (non-locked) step. */
  onSelectStep: (i: number) => void;
  /** Reserved for Task 9 (MCP shell); the SPA always renders the fullscreen chrome. */
  displayMode?: 'inline' | 'fullscreen' | 'pip';
  onSubmitReview: (d: ReviewDecision) => void;
  /** Request a fresh draft (no feedback) or an amendment (feedback = rationale). */
  onRequestDraft: (feedback?: string) => void;
  onRetry: () => void;
  /** Close the experience (the ✕ affordance). */
  onClose: () => void;
  /** The co-author session query is still in flight (distinct from `project` load,
   *  which gates whether this screen mounts at all). */
  sessionLoading: boolean;
  /** The active step has no co-author session yet (404) — distinct from loading. */
  sessionMissing: boolean;
  /** The first step's Request-draft failed a 409 precondition (no ResearchInput yet). */
  needsResearch: boolean;
  onSubmitResearch: (research: ResearchInput) => void;
  researchPending: boolean;
  /** "Request draft" button's mutation-in-flight state. */
  beginPending: boolean;
  /** DraftFailedPanel's Retry mutation-in-flight state. */
  retryPending: boolean;
  /** CommittedArtifactPanel's Amend mutation-in-flight state. */
  amendPending: boolean;
  /** The review-decision mutation (approve/reject/withdraw) is in flight. */
  decisionPending: boolean;
  /** A failed gate decision's message, surfaced inline until the next attempt. */
  gateError?: string | undefined;
  onAcknowledgeStale: (note: string) => void;
  acknowledgeStalePending: boolean;
  acknowledgeStaleError?: string | undefined;
  /**
   * SPA default (false, unset): GatePanel's "Send back" stays gated on
   * `commentSurface.commentCount > 0`. MCP (no client-side comment accumulator)
   * passes `true` so the click is reachable — see GatePanel's own doc comment for
   * why that doesn't weaken the "redraft always carries guidance" invariant.
   */
  allowEmptySendBack?: boolean;
  // ── SPA-only optional surfaces (see file header) ──────────────────────────
  chat?: ReactNode;
  chatOpen?: boolean;
  onOpenChat?: () => void;
  commentSurface?: CommentSurfaceProps;
  /** Reserved for Task 9's MCP two-call (arm, then submit) comment flow. */
  onSubmitSelectionComment?: (anchor: Anchor, text: string) => void;
}

export function SystemDesignView({
  project,
  session,
  spine,
  activeIndex,
  onSelectStep,
  onSubmitReview,
  onRequestDraft,
  onRetry,
  onClose,
  sessionLoading,
  sessionMissing,
  needsResearch,
  onSubmitResearch,
  researchPending,
  beginPending,
  retryPending,
  amendPending,
  decisionPending,
  gateError,
  onAcknowledgeStale,
  acknowledgeStalePending,
  acknowledgeStaleError,
  allowEmptySendBack = false,
  chat,
  chatOpen,
  onOpenChat,
  commentSurface,
}: SystemDesignViewProps): ReactNode {
  const t = useTokens();

  const safeIndex = Math.max(0, Math.min(activeIndex, spine.length - 1));
  const activeKind = (spine[safeIndex]?.kind ?? 'mission') as ArtifactKind;
  const meta = METHOD_METADATA[activeKind];

  const view = session?.view;
  const stage = session?.stage;
  const committedSlot = project.slots.find((s) => s.kind === activeKind);
  const committedEnvelope = committedSlot?.model;
  // The committed coreUseCases envelope (precedes `system` in the ladder): lets the
  // Architecture view label blank-titled dynamic views by their use case (F-QA2-51).
  const useCasesEnvelope = project.slots.find((s) => s.kind === 'coreUseCases')?.model;
  const committedRevisions = committedSlot?.revisions;
  const committedProvenance = committedSlot?.provenance;
  const committedStale = committedSlot?.staleBasis === true;
  const committedStaleCause = committedSlot?.staleCause;
  const hasDraft = view?.draft.model !== undefined;
  const findings = view?.findings ?? [];
  const reviewThread = view?.reviewThread ?? [];
  const openCommentCount = reviewThread.filter((c) => c.status === 'open').length;
  const generating = stage === 'drafting' || stage === 'redrafting';
  const asyncFailed = stage === 'draftFailed';
  const draftFailed = stage === 'refused' || asyncFailed;
  const failureReason = view?.failureReason;
  const failureRunUrl = view?.failureRunUrl;
  const activeCommitted = spine[safeIndex]?.committed === true;
  const upstreamStaleCount = spine.slice(0, safeIndex).filter((s) => s.stale === true).length;
  const showStandardCheckCaveat = activeKind === 'standardCheck' && upstreamStaleCount > 0;
  const commentCount = commentSurface?.commentCount ?? 0;

  // F-GTD-12: while this artifact's own co-author session is LIVE (an amendment in
  // flight — a committed slot can only host an amendment), the ack would commit to
  // main and merge-conflict the amendment's review PR. Gate the popover action too
  // so the refusal is explained instead of discovered.
  const sessionLive =
    stage === 'drafting' ||
    stage === 'awaitingReview' ||
    stage === 'redrafting' ||
    stage === 'draftFailed';
  const ackDisabledReason = sessionLive
    ? 'An amendment is already in flight for this artifact — reconcile rides it. Approve or withdraw the amendment first.'
    : undefined;

  return (
    <ExperienceChrome
      chat={chat}
      chatOpen={chatOpen}
      commentSurface={commentSurface}
      phaseNum={1}
      phaseTitle="System Design"
      projectName={project.name}
      spine={<SlimSpine activeIndex={safeIndex} steps={spine} onSelect={onSelectStep} />}
      onClose={onClose}
      onOpenChat={onOpenChat}
    >
      <Box sx={{ flexGrow: 1, minWidth: 0, overflowY: 'auto', px: { xs: 2, md: 4 }, py: 3 }}>
        {/* artifact header */}
        <Box sx={{ display: 'flex', alignItems: 'flex-start', gap: 1.5, mb: 2 }}>
          <Box sx={{ minWidth: 0 }}>
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, flexWrap: 'wrap' }}>
              <Typography component="h1" sx={{ color: t.ink }} variant="h4">
                {meta.title}
              </Typography>
              <StageChip stage={headerChipStage(activeCommitted, stage)} />
              {/* Committed framing copy moved off the full-width banner into a (?) info
                  popover; staleness moved off the amber banner into a compact chip —
                  so the first paint of a committed step is content, not banners. */}
              {activeCommitted ? <ArtifactInfoButton kind={activeKind} /> : null}
              {activeCommitted && committedStale ? (
                <StaleBasisHeaderChip
                  ackDisabledReason={ackDisabledReason}
                  ackError={acknowledgeStaleError}
                  acknowledgePending={acknowledgeStalePending}
                  cause={committedStaleCause}
                  onAcknowledge={onAcknowledgeStale}
                  onReconcile={() => {
                    onRequestDraft(RECONCILE_RATIONALE);
                  }}
                />
              ) : null}
            </Box>
            <Typography sx={{ fontFamily: t.mono, fontSize: 12, color: t.muted, mt: 0.5 }}>
              {meta.file} · step {safeIndex + 1} of {spine.length}
            </Typography>
            {/* PM-P1-3: a compact caveat (not a full-width banner) when the Standard
                Check renders over drifted upstream artifacts. */}
            {showStandardCheckCaveat ? (
              <Chip
                data-testid={UI_IDENTIFIERS.DesignExperience.STANDARD_CHECK_CAVEAT}
                icon={<WarningAmberIcon sx={{ fontSize: 14 }} />}
                label={`may be invalidated — ${String(upstreamStaleCount)} upstream artifact${upstreamStaleCount === 1 ? '' : 's'} changed since this check`}
                size="small"
                sx={{
                  mt: 1,
                  bgcolor: t.paperAlt,
                  color: t.ink,
                  fontWeight: 700,
                  border: `1.5px solid ${t.bandYellow}`,
                  '& .MuiChip-icon': { color: t.bandYellow },
                }}
              />
            ) : null}
          </Box>
          <Box sx={{ flexGrow: 1 }} />
          <Chip
            label="architect"
            size="small"
            sx={{ bgcolor: t.chatArchitectBg, color: t.chatArchitectFg }}
            variant="outlined"
          />
          {meta.hasPmCritic ? (
            <Chip
              label="pm"
              size="small"
              sx={{ bgcolor: t.chatPmBg, color: t.chatPmFg }}
              variant="outlined"
            />
          ) : null}
        </Box>

        {/* body */}
        <StepBody
          activeKind={activeKind}
          allowEmptySendBack={allowEmptySendBack}
          amendPending={amendPending}
          asyncFailed={asyncFailed}
          beginPending={beginPending}
          blurb={meta.blurb}
          commentCount={commentCount}
          committed={activeCommitted}
          committedEnvelope={committedEnvelope}
          committedProvenance={committedProvenance}
          committedRevisions={committedRevisions}
          decisionPending={decisionPending}
          draftFailed={draftFailed}
          failureReason={failureReason}
          failureRunUrl={failureRunUrl}
          findings={findings}
          gateError={gateError}
          generating={generating}
          hasDraft={hasDraft}
          loading={sessionLoading}
          needsResearch={needsResearch}
          openCommentCount={openCommentCount}
          researchPending={researchPending}
          retryPending={retryPending}
          sessionMissing={sessionMissing}
          stage={stage}
          t={t}
          title={meta.title}
          useCasesEnvelope={useCasesEnvelope}
          view={view}
          withdrawPending={decisionPending}
          onAmend={onRequestDraft}
          onApprove={() => {
            onSubmitReview('approve');
          }}
          onBegin={() => {
            onRequestDraft(undefined);
          }}
          onRetry={onRetry}
          onSendBack={() => {
            onSubmitReview('reject');
          }}
          onSubmitResearch={onSubmitResearch}
          onWithdraw={() => {
            onSubmitReview('withdraw');
          }}
        />
      </Box>
    </ExperienceChrome>
  );
}

function StepBody({
  t,
  activeKind,
  allowEmptySendBack,
  committed,
  committedEnvelope,
  committedRevisions,
  committedProvenance,
  useCasesEnvelope,
  loading,
  generating,
  needsResearch,
  draftFailed,
  asyncFailed,
  failureReason,
  failureRunUrl,
  hasDraft,
  sessionMissing,
  stage,
  title,
  blurb,
  view,
  findings,
  commentCount,
  openCommentCount,
  gateError,
  decisionPending,
  beginPending,
  researchPending,
  retryPending,
  withdrawPending,
  amendPending,
  onBegin,
  onRetry,
  onSubmitResearch,
  onApprove,
  onSendBack,
  onWithdraw,
  onAmend,
}: {
  t: Tokens;
  activeKind: ArtifactKind;
  allowEmptySendBack: boolean;
  committed: boolean;
  committedEnvelope: ArtifactModelEnvelope | undefined;
  committedRevisions: number | undefined;
  committedProvenance: ArtifactProvenance | undefined;
  /** The committed coreUseCases envelope (F-QA2-51 dynamic-view label fallback). */
  useCasesEnvelope: ArtifactModelEnvelope | undefined;
  loading: boolean;
  generating: boolean;
  needsResearch: boolean;
  draftFailed: boolean;
  asyncFailed: boolean;
  failureReason: string | undefined;
  failureRunUrl: string | undefined;
  hasDraft: boolean;
  sessionMissing: boolean;
  stage: string | undefined;
  title: string;
  blurb: string;
  view: SessionStateResponse['view'] | undefined;
  findings: Finding[];
  commentCount: number;
  openCommentCount: number;
  gateError: string | undefined;
  decisionPending: boolean;
  beginPending: boolean;
  researchPending: boolean;
  retryPending: boolean;
  withdrawPending: boolean;
  amendPending: boolean;
  onBegin: () => void;
  onRetry: () => void;
  onSubmitResearch: (research: ResearchInput) => void;
  onApprove: () => void;
  onSendBack: () => void;
  onWithdraw: () => void;
  onAmend: (feedback: string) => void;
}): ReactNode {
  if (needsResearch) {
    return <ResearchInputPanel pending={researchPending} onSubmit={onSubmitResearch} />;
  }
  // Terminal failure takes precedence over the generating loader so a failed
  // session surfaces an error + Retry/Withdraw instead of an infinite generating
  // screen. The async `draftFailed` variant frames it as a CI-job failure and
  // offers Withdraw alongside Retry.
  if (draftFailed) {
    return (
      <DraftFailedPanel
        artifact={title}
        async={asyncFailed}
        // A failed Retry/Withdraw decision surfaces inline here too (2026-07-16
        // incident: dead-session decisions 503'd with zero feedback rendered).
        gateError={gateError}
        pending={retryPending}
        reason={failureReason}
        runUrl={failureRunUrl}
        withdrawPending={withdrawPending}
        onRetry={onRetry}
        onWithdraw={asyncFailed ? onWithdraw : undefined}
      />
    );
  }
  if (generating) {
    // A committed slot that is generating is an amendment-in-flight: frame it so the
    // committed header + this scene read honestly (the committed revision stays current).
    const scene = (
      <GeneratingScene
        // F-GTD-6: the server surfaces the LIVE run's URL while the design job is in
        // flight, so the CI-job notice deep-links the actual GitHub Actions run.
        // Absent (older server / URL not yet resolved) → the notice renders unlinked.
        actionsUrl={view?.runUrl}
        activeRole={view?.activeRole}
        activeStep={view?.activeStep}
        amendingRevision={committed ? (committedRevisions ?? 0) : undefined}
        artifact={title}
        phrase={METHOD_METADATA[activeKind].phrase}
        round={view?.round}
      />
    );
    // Reviewers must be able to READ the committed revision while its amendment
    // drafts — don't blank the pane. Render the committed model read-only (dimmed,
    // labeled "current") above the generating scene, in a disabled comment context
    // so it carries zero comment affordances.
    if (committed && committedEnvelope !== undefined) {
      const revN = committedRevisions ?? 0;
      return (
        <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
          <Box>
            <Box
              data-testid={UI_IDENTIFIERS.DesignExperience.AMEND_CURRENT_LABEL}
              sx={{
                display: 'flex',
                alignItems: 'center',
                gap: 1,
                px: 2,
                py: 1,
                bgcolor: t.committedBg,
                border: `1.5px solid ${t.line}`,
                borderBottom: 'none',
              }}
            >
              <Typography
                sx={{
                  fontFamily: t.mono,
                  fontWeight: 700,
                  fontSize: 12,
                  letterSpacing: '0.08em',
                  color: t.committedFg,
                }}
              >
                COMMITTED{revN > 1 ? ` · revision ${String(revN)}` : ''} — current
              </Typography>
              <Typography sx={{ fontFamily: t.mono, fontSize: 11, color: t.muted }}>
                stays live until the amendment is approved
              </Typography>
            </Box>
            <Box
              aria-hidden
              sx={{ opacity: 0.6, pointerEvents: 'none', border: `1.5px solid ${t.line}`, p: 1 }}
            >
              <CommentProvider enabled={false}>
                {proseSurface(
                  committedEnvelope.kind,
                  <ArtifactRenderer
                    envelope={committedEnvelope}
                    height={480}
                    title={title}
                    useCasesEnvelope={useCasesEnvelope}
                  />
                )}
              </CommentProvider>
            </Box>
          </Box>
          {scene}
        </Box>
      );
    }
    return scene;
  }
  // The project head-state has resolved by now (the container renders the
  // full-screen skeleton while it is in flight, before this screen mounts at
  // all), so the surrounding header/chip/spine are already truthful. Only the
  // co-author session is still loading — sketch the content card instead of a
  // bare spinner so it stays consistent with the design system.
  if (loading && view === undefined) {
    return <SkeletonContentCard t={t} />;
  }
  // When the session is missing (404) but the slot is committed in the project
  // head-state, render the committed model read-only under the committed panel
  // (revision meta + stale-basis reconcile + Amend affordance).
  if (sessionMissing && committed && committedEnvelope !== undefined) {
    return (
      <CommittedArtifactPanel
        amendPending={amendPending}
        provenance={committedProvenance}
        revisions={committedRevisions}
        onAmend={onAmend}
      >
        {proseSurface(
          committedEnvelope.kind,
          <ArtifactRenderer
            envelope={committedEnvelope}
            height={620}
            title={title}
            useCasesEnvelope={useCasesEnvelope}
          />
        )}
      </CommittedArtifactPanel>
    );
  }

  if (!hasDraft || sessionMissing) {
    return (
      <Paper sx={{ p: 6, textAlign: 'center', borderStyle: 'dashed' }}>
        <AutoAwesomeIcon sx={{ fontSize: 30, color: t.accent }} />
        <Typography sx={{ fontFamily: t.mono, mt: 1, color: t.ink }}>No draft yet.</Typography>
        <Typography sx={{ color: t.muted, display: 'block', mb: 2 }} variant="caption">
          {blurb}
        </Typography>
        <Button
          color="primary"
          data-testid={UI_IDENTIFIERS.DesignExperience.REQUEST_DRAFT}
          disabled={beginPending}
          startIcon={<AutoAwesomeIcon />}
          variant="contained"
          onClick={onBegin}
        >
          Request draft
        </Button>
      </Paper>
    );
  }

  const gateOpen = stage === 'awaitingReview';
  return (
    <>
      {/* Draft framing stays as an inline note; the committed framing moved to the
          header (?) info popover and staleness to the header chip, so a committed
          step's first paint is content, not banners (UX-P1-4/P2-10/R7). */}
      {committed ? null : <ArtifactIntro committed={false} kind={activeKind} />}
      <Box sx={{ mb: gateOpen ? 3 : 0 }}>
        {proseSurface(
          view?.draft.kind ?? activeKind,
          <ArtifactRenderer
            envelope={view?.draft}
            height={620}
            title={title}
            useCasesEnvelope={useCasesEnvelope}
          />
        )}
      </Box>
      {/* QA F35 / F-GTD-12b / F-QA2-41: a contained approve/merge-window fault returns the
          session to awaitingReview carrying failureReason — without this the reviewer just
          sees AWAITING YOU again and the approve looks like a silent no-op. Keyed by the
          reason so a NEW fault re-surfaces after a dismissal. */}
      {gateOpen && failureReason !== undefined ? (
        <ApproveFaultBanner key={failureReason} reason={failureReason} />
      ) : null}
      {gateOpen ? (
        <GatePanel
          allowEmptySendBack={allowEmptySendBack}
          commentCount={commentCount}
          critique={view?.critique}
          findings={findings}
          gateError={gateError}
          openCommentCount={openCommentCount}
          pending={decisionPending}
          onApprove={onApprove}
          onSendBack={onSendBack}
          onWithdraw={onWithdraw}
        />
      ) : null}
    </>
  );
}

/**
 * The approve-fault notice rendered above the commit-authority bar when a
 * contained merge-window fault returned the session to the review gate
 * (F-QA2-41). Dismissible (founder direction) — the parent keys this component
 * by the reason text, so dismissal is per-notice and a NEW fault re-surfaces.
 * MUI Alert already carries role="alert"; stated explicitly for the contract.
 * Exported for the Phase-2 twin gate (routes/ProjectDesignExperience.tsx).
 */
export function ApproveFaultBanner({ reason }: { reason: string }): ReactNode {
  const [dismissed, setDismissed] = useState(false);
  if (dismissed) return null;
  return (
    <Alert
      data-testid={UI_IDENTIFIERS.DesignExperience.APPROVE_FAULT}
      role="alert"
      severity="warning"
      sx={{ mb: 2 }}
      onClose={() => {
        setDismissed(true);
      }}
    >
      {reason} If approving again fails the same way, a send-back refreshes the draft from main.
    </Alert>
  );
}
