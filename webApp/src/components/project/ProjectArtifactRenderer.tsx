/**
 * The single dispatcher that renders any typed Phase-2 ProjectArtifactModelEnvelope
 * by its string `kind`, choosing the right client-side renderer — the Phase-2 TWIN
 * of components/ArtifactRenderer.tsx:
 *
 *   planningAssumptions  → PlanningAssumptionsView (resources/usage/terms + risk flags)
 *   activityList         → ActivityListView        (grouped activities, 5-day quanta)
 *   network              → NetworkView             (react-flow CPM graph, critical path)
 *   *Solution            → SolutionView            (defining knobs + class rates)
 *   riskModel            → RiskModelView           (per-option risk decomposition)
 *   sdpReview            → SdpReviewView            (options + curves + decision gate)
 *
 * Resilient to an absent envelope/model — each underlying view returns a safe empty
 * state. The wrapping element carries the `artifact-render` testid (shared with
 * Phase 1) so tests can assert a renderer mounted, plus the data-artifact-kind.
 */
import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import type { ProjectArtifactKind, ProjectArtifactModelEnvelope } from '../../api/types';
import { isSolutionKind } from '../../api/projectAdapters';
import { UI_IDENTIFIERS } from '../../constants/UIIdentifiers';
import { PlanningAssumptionsView } from './PlanningAssumptionsView';
import { ActivityListView } from './ActivityListView';
import { NetworkView } from './NetworkView';
import { SolutionView } from './SolutionView';
import { RiskModelView } from './RiskModelView';
import { SdpReviewView } from './SdpReviewView';

/** A typed no-op for the optional SDP decision callbacks. */
function noop(): void {
  /* intentionally empty */
}

export function ProjectArtifactRenderer({
  envelope,
  kind,
  activityEnvelope,
  networkHeight,
  sdpPending,
  onSdpCommit,
  onSdpRejectAll,
}: {
  envelope: ProjectArtifactModelEnvelope | undefined;
  /** The active artifact kind (the slot being rendered). */
  kind: ProjectArtifactKind;
  /** The committed activity-list envelope, joined into the network CPM derivation. */
  activityEnvelope?: ProjectArtifactModelEnvelope | undefined;
  /** Optional network canvas height override. */
  networkHeight?: number;
  /** SDP decision mutation in flight. */
  sdpPending?: boolean;
  onSdpCommit?: (optionId: string) => void;
  onSdpRejectAll?: (feedback: string) => void;
}): ReactNode {
  return (
    <Box data-artifact-kind={envelope?.kind ?? kind} data-testid={UI_IDENTIFIERS.DesignExperience.ARTIFACT_RENDER}>
      {renderBody({ envelope, kind, activityEnvelope, networkHeight, sdpPending, onSdpCommit, onSdpRejectAll })}
    </Box>
  );
}

function renderBody({
  envelope,
  kind,
  activityEnvelope,
  networkHeight,
  sdpPending,
  onSdpCommit,
  onSdpRejectAll,
}: {
  envelope: ProjectArtifactModelEnvelope | undefined;
  kind: ProjectArtifactKind;
  activityEnvelope: ProjectArtifactModelEnvelope | undefined;
  networkHeight: number | undefined;
  sdpPending: boolean | undefined;
  onSdpCommit: ((optionId: string) => void) | undefined;
  onSdpRejectAll: ((feedback: string) => void) | undefined;
}): ReactNode {
  if (kind === 'planningAssumptions') return <PlanningAssumptionsView envelope={envelope} />;
  if (kind === 'activityList') return <ActivityListView envelope={envelope} />;
  if (kind === 'network') {
    return (
      <NetworkView
        activityEnvelope={activityEnvelope}
        networkEnvelope={envelope}
        {...(networkHeight !== undefined ? { height: networkHeight } : {})}
      />
    );
  }
  if (isSolutionKind(kind)) return <SolutionView envelope={envelope} kind={kind} />;
  if (kind === 'riskModel') return <RiskModelView envelope={envelope} />;
  if (kind === 'sdpReview') {
    return (
      <SdpReviewView
        envelope={envelope}
        pending={sdpPending ?? false}
        onCommit={onSdpCommit ?? noop}
        onRejectAll={onSdpRejectAll ?? noop}
      />
    );
  }
  return null;
}
