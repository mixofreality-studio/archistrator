/**
 * THE ARTIFACT UNDER REVIEW — `taskArtifactFor`'s four branches, rendered.
 *
 * This is the file that replaces `construction/detail/bodies/ArtifactBody.tsx`
 * as the review aids' only reachable caller (Task 13 deletes that one). NO new
 * renderer is written here and none is ever written for this screen: every kind
 * that has a view already has one, and a second implementation of a view is how
 * this branch's worst bug happened.
 *
 *   slot             ArtifactRenderer (the committed Phase-1 slot: mission,
 *                    glossary, volatilities, core use cases, system) — or, for
 *                    `sdpReview`, the Project Design M0 body below.
 *   serviceContract  ServiceContractView over the contract JOIN, which is what
 *                    keeps ContractCodeFlow and ContractSignatureList reachable.
 *   classified       artifactRenderers[classification] — the phase-aware
 *                    service/frontend renderer (ScenarioPlanView →
 *                    ScenarioBrowser on `test_plan`, TestRunPanel on
 *                    `integration`, FrontendArtifactView on `construction`),
 *                    uiDesign's FrontendArtifactView — called with EXACTLY
 *                    `ArtifactRendererProps`, the phase and the testing data
 *                    the container fetched included.
 *   unavailable      an honest panel carrying the dispatcher's reason verbatim.
 *
 * ── A renderer with no row draws a LIE ──────────────────────────────────────
 * `ArtifactRendererProps.vm` requires a `ConstructionRow`. A planned activity
 * that has never been dispatched has none, and a renderer handed no row draws an
 * empty frame that reads as "there is nothing here" — a different, and false,
 * claim from "nothing has been recorded". So the `classified` branch degrades to
 * the unavailable panel instead.
 *
 * ── The Project Design M0 body (spec §6/R7) ─────────────────────────────────
 * It reuses the REAL Phase-2 views over the committed slots — `SdpReviewView`
 * (16), `ActivityListView` (9), `NetworkView` (10 × 9). Nothing new is drawn:
 * the prototype's `PlanArtifact.tsx` was a mock of components that already
 * exist. `SdpReviewView` runs in `decision: 'chooser'` mode, so the option
 * radiogroup is live and its own Commit is NOT rendered: approve converges through
 * the one SubmitBar, so `onCommit` is a no-op that cannot be reached. Reject all
 * IS rendered while the gate is live (spec §12 B7: the use cases decline the plan
 * with a reason — commit-to-a-project-option [reject-all + feedback],
 * replan-under-scope-change [reject]); it is not a send-back, which M0 still has
 * none of, because it returns no artifact for a redraft.
 *
 * Pure and props-only: `useTokens` is `utilities/`, not `hooks/`.
 */
import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';

import {
  CONTRACT_BY_DESIGN,
  CONTRACT_MISSING,
  CONTRACT_UNRESOLVED,
  NO_CONSTRUCTION_RECORD,
} from './activityCopy.ts';
import type { TaskArtifact } from './taskArtifactFor.ts';
import { ArtifactRenderer } from '../ArtifactRenderer';
import {
  artifactRenderers,
  type ArtifactActivityVM,
  type TestingArtifactData,
} from '../construction/artifactRenderers';
import { ServiceContractView } from '../construction/ServiceContractView';
import { ActivityListView } from '../project/ActivityListView';
import { NetworkView } from '../project/NetworkView';
import { SdpReviewView } from '../project/SdpReviewView';
import type { ContractJoin } from '../../contracts/serviceContracts';
import type {
  ArtifactKindFull,
  ArtifactModelEnvelope,
  ArtifactSlotView,
  ProjectArtifactModelEnvelope,
  ProjectStateWithGit,
} from '../../contracts/types';
import { useTokens } from '../../utilities/theme/ThemeContext';
import { UI_IDENTIFIERS } from '../../utilities/constants/UIIdentifiers';

/** `onCommit` is unreachable in `decision: 'chooser'` mode — see the file header. */
function unreachable(): void {
  /* intentionally empty: the SDP view renders no verb of its own here */
}

/**
 * What the M0 body hands up and down. `onChoose` reports the option the bar will
 * approve; `onRejectAll` is present only while a decision is owed on the gate.
 */
export interface SdpDecisionProps {
  onChoose: (optionId: string) => void;
  onRejectAll?: ((feedback: string) => void) | undefined;
  /** A reject-all is in flight. */
  rejectPending?: boolean | undefined;
}

function slotEnvelope(
  slots: readonly ArtifactSlotView[],
  kind: ArtifactKindFull
): ArtifactModelEnvelope | undefined {
  return slots.find((s) => s.kind === kind)?.model;
}

/**
 * The same slot, as the Phase-2 views type it. `ArtifactSlotView.model` is the
 * union of BOTH phases' envelopes; the Phase-2 renderers take the narrower one,
 * and the two are structurally the same object — this is exactly the projection
 * `ProjectDesignExperience.tsx:111-117` already makes for the same reason.
 */
function projectEnvelope(
  slots: readonly ArtifactSlotView[],
  kind: ArtifactKindFull
): ProjectArtifactModelEnvelope | undefined {
  return slotEnvelope(slots, kind) as unknown as ProjectArtifactModelEnvelope | undefined;
}

