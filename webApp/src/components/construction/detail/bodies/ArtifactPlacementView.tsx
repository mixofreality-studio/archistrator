/**
 * Renders one PLACEMENT (artifactPlacement.ts) — the committed artifact the
 * designer's table puts at this selection, in its ArtifactFrame, or the honest
 * statement of why there is none.
 *
 * One component for the pane, the review body (above the verdict) and the
 * focus view, so the three can never show different artifacts for the same
 * selection. Every decision is made by the pure module; this is wiring.
 */
import type { ReactElement } from 'react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';

import type {
  ArtifactModelEnvelope,
  ProducedArtifactRow,
  ProjectStateWithGit,
  RecordOriginRow,
} from '../../../../contracts/types';
import type { ContractJoin } from '../../../../contracts/serviceContracts.ts';
import { useTokens } from '../../../../utilities/theme/ThemeContext';
import { UI_IDENTIFIERS } from '../../../../utilities/constants/UIIdentifiers';
import type { ArtifactViewId } from '../../lens/useLensSelection';
import { ComponentRelationshipsView } from '../../ComponentRelationshipsView';
import { ServiceContractView } from '../../ServiceContractView';
import { FrontendSurfaces } from '../../renderers/FrontendArtifactView';
import { AbsenceStatement, ArtifactFrame } from './ArtifactFrame';
import { ComponentTestPlanBody } from './ComponentTestPlanBody';
import { ContractReferenceLine, ContractSummaryCard } from './ContractSummaryCard';
import {
  byDesignSentence,
  codeReviewFrameFor,
  contractSourceLine,
  GAP_LABEL_BY_DESIGN,
  GAP_LABEL_MISSING,
  GAP_LABEL_UNRESOLVED,
  missingContractSentence,
  NO_CODE_VIEW,
  NO_COMMIT_RECORDED,
  reconstructedArtifactNote,
  relationshipsSourceLine,
  SRS_UNREADABLE,
  UNRESOLVED_SENTENCE,
  type ArtifactRole,
  type FocusTarget,
  type NamedOperation,
  type Placement,
} from './artifactPlacement.ts';

export interface PlacementViewContext {
  join: ContractJoin | undefined;
  /** 'service' reads SERVICE CONTRACT; 'frontend' reads CLIENT CONTRACT. */
  activityKind: string | undefined;
  role: ArtifactRole;
  /** The "Observed only" toggle is on: the source line says what the frame is. */
  observedOnly: boolean;
  /** The provenance note above is RECONSTRUCTED, and at what scope; else undefined. */
  reconstructedScope: 'task' | 'wider' | undefined;
  project: ProjectStateWithGit | undefined;
  systemEnvelope: ArtifactModelEnvelope | undefined;
  /** Below 600px: summary cards only, no canvas (§3). */
  compact: boolean;
  view: ArtifactViewId;
  onViewChange: (view: ArtifactViewId) => void;
  onFocusComponent: (componentId: string) => void;
  /** Select this activity's Detailed Design phase. */
  onOpenDesign: () => void;
  /** Open the contract's Dynamic tab in Detailed Design. */
  onOpenDynamic: () => void;
  /** Open the focus view; undefined inside the focus view itself. */
  onFocus: (() => void) | undefined;
  /** Rendered inside the focus view: the Code tab may draw its canvas. */
  inFocus: boolean;
  /** Open the system test plan AT one scenario (the `sc` deep link, B2). */
  onOpenSystemTestPlan: ((scenarioId: string) => void) | undefined;
  /** The system test plan activity's id (N-STP), named on the reached-through rows. */
  systemTestPlanId: string | undefined;
  /** Whether a neighbour's click goes anywhere (an activity builds it). */
  isNavigable: (componentId: string) => boolean;
  /** The activity a neighbour's row opens (`C-review-engine`); undefined when none builds it. */
  destinationOf: (componentId: string) => string | undefined;
  /** The selected attempt's evidence pointer (Code Review's commit). */
  evidence: { kind: string; ref: string } | undefined;
  /** The selected attempt's origin and number; undefined when none is recorded. */
  attemptOrigin: RecordOriginRow | undefined;
  attemptNumber: number | undefined;
  /** The gate at this selection is owed now (the live workflow stands at it). */
  gateOwedNow: boolean;
  /** The row's produced records — the SPA's surfaces. */
  produced: readonly ProducedArtifactRow[];
  /** The selected stp attempt was reconstructed (the §5.4 backfill clause). */
  stpReconstructed: boolean;
  /** Other activities whose contract is also missing (the §5.1 sentence). */
  othersMissing: number;
  /** The architecture's inbound edges to this component (the §5.1 sentence). */
  inboundOperations: readonly NamedOperation[];
}

