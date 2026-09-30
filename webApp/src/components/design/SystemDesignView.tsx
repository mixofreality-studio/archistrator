/**
 * The pure System Design (Phase-1) screen. ALL data and callbacks arrive via
 * props — no hooks that reach into `api`/`hooks` (fetching, mutations) or into
 * ambient CommentContext. Extracted from `routes/DesignExperience.tsx`'s former
 * `SystemDesignBody`/`StepBody` pair (Task 8) so the SPA container
 * (`containers/SystemDesignContainer.tsx`) and, later, an MCP host (Task 9) can
 * both compose this same screen against two different data-and-comment substrates.
 *
 * Composition: ExperienceChrome (chrome + optional comment margin + SelectionPopover)
 * → SlimSpine (progress rail) → artifact header → StepBody (the active step's
 * content: research CTA / generating scene / committed panel / draft + gate) →
 * SubmitBar (Task 10 — the one surface every review verb converges through:
 * Send back / Approve / Amend / Ask, sticky at the bottom of the scroll column).
 *
 * ── SPA-only optional surfaces ───────────────────────────────────────────────
 * `margin` is an opaque ReactNode FACTORY (the SPA container wires its own
 * CommentMargin against CommentContext + review mutations); omitted,
 * ExperienceChrome renders no margin affordance at all. It takes the SHARED
 * scroll container that wraps this screen's content column and the margin
 * together — owned by ExperienceChrome since Task 8b, and passed straight through
 * from here; a plain ReactNode could not carry it across that seam.
 * `commentSurface` carries the minimal bit of
 * CommentContext state this pure screen itself needs (the local pending-comment
 * counts, split by type, that SubmitBar's `resolveSubmitVerb` picks a verb from,
 * and the anchor-arming callback SelectionPopover
 * uses); omitted, both default to "no local comment surface" (SelectionPopover
 * still falls back to ambient CommentContext when a Provider happens to wrap the
 * tree — see CommentContext.useComments — so nested per-item commentable
 * affordances deep in ArtifactRenderer keep working via context regardless).
 * `onSubmitSelectionComment` is a forward-compat hook for Task 9's MCP
 * comment-submission flow; SelectionPopover only ARMS an anchor today (arming is
 * what opens the margin's in-place draft card, which lives entirely inside the
 * opaque `margin` slot), so this screen does not yet wire it to anything — Task 9
 * owns designing the MCP submit path.
 */
