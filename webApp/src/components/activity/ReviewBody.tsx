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
 * changing it means amending what it derives from. An ARCHITECTURE change amends
 * the Architecture, a different activity, so it is a link — under
 * `Activity.AMEND_ARCHITECTURE`, beside the bar rather than on it. A `components/`
 * file may import `contracts/` and may NOT import `routes/` or call `useNavigate`,
 * so the navigation itself is the container's `onNavigate` callback. A SCOPE change
 * is the committed plan's own AMEND (below): the plan is re-derived with it.
 *
 * ── What a DECIDED gate still offers (spec §12 B7) ─────────────────────────
 * A decided gate has no decision bar, and three verbs live past the decision: a
 * committed design slot's AMEND (`AmendComposer`, the add-a-use-case / replan
 * entry — on the M0 gate, the committed plan's scope change), its ASK (a question staged on the committed slot — ask-a-clarifying-
 * question-during-review [no: committed] — sent by the bar in its committed mode,
 * which offers Ask and nothing else: change requests on a committed slot go
 * through the amend composer, and an unstaged committed slot renders no bar), and
 * a committed M0 plan's grace-window REVOKE (`RevokeStrip`). Each renders only when
 * the container hands its prop, which it does only on the head revision.
 *
 * Once an amendment is sent, the gate is no longer decided: its head is the
 * redraft, drafting or awaiting review, while the slot is still committed. The
 * body then says so (`amending`, the `AmendingNotice` the drafting scene carries
 * too): the committed revision stays current until the redraft is approved.
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

import { AmendComposer } from './AmendComposer';
import { ArtifactPanel, type SdpDecisionProps } from './ArtifactPanel';
import { RevokeStrip } from './RevokeStrip';
import { ReviewersStrip } from './ReviewersStrip';
import { TaskHeader } from './TaskHeader';
import {
  ADVANCE_ANYWAY,
  ADVANCE_RETRY,
  advanceFailed,
  AMEND_ARCHITECTURE,
  REVISION_NOTE_LABEL,
  sdpDecisionFailed,
} from './activityCopy.ts';
import { AmendingNotice } from '../design/GeneratingScene';
import { StaleBasisHeaderChip } from '../design/StaleBasisChip';
import type { TaskSpecCheckEvidence } from './TaskSpecPanel';
import type { LifecycleRevision } from './lifecycleGraphTypes.ts';
import { ARCHITECTURE_ACTIVITY_ID, type TaskArtifact } from './taskArtifactFor.ts';
import { SubmitBar } from '../design/SubmitBar';
import type {
  ArtifactActivityVM,
  AsyncArtifactData,
  TestingArtifactData,
} from '../construction/artifactRenderers';
import type { ContractJoin } from '../../contracts/serviceContracts';
import { ACTIVITY_PATH } from '../../contracts/routePaths.ts';
import type { components } from '../../contracts/schema.ts';
import type {
  TaskSpecView,
  ArtifactModelEnvelope,
  ArtifactSlotView,
  ProjectStateWithGit,
} from '../../contracts/types';
import { useTokens } from '../../utilities/theme/ThemeContext';
import { UI_IDENTIFIERS } from '../../utilities/constants/UIIdentifiers';