function contractTitle(activityKind: string | undefined): string {
  return activityKind === 'frontend' ? 'CLIENT CONTRACT' : 'SERVICE CONTRACT';
}

export function ArtifactPlacementView({
  placement,
  ctx,
}: {
  placement: Placement;
  ctx: PlacementViewContext;
}): ReactElement | null {
  const t = useTokens();
  const join = ctx.join;
  if (placement.kind === 'none' || join === undefined) return null;
  const contractJoin = join.kind === 'contract' ? join : undefined;
  const source =
    contractJoin !== undefined
      ? contractSourceLine(
          contractJoin.contractKey,
          contractJoin.contract.revisions?.length ?? 0,
          ctx.observedOnly
        )
      : '';
  // The "committed today" sentence is not rendered here: it rides the provenance
  // note, after the basis (in its disclosure, in the pane), and the focus view's
  // rail — see ReconstructedArtifactNote.

  switch (placement.kind) {
    case 'contractSummary':
      return contractJoin !== undefined ? (
        <Stack>
          <ContractSummaryCard
            artifactRole="committedNow"
            contract={contractJoin.contract}
            source={source}
            title={contractTitle(ctx.activityKind)}
            onFocus={ctx.onFocus}
            onOpenDesign={ctx.onOpenDesign}
          />
        </Stack>
      ) : null;

    case 'contractReference':
      return contractJoin !== undefined ? (
        <ContractReferenceLine
          line={placement.line}
          source={source}
          onFocus={ctx.onFocus}
          onOpen={ctx.onOpenDesign}
        />
      ) : null;

    case 'contractFull':
      if (contractJoin === undefined) return null;
      return (
        <Stack>
          {ctx.compact ? (
            // Below 600px no canvas draws in the drawer: the summary, with Focus
            // as the primary action (§3).
            <ContractSummaryCard
              artifactRole={ctx.role}
              contract={contractJoin.contract}
              source={source}
              title={contractTitle(ctx.activityKind)}
              onFocus={ctx.onFocus}
            />
          ) : (
            <ArtifactFrame
              artifactRole={ctx.role}
              source={source}
              title={contractTitle(ctx.activityKind)}
              onFocus={ctx.onFocus}
            >
              <ContractFull ctx={ctx} />
            </ArtifactFrame>
          )}
          {ctx.activityKind === 'frontend' ? (
            <Typography sx={{ fontFamily: t.body, fontSize: 12, color: t.muted }}>
              The UI design itself has no view in this stage.
            </Typography>
          ) : null}
        </Stack>
      );

    case 'gapOverview':
    case 'contractGap':
      if (join.kind !== 'missing') return null;
      return (
        <Stack>
          <AbsenceStatement
            label={GAP_LABEL_MISSING}
            sentence={missingContractSentence({
              componentId: join.componentId,
              contractKey: join.contractKey,
              othersMissing: ctx.othersMissing,
              operations: ctx.inboundOperations,
            })}
            testId={UI_IDENTIFIERS.Construction.CONTRACT_GAP}
            tone="gap"
          />
          {placement.kind === 'contractGap' ? (
            <RelationshipsFrame componentId={join.componentId} ctx={ctx} title="COMPONENT" />
          ) : null}
        </Stack>
      );

    case 'byDesignOverview':
    case 'byDesignSpec':
      if (join.kind !== 'byDesign') return null;
      return (
        <Stack>
          <AbsenceStatement
            label={GAP_LABEL_BY_DESIGN}
            sentence={byDesignSentence(join.component.kind)}
            testId={UI_IDENTIFIERS.Construction.CONTRACT_BY_DESIGN}
            tone="byDesign"
          />
          <RelationshipsFrame
            caption={WHO_REACHES_CAPTION}
            componentId={join.componentId}
            ctx={ctx}
            testId={UI_IDENTIFIERS.Construction.WHO_REACHES_IT}
            title="WHO REACHES IT"
            variant="reachedBy"
          />
        </Stack>
      );

    case 'codeReview': {
      // B3: a reconstructed attempt reviewed nothing anyone watched — one line,
      // no CODE frame. Owed on an observed attempt: UNDER REVIEW. Observed and not
      // owed: REVIEWED, sourced to its commit and attempt. Never COMMITTED NOW.
      const code = codeReviewFrameFor({
        origin: ctx.attemptOrigin,
        owedNow: ctx.gateOwedNow,
        evidence: ctx.evidence,
        attempt: ctx.attemptNumber,
      });
      return (
        <Stack>
          {code.kind === 'noCodeView' ? (
            <Typography
              data-testid={UI_IDENTIFIERS.Construction.CODE_REVIEW_NO_VIEW}
              sx={{ fontFamily: t.body, fontSize: 12.5, color: t.muted }}
            >
              {NO_CODE_VIEW}
            </Typography>
          ) : (
            <ArtifactFrame artifactRole={code.role} source={code.source} title="CODE">
              <Box data-testid={UI_IDENTIFIERS.Construction.CODE_REVIEW_COMMIT}>
                {code.commit !== undefined ? (
                  <Typography
                    sx={{ fontFamily: t.mono, fontSize: 12, color: t.ink, wordBreak: 'break-all' }}
                  >
                    {code.commit}
                  </Typography>
                ) : (
                  <Typography sx={{ fontFamily: t.body, fontSize: 12, color: t.muted }}>
                    {NO_COMMIT_RECORDED}
                  </Typography>
                )}
                {/* No [Open commit ↗]: the read carries no remote URL to derive one from. */}
                <Typography sx={{ fontFamily: t.body, fontSize: 12, color: t.muted, mt: 0.5 }}>
                  {NO_CODE_VIEW}
                </Typography>
              </Box>
            </ArtifactFrame>
          )}
          {contractJoin !== undefined ? (
            <ContractSummaryCard
              artifactRole="reference"
              contract={contractJoin.contract}
              source={source}
              title={contractTitle(ctx.activityKind)}
              onFocus={ctx.onFocus}
              onOpenDesign={ctx.onOpenDesign}
            />
          ) : null}
        </Stack>
      );
    }

    case 'frontendSurfaces':
      return (
        <FrontendSurfaces
          componentId={'componentId' in join ? join.componentId : undefined}
          produced={ctx.produced}
          t={t}
        />
      );

    case 'srsUnreadable':
      return (
        <ArtifactFrame
          artifactRole={ctx.role}
          source="phaseArtifacts.srs · not carried by the project read"
          title="SOFTWARE REQUIREMENTS SPEC"
        >
          <Typography
            data-testid={UI_IDENTIFIERS.Construction.SRS_UNREADABLE}
            sx={{ fontFamily: t.body, fontSize: 12.5, color: t.muted }}
          >
            {SRS_UNREADABLE}
          </Typography>
        </ArtifactFrame>
      );

    case 'componentTestPlan':
      if (join.kind !== 'contract' && join.kind !== 'missing' && join.kind !== 'byDesign') {
        return null;
      }
      return (
        <ComponentTestPlanBody
          compact={ctx.compact}
          componentId={join.componentId}
          contractKey={contractJoin?.contractKey}
          observedOnly={ctx.observedOnly}
          project={ctx.project}
          stpReconstructed={ctx.stpReconstructed}
          systemEnvelope={ctx.systemEnvelope}
          systemTestPlanId={ctx.systemTestPlanId}
          variant={ctx.activityKind === 'frontend' ? 'frontend' : 'service'}
          onFocus={ctx.onFocus}
          onOpenDynamic={contractJoin !== undefined ? ctx.onOpenDynamic : undefined}
          onOpenSystemTestPlan={ctx.onOpenSystemTestPlan}
        />
      );

    case 'contractUnresolved':
      return (
        <AbsenceStatement
          label={GAP_LABEL_UNRESOLVED}
          sentence={UNRESOLVED_SENTENCE}
          testId={UI_IDENTIFIERS.Construction.CONTRACT_UNRESOLVED}
          tone="gap"
        />
      );
  }
}

