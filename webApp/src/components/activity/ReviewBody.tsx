/**
 * The body of a REVIEW task: who is on the gate, what they are judging, and the
 * one bar every verb converges through.
 *
 *   TaskHeader       the shared header + revision select (the dispatch body's own)
 *   ReviewersStrip   the live proposal, the round's roster, the recorded verdicts
 *   ArtifactPanel    the artifact, through `taskArtifactFor`'s dispatcher
 *   SubmitBar        approve / send back / ask, with the M0 gate's overrides
 *
 * ── The thread count comes from ONE place ───────────────────────────────────
 * `openThreads` is `openThreadCount(thread)` (threadAdapter.ts), the same number
 * the comment margin's cards are built from. Nothing else computes it, so the
 * bar's "Resolve N threads to approve" and the margin can never disagree.
 *
 * ── A navigation, deliberately not a verb ───────────────────────────────────
 * The Project Design M0 gate has no send-back (spec R7): the plan is DERIVED, so
 * changing it means amending the Architecture. That is a different activity, so
 * it is a link — under `Activity.AMEND_ARCHITECTURE`, beside the bar rather than
 * on it. A `components/` file may import `contracts/` and may NOT import
 * `routes/` or call `useNavigate`, so the navigation itself is the container's
 * `onNavigate` callback.
 *
 * ── Why no `VerbsFor` prop ──────────────────────────────────────────────────
 * `containers/activityVerbs.ts` owns the verb table, and `components → containers`
 * is lint-fatal. The container resolves the table and hands this body the two
 * facts the bar actually renders (`allowSendBack`, `approveCopy`) plus the
 * callbacks. The body never learns which op it is firing, which is the point.
 *
 * ── The read-only history (R1) ──────────────────────────────────────────────
 * A non-latest revision hands this body `historyCaption` and NO `live`, `stale`
 * or `advance`: the artifact keeps its caption saying it is the CURRENT one (no
 * op reads an artifact as of a ref — GAP-5), the recorded verdicts and the round's
 * note are what the reader came for, and every affordance that would change
 * something is simply not rendered. Nothing here is a disabled control.
 *
 * Pure and props-only.
 */
import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Button from '@mui/material/Button';
import Typography from '@mui/material/Typography';
import ArrowForwardIcon from '@mui/icons-material/ArrowForward';

import { ArtifactPanel } from './ArtifactPanel';
import { ReviewersStrip } from './ReviewersStrip';
import { TaskHeader } from './TaskHeader';
import {
  ADVANCE_ANYWAY,
  ADVANCE_RETRY,
  advanceFailed,
  AMEND_ARCHITECTURE,
  CONSTRUCTION_THREAD_READ_ONLY,
  REVISION_NOTE_LABEL,
} from './activityCopy.ts';
import { StaleBasisHeaderChip } from '../design/StaleBasisChip';
import type { TaskFacts } from './activityViewToGraph.ts';
import type { LifecycleRevision } from './lifecycleGraphTypes.ts';
import { ARCHITECTURE_ACTIVITY_ID, type TaskArtifact } from './taskArtifactFor.ts';
import { SubmitBar } from '../design/SubmitBar';
import type { ArtifactActivityVM } from '../construction/artifactRenderers';
import type { ContractJoin } from '../../contracts/serviceContracts';
import { ACTIVITY_PATH } from '../../contracts/routePaths.ts';
import type { components } from '../../contracts/schema.ts';
import type {
  ArtifactModelEnvelope,
  ArtifactSlotView,
  ProjectStateWithGit,
} from '../../contracts/types';
import { useTokens } from '../../utilities/theme/ThemeContext';
import { UI_IDENTIFIERS } from '../../utilities/constants/UIIdentifiers';