export function ReviewBody({
  title,
  spec,
  episodeCostUsd,
  checkEvidence,
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
  testing,
  sdp,
  revoke,
  sdpDecisionError,
  amend,
  amending,
  historyCaption,
  note,
  stale,
  costBasis,
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
  onApprove,
  onSendBack,
  onAsk,
  onRetry,
  onNavigate,
}: {
  title: string;
  /** The task's computed AgentTaskSpec, fetched by the container. */
  spec: AsyncArtifactData<TaskSpecView>;
  /** The selected revision's episode cost, when one was captured. */
  episodeCostUsd?: number | undefined;
  /** The spec's run evidence: the log texts and the file endpoint. */
  checkEvidence: TaskSpecCheckEvidence;
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
  /** The scenario plan / test run reads a component's testing phases render. */
  testing?: TestingArtifactData | undefined;
  /**
   * Project Design M0 only: how the chosen option reaches the bar, and the gate's
   * reject-all while a decision is owed. OMITTED on a read-only history, where the
   * option radiogroup goes inert with it.
   */
  sdp?: SdpDecisionProps | undefined;
  /** Project Design M0 only, on a COMMITTED plan: the grace-window revoke. */
  revoke?: { pending: boolean; onRevoke: () => void } | undefined;
  /** What the server said when it refused a reject-all or a revoke. */
  sdpDecisionError?: string | undefined;
  /** A COMMITTED design slot on its head revision: the amend exit. */
  amend?:
    | { pending: boolean; error: string | undefined; onSubmit: (rationale: string) => void }
    | undefined;
  /**
   * An amendment of this gate's COMMITTED slot is in flight: the committed revision
   * it would replace. `undefined` when the head is the decided revision itself, and
   * on a read-only history.
   */
  amending?: number | undefined;
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
   * WHAT THE COST ON THIS SCREEN WAS COMPUTED ON, when part of it was assumed
   * (`m0CostBasis.ts`). The M0 gate only: approving it binds the plan of record and
   * starts spending, so an assumed number the founder never saw is the one way this
   * screen can mislead. Rendered BESIDE the stale-basis chip and never instead of it
   * — they are two different warnings about the same committed basis — and `undefined`
   * whenever nothing was assumed.
   */
  costBasis?: string | undefined;
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
  /**
   * A decision is owed on THIS revision — the bar renders in its decision mode only
   * then (and in its committed, Ask-only mode on a committed slot's head: `amend`).
   */
  live: boolean;
  /** `openThreadCount(thread)` — nothing else computes this number. */
  openThreads: number;
  stagedChangeRequests: number;
  stagedQuestions: number;
  decisionPending: boolean;
  askPending: boolean;
  allowSendBack: boolean;
  /**
   * This gate can send a question at all. TRUE on every rail since stage 4b1 gave
   * construction `AskTaskQuestions`; what is left false is a design gate whose
   * artifact kind will not resolve, which loses every verb. When false the bar never
   * offers an Ask, and a question staged before the composer was hidden is reported by
   * the bar's notice instead of turning the one verb on the gate into a button that
   * dispatches nothing.
   */
  allowAsk: boolean;
  approveCopy?: { label: string; consequence: string } | undefined;
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
        revision={revision}
        revisions={revisions}
        spec={{ spec, episodeCostUsd, ...checkEvidence }}
        title={title}
        onRevision={onRevision}
      />

      {amending !== undefined ? <AmendingNotice committedRevision={amending} /> : null}

      {/* The two warnings about the committed basis, on ONE row: the chip (an
          upstream slot drifted) and the cost-basis line (part of the cost was
          assumed). Either may render alone; neither replaces the other. */}
      {stale !== undefined || costBasis !== undefined ? (
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.5, flexWrap: 'wrap' }}>
          {stale !== undefined ? (
            <StaleBasisHeaderChip
              ackError={stale.ackError}
              acknowledgePending={stale.ackPending}
              cause={stale.cause}
              onAcknowledge={stale.onAcknowledge}
              onReconcile={stale.onReconcile}
            />
          ) : null}
          {costBasis !== undefined ? (
            <Typography
              data-testid={UI_IDENTIFIERS.Activity.COST_BASIS}
              role="status"
              sx={{ fontSize: 12.5, color: t.awaitingFg, lineHeight: 1.5, minWidth: 0 }}
            >
              {costBasis}
            </Typography>
          ) : null}
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
        // Read-only on a past revision AND on a gate no decision is owed on: the
        // M0 option rows are a chooser only while the gate is live.
        readOnly={historyCaption !== undefined || !live}
        sdp={sdp}
        slots={slots}
        systemEnvelope={systemEnvelope}
        testing={testing}
        vm={vm}
      />

      {revoke !== undefined ? (
        <RevokeStrip pending={revoke.pending} onRevoke={revoke.onRevoke} />
      ) : null}

      {sdpDecisionError !== undefined ? (
        <Typography
          data-testid={UI_IDENTIFIERS.SdpReview.DECISION_ERROR}
          role="alert"
          sx={{ fontSize: 13, color: t.dangerFg, lineHeight: 1.5 }}
        >
          {sdpDecisionFailed(sdpDecisionError)}
        </Typography>
      ) : null}

      {amend !== undefined ? (
        <AmendComposer error={amend.error} pending={amend.pending} onSubmit={amend.onSubmit} />
      ) : null}

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

      {live || amend !== undefined ? (
        <SubmitBar
          allowAsk={allowAsk}
          allowSendBack={live ? allowSendBack : false}
          approveCopy={approveCopy}
          askPending={askPending}
          committed={!live}
          openThreads={openThreads}
          pending={decisionPending}
          stage={live ? 'awaitingReview' : 'other'}
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