/** The full contract view with the pane's tab state and neighbour navigation. */
function ContractFull({ ctx }: { ctx: PlacementViewContext }): ReactElement | null {
  const join = ctx.join;
  if (join?.kind !== 'contract') return null;
  return (
    <ServiceContractView
      componentId={join.componentId}
      contract={join.contract}
      destinationOf={ctx.destinationOf}
      inFocus={ctx.inFocus}
      isNavigable={ctx.isNavigable}
      systemEnvelope={ctx.systemEnvelope}
      view={ctx.view}
      onFocusComponent={ctx.onFocusComponent}
      onOpenFocus={ctx.onFocus}
      onViewChange={ctx.onViewChange}
    />
  );
}

/**
 * The one sentence between a RECONSTRUCTED attempt and the contract (§4.2):
 * the contract is today's, and nothing links it to the attempt. It rides the
 * provenance note after the basis — in the pane inside its "Basis and evidence"
 * disclosure, so the first op clears the action bar at 1366×768 (designer
 * recheck on S2); in the focus view's rail in the open (polish 1). `shown`: the
 * selection shows the committed contract (the sentence says "below").
 */
export function ReconstructedArtifactNote({
  ctx,
  shown,
}: {
  ctx: PlacementViewContext;
  shown: boolean;
}): ReactElement | null {
  const t = useTokens();
  const join = ctx.join;
  if (!shown || ctx.reconstructedScope === undefined || join?.kind !== 'contract') return null;
  return (
    <Typography
      data-testid={UI_IDENTIFIERS.Construction.ARTIFACT_RECONSTRUCTED_NOTE}
      sx={{ fontFamily: t.body, fontSize: 12, color: t.ink, lineHeight: 1.45 }}
    >
      {reconstructedArtifactNote(ctx.reconstructedScope, join.contract.revisions?.length ?? 0)}
    </Typography>
  );
}