function UnavailablePanel({ reason }: { reason: string }): ReactNode {
  const t = useTokens();
  return (
    <Paper data-testid={UI_IDENTIFIERS.Activity.ARTIFACT_UNAVAILABLE} sx={{ p: 3 }}>
      <Typography sx={{ fontSize: 13.5, color: t.ink, lineHeight: 1.5 }}>{reason}</Typography>
    </Paper>
  );
}

/** The Project Design M0 body: the SDP review, the plan it derives from, its network. */
function ProjectDesignBody({
  slots,
  sdp,
  readOnly,
}: {
  slots: readonly ArtifactSlotView[];
  sdp: SdpDecisionProps | undefined;
  /** A read-only history: the option radiogroup reports and changes nothing. */
  readOnly: boolean;
}): ReactNode {
  const activityEnvelope = projectEnvelope(slots, 'activityList');
  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2.5 }}>
      {/* `decision: 'chooser'` is kept in BOTH modes on purpose: it is what
          suppresses the view's own Commit, so a read-only history renders no
          decision control at all rather than a disabled one. `readOnly` makes the
          option rows inert, and Reject all renders only where the host hands
          `onRejectAll` — a LIVE gate, never a history. */}
      <SdpReviewView
        decision="chooser"
        envelope={projectEnvelope(slots, 'sdpReview')}
        pending={sdp?.rejectPending ?? false}
        readOnly={readOnly}
        onChoose={sdp?.onChoose}
        onCommit={unreachable}
        onRejectAll={readOnly ? undefined : sdp?.onRejectAll}
      />
      <ActivityListView envelope={activityEnvelope} />
      <NetworkView
        activityEnvelope={activityEnvelope}
        networkEnvelope={projectEnvelope(slots, 'network')}
      />
    </Box>
  );
}

/**
 * The JOIN's three honest absences, said apart. `contract` never reaches here
 * (the caller renders the view for it) and `noComponent` / `unresolved` /
 * undefined are the same fact to a reader: the committed data does not place
 * this activity on a component.
 */
function contractAbsence(join: ContractJoin | undefined): string {
  switch (join?.kind) {
    case 'byDesign':
      return CONTRACT_BY_DESIGN;
    case 'missing':
      return CONTRACT_MISSING;
    case 'contract':
    case 'noComponent':
    case 'unresolved':
    case undefined:
      return CONTRACT_UNRESOLVED;
  }
}

export function ArtifactPanel({
  artifact,
  slots,
  vm,
  project,
  systemEnvelope,
  contractJoin,
  testing,
  sdp,
  readOnly = false,
}: {
  artifact: TaskArtifact;
  /** For the `slot` branch: the committed slot envelopes from useProject. */
  slots: readonly ArtifactSlotView[];
  /** For the `classified` branch — exactly artifactRenderers.tsx's ArtifactRendererProps. */
  vm: ArtifactActivityVM | undefined;
  project: ProjectStateWithGit | undefined;
  systemEnvelope: ArtifactModelEnvelope | undefined;
  /** For the `serviceContract` branch: the join the container already computes. */
  contractJoin: ContractJoin | undefined;
  /** For the `classified` branch on a testing phase: the scenario plan / test run reads. */
  testing?: TestingArtifactData | undefined;
  /** Project Design M0 only: the option the bar will approve, and the gate's reject-all. */
  sdp?: SdpDecisionProps | undefined;
  /**
   * The reader is on a NON-LATEST revision (R1). Only the M0 body has an
   * interactive control of its own; every other renderer here is already a
   * read-only view, so this reaches exactly that one.
   */
  readOnly?: boolean;
}): ReactNode {
  const t = useTokens();

  const body = ((): ReactNode => {
    switch (artifact.kind) {
      case 'slot':
        // `SLOT_KIND` yields exactly one Phase-2 kind, `sdpReview`, and it is
        // the M0 body; every other slot it names is a Phase-1 artifact, which
        // is what `ArtifactRenderer` dispatches.
        return artifact.artifactKind === 'sdpReview' ? (
          <ProjectDesignBody readOnly={readOnly} sdp={sdp} slots={slots} />
        ) : (
          <ArtifactRenderer
            envelope={slotEnvelope(slots, artifact.artifactKind)}
            serviceContracts={project?.serviceContracts}
            systemEnvelope={systemEnvelope}
            useCasesEnvelope={slotEnvelope(slots, 'coreUseCases')}
          />
        );

      case 'serviceContract':
        return contractJoin?.kind === 'contract' ? (
          <ServiceContractView
            componentId={contractJoin.componentId}
            contract={contractJoin.contract}
            systemEnvelope={systemEnvelope}
          />
        ) : (
          <UnavailablePanel reason={contractAbsence(contractJoin)} />
        );

      case 'classified': {
        if (vm === undefined) return <UnavailablePanel reason={NO_CONSTRUCTION_RECORD} />;
        const Renderer = artifactRenderers[artifact.classification];
        if (Renderer === undefined) return <UnavailablePanel reason={NO_CONSTRUCTION_RECORD} />;
        return (
          <Renderer
            phase={artifact.phase}
            project={project}
            systemEnvelope={systemEnvelope}
            t={t}
            testing={testing}
            useCasesEnvelope={slotEnvelope(slots, 'coreUseCases')}
            vm={vm}
          />
        );
      }

      case 'unavailable':
        return <UnavailablePanel reason={artifact.reason} />;
    }
  })();

  return (
    <Box data-testid={UI_IDENTIFIERS.Activity.ARTIFACT_PANEL} sx={{ minWidth: 0 }}>
      {body}
    </Box>
  );
}