export function ReviewBody({
  title,
  facts,
  revisions,
  revision,
  onRevision,
  reviewSet,
  reviewSetError,
  roster,
  verdicts,
  artifact,
  slots,
  vm,
  project,
  systemEnvelope,
  contractJoin,
  sdp,
  historyCaption,
  note,
  stale,
  advance,
  live,
  openThreads,
  stagedChangeRequests,
  stagedQuestions,
  decisionPending,
  askPending,
  allowSendBack,
  allowAsk,
  approveCopy,
  threadReadOnly,
  onApprove,
  onSendBack,
  onAsk,
  onRetry,
  onNavigate,
}: {
  title: string;
  facts: TaskFacts;
  revisions: readonly LifecycleRevision[];
  revision: number;
  onRevision: (n: number) => void;
  reviewSet?: components['schemas']['DeliveryReviewSet'] | undefined;
  reviewSetError?: string | undefined;
  roster?: readonly components['schemas']['DeliveryReviewRosterSeat'][] | undefined;
  verdicts?: readonly components['schemas']['DeliveryReviewVerdictView'][] | undefined;
  artifact: TaskArtifact;
  slots: readonly ArtifactSlotView[];
  vm: ArtifactActivityVM | undefined;
  project: ProjectStateWithGit | undefined;
  systemEnvelope: ArtifactModelEnvelope | undefined;
  contractJoin: ContractJoin | undefined;
  /**
   * Project Design M0 only: how the chosen option reaches the bar. OMITTED on a
   * read-only history, where the option radiogroup goes inert with it.
   */
  sdp?: { onChoose: (optionId: string) => void } | undefined;
  /**
   * The caption over the artifact while a NON-LATEST revision is on screen
   * (`HISTORY_ARTIFACT_CAPTION`): what is rendered is the CURRENT artifact, not
   * the one this revision judged. Its presence is what puts the panel in
   * read-only mode. `undefined` on the head.
   */
  historyCaption: string | undefined;
  /** This revision's send-back note, verbatim (`ConstructionTaskRevisionView.note`). */
  note: string | undefined;
  /**
   * The committed slot this gate judges has a drifted upstream basis — the two
   * non-blocking ways out (design rails only; omitted on a read-only history and
   * wherever the artifact is not a committed slot).
   */
  stale?:
    | {
        /** The upstream slot that drifted, when the read model names it. */
        cause: string | undefined;
        ackPending: boolean;
        ackError: string | undefined;
        onReconcile: () => void;
        onAcknowledge: (note: string) => void;
      }
    | undefined;
  /**
   * The M0 approve is commit-then-advance, and the ADVANCE failed: the plan of
   * record is bound and construction is not running. Rendered independently of
   * {@link live}, because by then the gate is decided and the bar is gone —
   * which is exactly when the failure would otherwise be invisible.
   */
  advance?:
    | {
        error: string;
        /** The FailedPrecondition over stale committed slots (F55) — offer "advance anyway". */
        stale: boolean;
        pending: boolean;
        onRetry: () => void;
        onAdvanceAnyway: () => void;
      }
    | undefined;
  /** A decision is owed on THIS revision — the bar renders only then. */
  live: boolean;
  /** `openThreadCount(thread)` — nothing else computes this number. */
  openThreads: number;
  stagedChangeRequests: number;
  stagedQuestions: number;
  decisionPending: boolean;
  askPending: boolean;
  allowSendBack: boolean;
  /**
   * This rail can send a question at all. False on every construction type
   * (R2/GAP-6): the bar then never offers an Ask, and a question staged before
   * the composer was hidden is reported by the bar's notice instead of turning
   * the one verb on the gate into a button that dispatches nothing.
   */
  allowAsk: boolean;
  approveCopy?: { label: string; consequence: string } | undefined;
  /** The rail behind this gate has no comment-status op (R2) — say so once. */
  threadReadOnly: boolean;
  onApprove: () => void;
  onSendBack: () => void;
  onAsk: () => void;
  onRetry: (() => void) | undefined;
  onNavigate: (path: typeof ACTIVITY_PATH, params: { activityId: string }) => void;
}): ReactNode {
  const t = useTokens();
  // The amendment link belongs to the M0 GATE and to nothing else. `allowSendBack`
  // alone would not do: a design review whose artifact kind could not be resolved
  // also offers no send-back, and "amend the Architecture" is not what is wrong
  // with it. The `sdpReview` slot is what names this gate.
  const amendable =
    !allowSendBack && artifact.kind === 'slot' && artifact.artifactKind === 'sdpReview';
  return (
    <Box
      data-testid={UI_IDENTIFIERS.Activity.REVIEW_BODY}
      sx={{ display: 'flex', flexDirection: 'column', gap: 2.5 }}
    >
      <TaskHeader
        facts={facts}
        revision={revision}
        revisions={revisions}
        title={title}
        onRevision={onRevision}
      />

      {stale !== undefined ? (
        <Box sx={{ display: 'flex' }}>
          <StaleBasisHeaderChip
            ackError={stale.ackError}
            acknowledgePending={stale.ackPending}
            cause={stale.cause}
            onAcknowledge={stale.onAcknowledge}
            onReconcile={stale.onReconcile}
          />
        </Box>
      ) : null}

      <ReviewersStrip
        error={reviewSetError}
        reviewSet={reviewSet}
        roster={roster}
        verdicts={verdicts}
      />

      {note !== undefined && note.length > 0 ? (
        <Box data-testid={UI_IDENTIFIERS.Activity.REVISION_NOTE} sx={{ minWidth: 0 }}>
          <Typography
            sx={{ fontFamily: t.mono, fontSize: 9.5, letterSpacing: '0.14em', color: t.muted }}
          >
            {REVISION_NOTE_LABEL}
          </Typography>
          <Typography sx={{ fontSize: 13.5, color: t.ink, lineHeight: 1.5 }}>{note}</Typography>
        </Box>
      ) : null}

      {threadReadOnly && openThreads > 0 ? (
        <Typography sx={{ fontFamily: t.mono, fontSize: 12, color: t.muted }}>
          {CONSTRUCTION_THREAD_READ_ONLY}
        </Typography>
      ) : null}

      {historyCaption !== undefined ? (
        <Typography
          data-testid={UI_IDENTIFIERS.Activity.HISTORY_CAPTION}
          sx={{ fontSize: 12.5, color: t.muted, lineHeight: 1.5 }}
        >
          {historyCaption}
        </Typography>
      ) : null}

      <ArtifactPanel
        artifact={artifact}
        contractJoin={contractJoin}
        project={project}
        readOnly={historyCaption !== undefined}
        sdp={sdp}
        slots={slots}
        systemEnvelope={systemEnvelope}
        vm={vm}
      />

      {amendable ? (
        <Box sx={{ display: 'flex', justifyContent: 'flex-end' }}>
          <Button
            data-testid={UI_IDENTIFIERS.Activity.AMEND_ARCHITECTURE}
            endIcon={<ArrowForwardIcon sx={{ fontSize: 16 }} />}
            size="small"
            sx={{ fontFamily: t.mono, color: t.ink }}
            variant="text"
            onClick={() => {
              onNavigate(ACTIVITY_PATH, { activityId: ARCHITECTURE_ACTIVITY_ID });
            }}
          >
            {AMEND_ARCHITECTURE}
          </Button>
        </Box>
      ) : null}

      {advance !== undefined ? (
        <Box
          data-testid={UI_IDENTIFIERS.Activity.ADVANCE_ERROR}
          role="alert"
          sx={{
            display: 'flex',
            alignItems: 'center',
            gap: 1.5,
            flexWrap: 'wrap',
            px: 2,
            py: 1.5,
            bgcolor: t.paperAlt,
            border: `1.5px solid ${t.dangerFg}`,
            borderRadius: t.radius / 8 + 0.5,
          }}
        >
          <Typography sx={{ fontSize: 13, color: t.dangerFg, lineHeight: 1.5, minWidth: 240 }}>
            {advanceFailed(advance.error)}
          </Typography>
          <Box sx={{ flexGrow: 1 }} />
          <Button
            data-testid={UI_IDENTIFIERS.Activity.ADVANCE_RETRY}
            disabled={advance.pending}
            size="small"
            sx={{ fontFamily: t.mono, color: t.ink, borderColor: t.line, textTransform: 'none' }}
            variant="outlined"
            onClick={advance.onRetry}
          >
            {ADVANCE_RETRY}
          </Button>
          {advance.stale ? (
            <Button
              data-testid={UI_IDENTIFIERS.Activity.ADVANCE_ANYWAY}
              disabled={advance.pending}
              size="small"
              sx={{ fontFamily: t.mono, color: t.ink, borderColor: t.line, textTransform: 'none' }}
              variant="outlined"
              onClick={advance.onAdvanceAnyway}
            >
              {ADVANCE_ANYWAY}
            </Button>
          ) : null}
        </Box>
      ) : null}

      {live ? (
        <SubmitBar
          allowAsk={allowAsk}
          allowSendBack={allowSendBack}
          approveCopy={approveCopy}
          askPending={askPending}
          committed={false}
          openThreads={openThreads}
          pending={decisionPending}
          stage="awaitingReview"
          stagedChangeRequests={stagedChangeRequests}
          stagedQuestions={stagedQuestions}
          onAmend={noAmend}
          onApprove={onApprove}
          onAsk={onAsk}
          onRetry={onRetry}
          onSendBack={onSendBack}
        />
      ) : null}
    </Box>
  );
}

/**
 * `resolveSubmitVerb` only ever returns `amend` for a COMMITTED slot, and this
 * bar is always mounted with `committed: false` — a review task's artifact is
 * under review by definition, so there is nothing committed to amend from here.
 * The prop is required, so it is named rather than left as an inline no-op.
 */
function noAmend(): void {
  /* unreachable — see the comment above */
}