/** The WHO REACHES IT frame's caption — the pane's and the focus view's alike. */
const WHO_REACHES_CAPTION =
  'From the committed architecture (system · relationships) — who reaches this resource.';

/**
 * The COMPONENT frame of a missing contract and the WHO REACHES IT frame of a
 * Resource — as the Component tab draws them (designer check on renderers S3):
 * text rows in the pane, the 500px drawer included, each saying the call and the
 * activity it opens, with "Open diagram in focus view"; the diagram in the focus
 * view only.
 */
function RelationshipsFrame({
  componentId,
  ctx,
  title,
  caption,
  testId,
  variant,
}: {
  componentId: string;
  ctx: PlacementViewContext;
  title: string;
  caption?: string;
  testId?: string;
  variant?: 'component' | 'reachedBy';
}): ReactElement {
  return (
    <ArtifactFrame
      artifactRole="committedNow"
      source={relationshipsSourceLine(ctx.observedOnly)}
      title={title}
      onFocus={ctx.onFocus}
    >
      <ComponentRelationshipsView
        componentId={componentId}
        destinationOf={ctx.destinationOf}
        isNavigable={ctx.isNavigable}
        mode={ctx.inFocus ? 'canvas' : 'list'}
        systemEnvelope={ctx.systemEnvelope}
        onFocusComponent={ctx.onFocusComponent}
        onOpenFocus={ctx.inFocus ? undefined : ctx.onFocus}
        {...(caption !== undefined ? { caption } : {})}
        {...(testId !== undefined ? { testId } : {})}
        {...(variant !== undefined ? { variant } : {})}
      />
    </ArtifactFrame>
  );
}

function Stack({ children }: { children: React.ReactNode }): ReactElement {
  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1.5, minWidth: 0 }}>{children}</Box>
  );
}

/**
 * The FOCUS VIEW's artifact: the full contract, or the relationships view for a
 * component with none, or the test plan body — whatever depth opened it.
 */
export function FocusArtifact({
  target,
  artifactRole: role,
  ctx,
}: {
  target: FocusTarget;
  artifactRole: ArtifactRole;
  ctx: PlacementViewContext;
}): ReactElement | null {
  const join = ctx.join;
  if (join === undefined) return null;
  const inFocus: PlacementViewContext = {
    ...ctx,
    compact: false,
    onFocus: undefined,
    inFocus: true,
  };
  if (target === 'testPlan') {
    return <ArtifactPlacementView ctx={inFocus} placement={{ kind: 'componentTestPlan' }} />;
  }
  if (target === 'contract' && join.kind === 'contract') {
    return (
      <ArtifactFrame
        artifactRole={role}
        source={contractSourceLine(
          join.contractKey,
          join.contract.revisions?.length ?? 0,
          ctx.observedOnly
        )}
        title={contractTitle(ctx.activityKind)}
      >
        <ContractFull ctx={inFocus} />
      </ArtifactFrame>
    );
  }
  if (target === 'relationships' && (join.kind === 'missing' || join.kind === 'byDesign')) {
    // The same placement the pane shows — the gap or by-design statement above
    // the diagram, and a Resource's "who reaches this resource" caption — so the
    // focus view never says less than the pane (designer check on renderers S3).
    return (
      <ArtifactPlacementView
        ctx={inFocus}
        placement={{ kind: join.kind === 'byDesign' ? 'byDesignSpec' : 'contractGap' }}
      />
    );
  }
  return null;
}
