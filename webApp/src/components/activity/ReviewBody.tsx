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
import { AMEND_ARCHITECTURE, CONSTRUCTION_THREAD_READ_ONLY } from './activityCopy.ts';
import type { TaskFacts } from './activityViewToGraph.ts';
import type { LifecycleRevision } from './lifecycleGraphTypes.ts';
import type { TaskArtifact } from './taskArtifactFor.ts';
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

/** The Architecture activity's id — the one the M0 gate's amendment link opens. */
const ARCHITECTURE_ACTIVITY_ID = 'architecture';

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
  live,
  openThreads,
  stagedChangeRequests,
  stagedQuestions,
  decisionPending,
  askPending,
  allowSendBack,
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
  reviewSet?: components['schemas']['ConstructionReviewSet'] | undefined;
  reviewSetError?: string | undefined;
  roster?: readonly components['schemas']['ConstructionReviewRosterSeat'][] | undefined;
  verdicts?: readonly components['schemas']['ConstructionReviewVerdictView'][] | undefined;
  artifact: TaskArtifact;
  slots: readonly ArtifactSlotView[];
  vm: ArtifactActivityVM | undefined;
  project: ProjectStateWithGit | undefined;
  systemEnvelope: ArtifactModelEnvelope | undefined;
  contractJoin: ContractJoin | undefined;
  /** Project Design M0 only: how the chosen option reaches the bar. */
  sdp?: { onChoose: (optionId: string) => void } | undefined;
  /** A decision is owed on THIS revision — the bar renders only then. */
  live: boolean;
  /** `openThreadCount(thread)` — nothing else computes this number. */
  openThreads: number;
  stagedChangeRequests: number;
  stagedQuestions: number;
  decisionPending: boolean;
  askPending: boolean;
  allowSendBack: boolean;
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

      <ReviewersStrip
        error={reviewSetError}
        reviewSet={reviewSet}
        roster={roster}
        verdicts={verdicts}
      />

      {threadReadOnly && openThreads > 0 ? (
        <Typography sx={{ fontFamily: t.mono, fontSize: 12, color: t.muted }}>
          {CONSTRUCTION_THREAD_READ_ONLY}
        </Typography>
      ) : null}

      <ArtifactPanel
        artifact={artifact}
        contractJoin={contractJoin}
        project={project}
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

      {live ? (
        <SubmitBar
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