import { useState, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import Chip from '@mui/material/Chip';
import Button from '@mui/material/Button';
import Alert from '@mui/material/Alert';
import Tooltip from '@mui/material/Tooltip';
import AutoAwesomeIcon from '@mui/icons-material/AutoAwesome';

import type {
  ArtifactKind,
  ArtifactModelEnvelope,
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
import { DraftFailedPanel } from './DraftFailedPanel';
import { designAmendmentInFlight } from './liveDesignGate.ts';
import { GatePanel } from './GatePanel';
import { SubmitBar } from './SubmitBar';
import { CommittedArtifactPanel, CommittedChip } from './CommittedArtifactPanel';
import { StaleBasisHeaderChip } from './StaleBasisChip';
import { ResearchInputPanel } from './ResearchInputPanel';
import { SkeletonContentCard } from './DesignSkeleton';

import { useTokens } from '../../utilities/theme/ThemeContext';
import type { Tokens } from '../../utilities/theme/themes';
import { UI_IDENTIFIERS } from '../../utilities/constants/UIIdentifiers';

// Prose (markdown) artifact kinds get a paper surface in the full-screen design
// experience; diagram kinds (volatilities/system/coreUseCases) render on their own
// bordered canvases, so they stay unwrapped. The retired-in-place scrubbedRequirements
// and standardCheck kinds left this set with their steps.
const PROSE_ARTIFACT_KINDS = new Set<string>(['mission']);

/** Seed rationale for a reconcile-via-amendment fired from the stale banner (F45). */
const RECONCILE_RATIONALE = 'Reconcile with amended upstream basis.';

/** SubmitBar's Ask verb never resolves without a staged question — see
 *  `resolveSubmitVerb` — so this stands in for `onAsk` wherever the container
 *  hasn't wired one (MCP has no client-side comment accumulator to ask about). */
const NOOP = (): void => undefined;

// Floor height for a fill-mode artifact card (today only the glossary): the card
// grows to fill the scroll area on a tall viewport, but never shrinks below this,
// so a short viewport scrolls the outer container instead of collapsing the card
// to its sticky search header. A minimum, not a fixed size.
const FILL_MIN_HEIGHT = 360;

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
 * not-yet-sent comment counts (feed the submit bar's `resolveSubmitVerb`) and the
 * anchor-arming callback SelectionPopover commits into. SPA-only; the container
 * builds this from its own `useComments()` call (it renders inside
 * CommentProvider). Omitted in MCP.
 */
export interface CommentSurfaceProps extends SelectionCommentSurface {
  /**
   * Counts of accumulated, not-yet-sent comments this gate cycle, split by type
   * (Task 10): the submit bar picks Send back/Amend vs. Ask from exactly this
   * split — a single merged count (the pre-Task-10 shape) could not tell a staged
   * question from a staged change request.
   */
  changeRequestCount: number;
  questionCount: number;
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
  /**
   * Request a fresh draft (no feedback) or an amendment (feedback = rationale). `onAccepted`,
   * when supplied on an amend, fires only after the server accepts the request (mutation
   * onSuccess) so the composer clears its folded rail comments solely on success.
   */
  onRequestDraft: (feedback?: string, onAccepted?: () => void) => void;
  onRetry: () => void;
  /** Close the experience (the ✕ affordance). */
  onClose: () => void;
  /** The co-author session query is still in flight (distinct from `project` load,
   *  which gates whether this screen mounts at all). */
  sessionLoading: boolean;
  /** The active step has no co-author session yet (404) — distinct from loading. */
  sessionMissing: boolean;
  /**
   * WHETHER THIS ARTIFACT ACTUALLY AWAITS A HUMAN, from the LIVE authority
   * (`liveDesignGate.ts` over `QueryActivityView`). It opens the approve/reject
   * gate. It is a PROP and not a stage read because the derived session door cannot
   * answer it: its whole vocabulary is {unknown, committed, withdrawn, draftFailed},
   * so the `stage === 'awaitingReview'` this replaces had no producer left and the
   * gate could never open for anyone.
   */
  awaitingHuman: boolean;
  /**
   * THIS ARTIFACT'S DRAFT/AMENDMENT IS RUNNING RIGHT NOW, from the same live read
   * (`liveDesignGate.ts`). It is the other half of the stale-basis ack's refusal: the
   * derived door's `drafting`/`redrafting` lost their producer with the co-author
   * workflow, and without this the ack stayed ENABLED over an amendment in flight —
   * exactly the merge conflict its own sentence warns about. Defaults false, which is
   * what a surface with no activity read (and the loading tick) honestly knows.
   */
  dispatchRunning?: boolean | undefined;
  /**
   * The failed design job's run URL, read off the failed dispatch attempt's own
   * sentence (`liveDesignGate.ts`). Replaces `SessionStateView.failureRunUrl`, a
   * wire field nothing ever set. Absent → DraftFailedPanel simply omits the link.
   */
  failedRunUrl?: string | undefined;
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
  /**
   * THE LIVE DESIGN-HEALTH FINDINGS for this project, from
   * `QueryProjectView{designHealth}` — the gate's machine validation.
   *
   * They are the SERVER'S, and that is the point: the thirteen rules the co-author
   * session used to serve were moved into the `designhealth` engine, so
   * `SessionStateView.findings` now carries only the session's own
   * critique-unresolved warning. Both are shown at the gate, health first, because
   * they are two different kinds of fact about one draft — a rule the platform checks,
   * and a critic the platform ran. Omitted where a host has no design-health read; the
   * session's own findings then stand alone, as they did before.
   *
   * They are PROJECT-scoped, not per-slot: the view carries no artifact-kind
   * attribution (a finding's `location.section` is free text), so the gate shows the
   * project's live health rather than a filtered subset that would silently drop the
   * rules whose section nobody mapped.
   */
  designHealthFindings?: readonly Finding[] | undefined;
  onAcknowledgeStale: (note: string) => void;
  acknowledgeStalePending: boolean;
  acknowledgeStaleError?: string | undefined;
  /**
   * Submit the staged QUESTIONS without a redraft — the submit bar's Ask verb
   * (Task 10). Omitted where asking is not wired (MCP has no client-side comment
   * accumulator, so a staged question never exists there to ask about).
   */
  onAsk?: (() => void) | undefined;
  /** The AskQuestions mutation is in flight — the submit bar's Ask action disables. */
  askPending?: boolean;
  /**
   * SPA default (false, unset): the submit bar's Send back stays gated on
   * `commentSurface.changeRequestCount > 0`. MCP (no client-side comment
   * accumulator) passes `true` so the click is reachable — see SubmitBar's own
   * doc comment for why that doesn't weaken the "redraft always carries
   * guidance" invariant.
   */
  allowEmptySendBack?: boolean;
  // ── SPA-only optional surfaces (see file header) ──────────────────────────
  /**
   * Builds the comment margin, given the SHARED scroll container that wraps this
   * column and the margin together (owned by ExperienceChrome). Passed straight
   * through — this screen never sees the element.
   */
  margin?: ((scrollRoot: HTMLElement | null) => ReactNode) | undefined;
  marginOpen?: boolean | undefined;
  onOpenMargin?: (() => void) | undefined;
  commentSurface?: CommentSurfaceProps;
  /** Reserved for Task 9's MCP two-call (arm, then submit) comment flow. */
  onSubmitSelectionComment?: (anchor: Anchor, text: string) => void;
  /**
   * The SP1 capture-seam episodes panel for the active artifact (Task 10) —
   * an opaque, pre-built ReactNode (`<EpisodesPanelContainer targetRef={activeKind} .../>`),
   * wired by the container (this is a pure components-layer file: it may not
   * import hooks/containers itself — eslint.platform.config.js:53). Rendered
   * below the artifact renderer.
   */
  episodesSlot?: ReactNode;
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
  awaitingHuman,
  dispatchRunning = false,
  failedRunUrl,
  needsResearch,
  onSubmitResearch,
  researchPending,
  beginPending,
  retryPending,
  amendPending,
  decisionPending,
  gateError,
  designHealthFindings,
  onAcknowledgeStale,
  acknowledgeStalePending,
  acknowledgeStaleError,
  onAsk,
  askPending = false,
  allowEmptySendBack = false,
  margin,
  marginOpen,
  onOpenMargin,
  commentSurface,
  episodesSlot,
}: SystemDesignViewProps): ReactNode {
  const t = useTokens();
  // The scroll container is NOT this screen's any more: ExperienceChrome owns the
  // ONE scroller that wraps this column AND the margin (Task 8b), so `margin` goes
  // through to the chrome as the factory it already was and the chrome feeds it
  // the element.
  // The amend composer dialog's open state, lifted out of CommittedArtifactPanel
  // (RULING P6): Task 10's submit-bar Amend button sets this directly — no
  // imperative handle, no ref.
  const [amendOpen, setAmendOpen] = useState(false);

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
  // The committed System envelope (the reverse join): lets the use-case carousel
  // offer "View call chain" into the Architecture step's Dynamic lens.
  const systemEnvelope = project.slots.find((s) => s.kind === 'system')?.model;
  const committedRevisions = committedSlot?.revisions;
  const committedProvenance = committedSlot?.provenance;
  const committedStale = committedSlot?.staleBasis === true;
  const committedStaleCause = committedSlot?.staleCause;
  const hasDraft = view?.draft.model !== undefined;
  // The gate's machine validation: the server's live design-health findings. The session
  // used to contribute its own half here; `SessionStateView.findings` had zero producers
  // and left the wire at stage 4b2, so the merge was an append of an always-empty list.
  const findings = [...(designHealthFindings ?? [])];
  const reviewThread = view?.reviewThread ?? [];
  const openCommentCount = reviewThread.filter((c) => c.status === 'open').length;
  // `drafting` / `redrafting` / `refused` are NOT tested here any more, and their branches
  // are gone with them (stage 4b2). The door this screen reads is the DERIVED session view:
  // a projection of one durable slot, whose whole stage vocabulary is
  // {committed, withdrawn, draftFailed}. The live stages had no producer once 4b1 retired
  // the co-author workflow, so the generating scene and the `refused` arm of the failure
  // panel could not render for anyone.
  //
  // `awaitingReview` was the LAST of them, and it is gone from this file too — but as a
  // RE-POINT, not a deletion (Task 9, controller ruling 1): the gate now opens on the
  // `awaitingHuman` prop, which the container reads from `QueryActivityView`, the live
  // authority 4b1 made canonical for design activities. A stage vocabulary that cannot
  // say "a human is owed a decision" is not the place to ask.
  const draftFailed = stage === 'draftFailed';
  const failureReason = view?.failureReason;
  const activeCommitted = spine[safeIndex]?.committed === true;
  const stagedChangeRequests = commentSurface?.changeRequestCount ?? 0;
  const stagedQuestions = commentSurface?.questionCount ?? 0;

  // Whether StepBody's committed-panel arm (below) is what renders: when it does,
  // the header renders CommittedChip instead of StageChip (Task 9 — the chip
  // replaces the old full-width strip, so it is the signal, not a second one).
  // This mirrors StepBody's early-return sequence up to the F-GTD-11 committed-panel
  // guard.
  const showsCommittedPanel =
    !needsResearch &&
    !draftFailed &&
    !(sessionLoading && view === undefined) &&
    (sessionMissing || stage === 'committed') &&
    activeCommitted &&
    committedEnvelope !== undefined;

  // The submit bar (Task 10) mounts wherever GatePanel or the committed panel's
  // Amend affordance would otherwise be the review surface: a draft under review
  // (gateOpen), or a committed slot with nothing else showing. It renders nothing
  // itself once mounted with nothing to do (see SubmitBar's own doc comment).
  const gateOpen = awaitingHuman;
  const showSubmitBar = gateOpen || showsCommittedPanel;
  const submitStage: 'drafted' | 'awaitingReview' | 'other' = gateOpen ? 'awaitingReview' : 'other';
  // Withdraw only applies while a draft sits under review — a clean committed
  // slot (the OTHER case showSubmitBar mounts for) has no live session to
  // withdraw. Named (not inline) so its return type is explicit regardless of
  // the ternary around it.
  const onWithdrawFromBar = gateOpen
    ? (): void => {
        onSubmitReview('withdraw');
      }
    : undefined;

  // F-GTD-12: while this artifact's own session is LIVE (an amendment in flight — a
  // committed slot can only host an amendment), the ack would commit to main and
  // merge-conflict the amendment's review PR. Gate the popover action too so the refusal
  // is explained instead of discovered.
  //
  // The three live stages in the old disjunct (`drafting`/`awaitingReview`/`redrafting`)
  // lost their producer on the derived door, so this had collapsed to `draftFailed` alone —
  // the one member that needs the refusal LEAST. Both halves are restored from the live
  // authority now: `awaitingHuman` (a draft sitting at its gate) and `dispatchRunning` (a
  // draft or redraft actually running, which is the case the sentence below names). The
  // predicate itself lives in `liveDesignGate.ts` so the node suite can hold it — a rule
  // that only exists inside a .tsx is a rule that can collapse again with CI green.
  const sessionLive = designAmendmentInFlight({ awaitingHuman, dispatchRunning, stage });
  const ackDisabledReason = sessionLive
    ? 'An amendment is already in flight for this artifact — reconcile rides it. Approve or withdraw the amendment first.'
    : undefined;

  return (
    <ExperienceChrome
      // ALWAYS shared, margin or no margin: this screen's content column carries no
      // overflow of its own, and the MCP host composes it with no margin wired.
      bodyScroll="shared"
      commentSurface={commentSurface}
      margin={margin}
      marginOpen={marginOpen}
      phaseNum={1}
      phaseTitle="System Design"
      projectName={project.name}
      spine={<SlimSpine activeIndex={safeIndex} steps={spine} onSelect={onSelectStep} />}
      onClose={onClose}
      onOpenMargin={onOpenMargin}
    >
      <Box
        sx={{
          flexGrow: 1,
          minWidth: 0,
          // A flex column so a fill-mode artifact card (the glossary) can grow to the
          // bottom of the scroll viewport instead of sitting at a fixed height with
          // dead space below it. Prose/diagram bodies carry no flexGrow, so they stay
          // at their natural height at the top — unchanged.
          //
          // No scroll and no height of its own: ExperienceChrome's page box (which
          // is at least a viewport tall) stretches this column, so the glossary's
          // fill still works and a taller artifact still grows the page (Task 8b).
          display: 'flex',
          flexDirection: 'column',
          px: { xs: 2, md: 4 },
          py: 3,
        }}
      >
        {/* artifact header */}
        <Box sx={{ display: 'flex', alignItems: 'flex-start', gap: 1.5, mb: 2, flexShrink: 0 }}>
          <Box sx={{ minWidth: 0 }}>
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, flexWrap: 'wrap' }}>
              <Typography component="h1" sx={{ color: t.ink }} variant="h4">
                {meta.title}
              </Typography>
              {/* Suppressed when the committed-panel strip below already shows COMMITTED
                  (+ Amend), so the header chip is not a duplicate signal. */}
              {showsCommittedPanel ? (
                <CommittedChip provenance={committedProvenance} revisions={committedRevisions} />
              ) : (
                <StageChip stage={headerChipStage(activeCommitted, stage)} />
              )}
              {/* Committed framing copy moved off the full-width banner into a (?) info
                  popover; staleness moved off the amber banner into a compact chip —
                  so the first paint of a committed step is content, not banners. The
                  slot's state address (project.json path) rides along here too (Task 9),
                  off its own always-visible subtitle line. */}
              {activeCommitted ? (
                <ArtifactInfoButton kind={activeKind} stateAddress={meta.stateAddress} />
              ) : null}
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
          </Box>
          <Box sx={{ flexGrow: 1 }} />
          <Tooltip title="drafts: architect Worker">
            <Chip
              label="architect"
              size="small"
              sx={{ bgcolor: t.chatArchitectBg, color: t.chatArchitectFg }}
              variant="outlined"
            />
          </Tooltip>
          {meta.hasPmCritic ? (
            <Tooltip title="critiques & ratifies: PM Worker">
              <Chip
                label="pm"
                size="small"
                sx={{ bgcolor: t.chatPmBg, color: t.chatPmFg }}
                variant="outlined"
              />
            </Tooltip>
          ) : null}
        </Box>

        {/* body */}
        <StepBody
          activeKind={activeKind}
          amendOpen={amendOpen}
          amendPending={amendPending}
          awaitingHuman={awaitingHuman}
          beginPending={beginPending}
          blurb={meta.blurb}
          committed={activeCommitted}
          committedEnvelope={committedEnvelope}
          draftFailed={draftFailed}
          failedRunUrl={failedRunUrl}
          failureReason={failureReason}
          findings={findings}
          gateError={gateError}
          hasDraft={hasDraft}
          loading={sessionLoading}
          needsResearch={needsResearch}
          openCommentCount={openCommentCount}
          researchPending={researchPending}
          retryPending={retryPending}
          sessionMissing={sessionMissing}
          stage={stage}
          systemEnvelope={systemEnvelope}
          t={t}
          title={meta.title}
          useCasesEnvelope={useCasesEnvelope}
          view={view}
          withdrawPending={decisionPending}
          onAmend={onRequestDraft}
          onAmendOpenChange={setAmendOpen}
          onBegin={() => {
            onRequestDraft(undefined);
          }}
          onRetry={onRetry}
          onSubmitResearch={onSubmitResearch}
          onWithdraw={() => {
            onSubmitReview('withdraw');
          }}
        />

        {/* The submit bar (Task 10): the one surface every review verb converges
            through — Send back / Approve / Amend / Ask, replacing the header's old
            Amend button, the gate's own action row, and the margin foot's Ask.
            Sticky at the bottom of this scroll column. */}
        {showSubmitBar ? (
          <SubmitBar
            allowEmptySendBack={allowEmptySendBack}
            askPending={askPending}
            committed={activeCommitted}
            openThreads={openCommentCount}
            pending={decisionPending}
            stage={submitStage}
            stagedChangeRequests={stagedChangeRequests}
            stagedQuestions={stagedQuestions}
            withdrawPending={decisionPending}
            onAmend={() => {
              setAmendOpen(true);
            }}
            onApprove={() => {
              onSubmitReview('approve');
            }}
            onAsk={onAsk ?? NOOP}
            onSendBack={() => {
              onSubmitReview('reject');
            }}
            onWithdraw={onWithdrawFromBar}
          />
        ) : null}

        {/* SP1 capture-seam episodes panel — below the artifact renderer. */}
        {episodesSlot !== undefined && <Box sx={{ mt: 2, flexShrink: 0 }}>{episodesSlot}</Box>}
      </Box>
    </ExperienceChrome>
  );
}

