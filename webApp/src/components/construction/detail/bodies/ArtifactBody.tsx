/**
 * THE ARTIFACT BODY — the thing the selected phase actually produced.
 *
 * Dispatch is the EXISTING `classify(row)`, reached through
 * bodyDispatch.artifactRendererKeyFor, and the renderers are the EXISTING ones.
 * No new renderer is written here: every kind the founder named already has one,
 * and a second implementation of a view is how this branch's worst bug happened.
 *
 *   service            ServiceContractView → ContractCodeFlow (the code-level
 *                      diagram) + ContractComponentFlow + ContractRevisionHistory
 *   uiDesign           FrontendArtifactView (the UI spec)
 *   frontend           FrontendArtifactView
 *   testing:plan       TestPlanView → ScenarioBrowser
 *   testing:systemTest SystemTestRunView → ScenarioBrowser (run mode)
 *
 * deployment / documentation / integration are CUT for this stage, and the three
 * testing variants with no authored renderer (harness / perf / qaProcess) are in
 * the same position. All of them fall back to the unknown body with a sentence
 * saying WHY — "this stage renders no view for it" is a different fact from "no
 * record exists", and an empty renderer frame would say neither.
 *
 * `ArtifactRender` is exported because the REVIEW body puts the same artifact
 * above its verdict: one renderer, reached two ways, so the review can never
 * show a different artifact from the one the artifact body shows.
 */
import type { ReactElement } from 'react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';

import type {
  ArtifactModelEnvelope,
  ConstructionRow,
  ProjectStateWithGit,
} from '../../../../contracts/types';
import { useTokens } from '../../../../utilities/theme/ThemeContext';
import { UI_IDENTIFIERS } from '../../../../utilities/constants/UIIdentifiers';
import { contractForActivity } from '../../../../contracts/serviceContracts';
import type { LensSelection } from '../../lens/useLensSelection';
import { artifactRenderers } from '../../artifactRenderers';
import { ServiceContractView } from '../../ServiceContractView';
import {
  artifactRendererKeyFor,
  testingArtifactRendererKeyFor,
  type ArtifactBodyKind,
} from './bodyDispatch.ts';
import { UnknownBody } from './UnknownBody';

/** What each renderer is showing, named so the reader is never left guessing. */
const ARTIFACT_LABEL: Record<ArtifactBodyKind, string> = {
  service: 'SERVICE CONTRACT',
  uiDesign: 'UI DESIGN CONCEPT',
  frontend: 'FRONTEND ARTIFACT',
  'testing:plan': 'SYSTEM TEST PLAN',
  'testing:systemTest': 'SYSTEM TEST RUN',
};

export interface ArtifactBodyProps {
  row: ConstructionRow | undefined;
  selection: LensSelection;
  activityTitle?: string | undefined;
  project?: ProjectStateWithGit | undefined;
  /** The committed Phase-1 `system` slot — ServiceContractView's Dynamic tab. */
  systemEnvelope?: ArtifactModelEnvelope | undefined;
}

export function ArtifactBody(props: ArtifactBodyProps): ReactElement {
  return (
    <Box
      data-testid={UI_IDENTIFIERS.Construction.DETAIL_BODY_ARTIFACT}
      sx={{ display: 'flex', flexDirection: 'column', gap: 1, minWidth: 0 }}
    >
      <ArtifactRender {...props} />
    </Box>
  );
}

/**
 * The artifact itself, headed by what it is. Shared with ReviewBody.
 *
 * Every fall-back path lands on the unknown body with its OWN sentence, because
 * the three reasons an artifact is not shown here are genuinely different:
 * this stage ships no renderer for the kind; the kind has a renderer but this
 * activity recorded no contract; or the activity was never classified at all.
 */
export function ArtifactRender({
  row,
  selection,
  activityTitle,
  project,
  systemEnvelope,
}: ArtifactBodyProps): ReactElement {
  const t = useTokens();
  // `testingArtifactRendererKeyFor` is checked FIRST: it is the only one of the
  // two that resolves a bare activity-row selection (no phase, no task) for a
  // testing-kind row's own committed artifact — see bodyDispatch.ts's module
  // comment. Every other selection it declines to widen falls through to the
  // ordinary `artifactRendererKeyFor`, unchanged.
  const key =
    testingArtifactRendererKeyFor(row, selection, project) ??
    artifactRendererKeyFor(row, selection);

  if (key === undefined || row === undefined) {
    return (
      <UnknownBody
        row={row}
        selection={selection}
        statement="This stage renders no artifact view for this activity kind — deployment, documentation and integration are cut from it, as are the testing variants with no authored view. That is a gap in this surface, not a statement about whether an artifact exists."
      />
    );
  }

  const activityId = row.activityId;
  const vm = { activityId, name: activityTitle ?? activityId, row };

  if (key === 'service') {
    const contract = contractForActivity(project, activityId);
    if (contract === undefined) {
      return (
        <UnknownBody
          row={row}
          selection={selection}
          statement="No service contract is recorded against this activity, so there is nothing to render — the contract view is not shown empty, which would read as a contract with no operations."
        />
      );
    }
    return (
      <>
        <ArtifactHeading label={ARTIFACT_LABEL.service} />
        <ServiceContractView contract={contract} systemEnvelope={systemEnvelope} />
      </>
    );
  }

  const Renderer = artifactRenderers[key];
  if (Renderer === undefined) {
    return (
      <UnknownBody
        row={row}
        selection={selection}
        statement="No renderer is registered for this artifact kind in this stage. Nothing is drawn rather than an empty frame that would imply the artifact is missing."
      />
    );
  }

  return (
    <>
      <ArtifactHeading label={ARTIFACT_LABEL[key]} />
      <Renderer project={project} systemEnvelope={systemEnvelope} t={t} vm={vm} />
    </>
  );
}

function ArtifactHeading({ label }: { label: string }): ReactElement {
  const t = useTokens();
  return (
    <Typography
      sx={{
        fontFamily: t.mono,
        fontWeight: 700,
        fontSize: 10,
        letterSpacing: '0.08em',
        color: t.muted,
      }}
    >
      {label}
    </Typography>
  );
}
