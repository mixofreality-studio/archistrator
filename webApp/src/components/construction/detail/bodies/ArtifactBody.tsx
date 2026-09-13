/**
 * THE ARTIFACT BODY — the thing the selected phase actually produced.
 *
 * Dispatch is the EXISTING `classify(row)`, reached through
 * bodyDispatch.artifactRendererKeyFor, and the renderers are the EXISTING ones.
 * No new renderer is written here: every kind the founder named already has one,
 * and a second implementation of a view is how this branch's worst bug happened.
 *
 *   service            placed by artifactPlacement.ts, not this dispatch:
 *                      ServiceContractView → ContractCodeFlow (the code-level
 *                      diagram) + the architecture's relationships + revisions
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
 *
 * The SERVICE CONTRACT is no longer dispatched here: artifactPlacement.ts places
 * it (and its honest absences) per phase and task from the contract JOIN, and
 * the pane hands the result in as `primary` — see ArtifactStateFrame for how a
 * Not started row shows a committed artifact without pretending it ran.
 */
import type { ReactElement, ReactNode } from 'react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';

import type {
  ArtifactModelEnvelope,
  ConstructionRow,
  ProjectStateWithGit,
} from '../../../../contracts/types';
import { useTokens } from '../../../../utilities/theme/ThemeContext';
import { UI_IDENTIFIERS } from '../../../../utilities/constants/UIIdentifiers';
import type { LensSelection } from '../../lens/useLensSelection';
import { artifactRenderers } from '../../artifactRenderers';
import {
  artifactRendererKeyFor,
  testingArtifactRendererKeyFor,
  type ArtifactBodyKind,
} from './bodyDispatch.ts';
import { UnknownBody } from './UnknownBody';
import type { TaskDetailState } from '../detailPaneState.ts';
import { briefingFor, unknownStatementFor } from './taskBriefing.ts';

/** What each renderer is showing, named so the reader is never left guessing. */
const ARTIFACT_LABEL: Record<ArtifactBodyKind, string> = {
  // Never reached through this dispatch any more (ARTIFACT_PHASES.service is
  // empty); the contract is placed by artifactPlacement.ts.
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

export function ArtifactBody({
  primary,
  ...props
}: ArtifactBodyProps & {
  /** The placed committed artifact (artifactPlacement.ts); replaces the renderer dispatch. */
  primary?: ReactNode;
}): ReactElement {
  return (
    <Box
      data-testid={UI_IDENTIFIERS.Construction.DETAIL_BODY_ARTIFACT}
      sx={{ display: 'flex', flexDirection: 'column', gap: 1, minWidth: 0 }}
    >
      {primary ?? <ArtifactRender {...props} />}
    </Box>
  );
}

/**
 * A committed artifact shown on a selection with NO RECORD (designer §2.1): the
 * artifact outranks "no record", but the state is not hidden. One state line —
 * the unknown body's own sentence — opens the body, and the rest of that body's
 * briefing (what it is, exit, weight, retry rule) moves into a collapsed "About
 * this task" disclosure under the artifact. The header already carries exit and
 * weight, so nothing is lost. Any other state renders the artifact alone.
 */
export function ArtifactStateFrame({
  row,
  selection,
  state,
  hiddenCount,
  children,
}: {
  row: ConstructionRow | undefined;
  selection: LensSelection;
  state: TaskDetailState;
  hiddenCount: number;
  children: ReactNode;
}): ReactElement {
  const t = useTokens();
  const noRecord = state === 'unknown' || state === 'notStarted';
  if (!noRecord) return <>{children}</>;
  const briefing = briefingFor(row, selection);
  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1.5, minWidth: 0 }}>
      <Typography
        data-testid={UI_IDENTIFIERS.Construction.ARTIFACT_STATE_LINE}
        sx={{ fontFamily: t.body, fontSize: 12.5, color: t.muted, lineHeight: 1.5 }}
      >
        {unknownStatementFor(briefing?.scope, hiddenCount, state)}
      </Typography>
      {children}
      {briefing !== undefined ? (
        <Box
          component="details"
          data-testid={UI_IDENTIFIERS.Construction.ARTIFACT_ABOUT_TASK}
          sx={{ borderTop: `1px solid ${t.line}`, pt: 1 }}
        >
          <Box
            component="summary"
            sx={{
              cursor: 'pointer',
              fontFamily: t.mono,
              fontSize: 10.5,
              fontWeight: 700,
              letterSpacing: '0.06em',
              color: t.muted,
            }}
          >
            {briefing.scope === 'task' ? 'About this task' : 'About this phase'}
          </Box>
          <Box sx={{ display: 'flex', flexDirection: 'column', gap: 0.6, mt: 1 }}>
            {(
              [
                ['What it is', briefing.whatItIs],
                ['Exit', briefing.exit],
                ['Weight', briefing.weight],
                ['Retry rule', briefing.retryRule],
              ] as const
            ).map(([label, value]) => (
              <Box key={label} sx={{ display: 'flex', gap: 1 }}>
                <Typography
                  sx={{
                    flexShrink: 0,
                    width: 86,
                    fontFamily: t.mono,
                    fontSize: 9.5,
                    fontWeight: 700,
                    letterSpacing: '0.08em',
                    color: t.muted,
                    textTransform: 'uppercase',
                    lineHeight: 1.6,
                  }}
                >
                  {label}
                </Typography>
                <Typography
                  sx={{ fontFamily: t.body, fontSize: 12, color: t.ink, lineHeight: 1.5 }}
                >
                  {value}
                </Typography>
              </Box>
            ))}
          </Box>
        </Box>
      ) : null}
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