function StepBody({
  t,
  activeKind,
  amendOpen,
  committed,
  committedEnvelope,
  useCasesEnvelope,
  systemEnvelope,
  loading,
  needsResearch,
  draftFailed,
  failedRunUrl,
  failureReason,
  hasDraft,
  sessionMissing,
  awaitingHuman,
  stage,
  title,
  blurb,
  view,
  findings,
  openCommentCount,
  gateError,
  beginPending,
  researchPending,
  retryPending,
  withdrawPending,
  amendPending,
  onBegin,
  onRetry,
  onSubmitResearch,
  onWithdraw,
  onAmend,
  onAmendOpenChange,
}: {
  t: Tokens;
  activeKind: ArtifactKind;
  /** The amend composer dialog's open state, lifted from CommittedArtifactPanel (RULING P6). */
  amendOpen: boolean;
  committed: boolean;
  committedEnvelope: ArtifactModelEnvelope | undefined;
  /** The committed coreUseCases envelope (F-QA2-51 dynamic-view label fallback). */
  useCasesEnvelope: ArtifactModelEnvelope | undefined;
  /** The committed System envelope (the carousel's "View call chain" join). */
  systemEnvelope: ArtifactModelEnvelope | undefined;
  loading: boolean;
  needsResearch: boolean;
  draftFailed: boolean;
  /** The failed design job's run URL, from the failed attempt's own sentence. */
  failedRunUrl: string | undefined;
  failureReason: string | undefined;
  hasDraft: boolean;
  sessionMissing: boolean;
  /** A human decision is owed on this artifact right now (the LIVE authority). */
  awaitingHuman: boolean;
  stage: string | undefined;
  title: string;
  blurb: string;
  view: SessionStateResponse['view'] | undefined;
  findings: Finding[];
  openCommentCount: number;
  gateError: string | undefined;
  beginPending: boolean;
  researchPending: boolean;
  retryPending: boolean;
  withdrawPending: boolean;
  amendPending: boolean;
  onBegin: () => void;
  onRetry: () => void;
  onSubmitResearch: (research: ResearchInput) => void;
  onWithdraw: () => void;
  onAmend: (feedback: string, onAccepted: () => void) => void;
  onAmendOpenChange: (open: boolean) => void;
}): ReactNode {
  if (needsResearch) {
    return <ResearchInputPanel pending={researchPending} onSubmit={onSubmitResearch} />;
  }
  // The terminal-failure panel. `draftFailed` is the ONLY failure stage this door can
  // report since stage 4b2 — its `refused` sibling had no producer — so the panel is
  // always the async, CI-job framing and always offers Withdraw alongside Retry. Its
  // deep-link to the failed run is BACK (Task 9): `SessionStateView.failureRunUrl` was a
  // wire field nothing ever set, but the failed dispatch attempt's own sentence carries
  // the URL, and `liveDesignGate` reads it out of the activity view. No URL in the
  // sentence, no link — nothing is fabricated.
  if (draftFailed) {
    return (
      <DraftFailedPanel
        async
        artifact={title}
        // A failed Retry/Withdraw decision surfaces inline here too (2026-07-16
        // incident: dead-session decisions 503'd with zero feedback rendered).
        gateError={gateError}
        pending={retryPending}
        reason={failureReason}
        runUrl={failedRunUrl}
        withdrawPending={withdrawPending}
        onRetry={onRetry}
        onWithdraw={onWithdraw}
      />
    );
  }
  // The `generating` branch stood here and is DELETED (stage 4b2). It rendered the
  // GeneratingScene — with the server-reported activeRole / activeStep / round and the
  // live run URL — for stage `drafting` or `redrafting`, and it framed an
  // amendment-in-flight by showing the committed revision above it. None of those stages
  // and none of those fields survive on the derived door, so the branch was unreachable
  // and the scene never painted here. GeneratingScene itself is alive and in use on the
  // ACTIVITY rail (DispatchBody), which passes its own role line.

  // The project head-state has resolved by now (the container renders the
  // full-screen skeleton while it is in flight, before this screen mounts at
  // all), so the surrounding header/chip/spine are already truthful. Only the
  // co-author session is still loading — sketch the content card instead of a
  // bare spinner so it stays consistent with the design system.
  if (loading && view === undefined) {
    return <SkeletonContentCard t={t} />;
  }
  // When the slot is committed and no review is in progress — either the co-author
  // session is gone (404) or it has reached its terminal 'committed' stage — render
  // the committed model read-only under the committed panel (revision meta +
  // stale-basis reconcile + Amend affordance). Without the stage==='committed' arm a
  // freshly-approved artifact loses its Amend affordance until the session ages out
  // (F-GTD-11): the architect could no longer reopen a clean committed slot.
  if ((sessionMissing || stage === 'committed') && committed && committedEnvelope !== undefined) {
    // Same fill rule as the live-session path: a self-scrolling committed card (the
    // glossary) grows to the bottom of the scroll area (no dead region below it).
    // The committed chip + Amend affordance now live in the header / submit bar,
    // not a strip inside this panel.
    const committedFill = committedEnvelope.kind === 'glossary';
    return (
      <CommittedArtifactPanel
        amendOpen={amendOpen}
        amendPending={amendPending}
        fill={committedFill}
        fillMinHeight={FILL_MIN_HEIGHT}
        onAmend={onAmend}
        onAmendOpenChange={onAmendOpenChange}
      >
        {proseSurface(
          committedEnvelope.kind,
          <ArtifactRenderer
            envelope={committedEnvelope}
            fill={committedFill}
            height={620}
            systemEnvelope={systemEnvelope}
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

  const gateOpen = awaitingHuman;
  const draftKind = view?.draft.kind ?? activeKind;
  // Self-scrolling cards (today only the glossary) FILL the available height so the
  // committed/draft glossary grows to the bottom of the scroll area instead of a
  // fixed-height card with a dead region below it. Prose flows and the diagram kinds
  // keep their pixel canvas heights, so `fill` is scoped to the glossary here.
  const fill = draftKind === 'glossary';
  return (
    <>
      {/* Draft framing stays as an inline note; the committed framing moved to the
          header (?) info popover and staleness to the header chip, so a committed
          step's first paint is content, not banners (UX-P1-4/P2-10/R7). */}
      {committed ? null : <ArtifactIntro committed={false} kind={activeKind} />}
      <Box
        sx={{
          mb: gateOpen ? 3 : 0,
          ...(fill
            ? {
                flexGrow: 1,
                minHeight: FILL_MIN_HEIGHT,
                display: 'flex',
                flexDirection: 'column',
              }
            : {}),
        }}
      >
        {proseSurface(
          draftKind,
          <ArtifactRenderer
            envelope={view?.draft}
            fill={fill}
            height={620}
            systemEnvelope={systemEnvelope}
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
        <GatePanel findings={findings} gateError={gateError} openCommentCount={openCommentCount} />
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
