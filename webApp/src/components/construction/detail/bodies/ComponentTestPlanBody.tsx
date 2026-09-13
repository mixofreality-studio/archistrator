/**
 * THE COMPONENT TEST PLAN BODY — the Test Plan phase of a component's activity
 * (designer §2.6; architect §1.7: the honest empty state plus the system-test
 * coverage rows, until a structured per-component plan exists).
 *
 * Three stacked sections, and nothing here is titled as this component's plan
 * unless it is one:
 *   1. COMPONENT TEST PLAN — not recorded (§5.4). The awaiting-ink label marks a
 *      real gap; the backfill clause appears only where the stp attempt was
 *      reconstructed.
 *   2. SYSTEM TEST COVERAGE · from N-STP — REFERENCE, never a substitute:
 *      DIRECT scenarios (the managers the plan calls) through the unchanged
 *      ScenarioBrowser, REACHED THROUGH rows for everything else. Neither reads
 *      as "untested".
 *   3. One line to the use-case flows on the contract's Dynamic tab — "designed
 *      call chains, not tests". Not duplicated here.
 */
import { useMemo, type ReactElement } from 'react';
import Box from '@mui/material/Box';
import Link from '@mui/material/Link';
import Typography from '@mui/material/Typography';

import type { ArtifactModelEnvelope, ProjectStateWithGit } from '../../../../contracts/types';
import { dynamicViewUseCaseIds } from '../../../../contracts/adapters';
import { useTokens } from '../../../../utilities/theme/ThemeContext';
import { UI_IDENTIFIERS } from '../../../../utilities/constants/UIIdentifiers';
import { ScenarioBrowser } from '../../renderers/ScenarioBrowser';
import { AbsenceStatement, ArtifactFrame } from './ArtifactFrame';
import {
  GAP_LABEL_NO_TEST_PLAN,
  OBSERVED_ONLY_SOURCE_SUFFIX,
  noTestPlanSentence,
} from './artifactPlacement.ts';
import {
  DIRECT_CAPTION,
  NO_COVERAGE_SENTENCE,
  REACHED_CAPTION,
  reachedThroughLabel,
  systemTestCoverageFor,
  useCaseFlowsSentence,
} from './componentCoverage.ts';

export function ComponentTestPlanBody({
  componentId,
  contractKey,
  project,
  systemEnvelope,
  stpReconstructed,
  observedOnly,
  compact,
  onOpenSystemTestPlan,
  onOpenDynamic,
  onFocus,
}: {
  componentId: string;
  contractKey: string | undefined;
  project: ProjectStateWithGit | undefined;
  systemEnvelope: ArtifactModelEnvelope | undefined;
  stpReconstructed: boolean;
  observedOnly: boolean;
  /** Below 600px: no canvas — the direct scenarios collapse to a count. */
  compact: boolean;
  onOpenSystemTestPlan?: (() => void) | undefined;
  /** Open the contract's Dynamic tab; absent when there is no contract to open. */
  onOpenDynamic?: (() => void) | undefined;
  onFocus?: (() => void) | undefined;
}): ReactElement {
  const t = useTokens();
  const scenarios = useMemo(
    () => project?.testingState?.systemTestPlan?.scenarios ?? [],
    [project]
  );
  const useCaseIds = useMemo(
    () => dynamicViewUseCaseIds(systemEnvelope, componentId),
    [systemEnvelope, componentId]
  );
  const coverage = useMemo(
    () => systemTestCoverageFor(scenarios, [contractKey ?? '', componentId], useCaseIds),
    [scenarios, contractKey, componentId, useCaseIds]
  );
  const none = coverage.direct.length === 0 && coverage.reachedThrough.length === 0;

  return (
    <Box
      data-testid={UI_IDENTIFIERS.Construction.COMPONENT_TEST_PLAN}
      sx={{ display: 'flex', flexDirection: 'column', gap: 2, minWidth: 0 }}
    >
      <AbsenceStatement
        label={GAP_LABEL_NO_TEST_PLAN}
        sentence={noTestPlanSentence(stpReconstructed)}
        testId={UI_IDENTIFIERS.Construction.COMPONENT_TEST_PLAN_EMPTY}
        tone="gap"
      />

      <ArtifactFrame
        artifactRole="reference"
        source={`testingState.systemTestPlan · ${String(scenarios.length)} scenarios${observedOnly ? OBSERVED_ONLY_SOURCE_SUFFIX : ''}`}
        title="SYSTEM TEST COVERAGE · from N-STP"
        onFocus={onFocus}
      >
        <Box
          data-testid={UI_IDENTIFIERS.Construction.TEST_COVERAGE}
          sx={{ display: 'flex', flexDirection: 'column', gap: 1.25, minWidth: 0 }}
        >
          {none ? (
            <Typography sx={{ fontFamily: t.body, fontSize: 12, color: t.muted }}>
              {NO_COVERAGE_SENTENCE}
            </Typography>
          ) : null}

          {coverage.direct.length > 0 ? (
            <Box
              data-testid={UI_IDENTIFIERS.Construction.TEST_COVERAGE_DIRECT}
              sx={{ display: 'flex', flexDirection: 'column', gap: 0.75, minWidth: 0 }}
            >
              <SectionLabel text={`Direct · ${String(coverage.direct.length)}`} />
              <Typography
                sx={{ fontFamily: t.body, fontSize: 11.5, color: t.muted, lineHeight: 1.45 }}
              >
                {DIRECT_CAPTION}
              </Typography>
              {compact ? (
                <Typography sx={{ fontFamily: t.mono, fontSize: 11, color: t.ink }}>
                  {coverage.direct.map((s) => s.id).join(' · ')}
                </Typography>
              ) : (
                <ScenarioBrowser mode="plan" scenarios={coverage.direct} t={t} />
              )}
            </Box>
          ) : null}

          {coverage.reachedThrough.length > 0 ? (
            <Box sx={{ display: 'flex', flexDirection: 'column', gap: 0.5, minWidth: 0 }}>
              <SectionLabel text={`Reached through · ${String(coverage.reachedThrough.length)}`} />
              <Typography
                sx={{ fontFamily: t.body, fontSize: 11.5, color: t.muted, lineHeight: 1.45 }}
              >
                {REACHED_CAPTION}
              </Typography>
              <Box component="ul" sx={{ m: 0, pl: 0, listStyle: 'none' }}>
                {coverage.reachedThrough.map((row) => (
                  <Box component="li" key={row.scenario.id} sx={{ py: 0.25 }}>
                    {onOpenSystemTestPlan !== undefined ? (
                      <Link
                        component="button"
                        data-testid={UI_IDENTIFIERS.Construction.coverageReachedRow(
                          row.scenario.id
                        )}
                        sx={{ fontFamily: t.mono, fontSize: 11, color: t.ink, textAlign: 'left' }}
                        underline="hover"
                        onClick={onOpenSystemTestPlan}
                      >
                        {reachedThroughLabel(row)}
                      </Link>
                    ) : (
                      <Typography
                        data-testid={UI_IDENTIFIERS.Construction.coverageReachedRow(
                          row.scenario.id
                        )}
                        sx={{ fontFamily: t.mono, fontSize: 11, color: t.ink }}
                      >
                        {reachedThroughLabel(row)}
                      </Typography>
                    )}
                  </Box>
                ))}
              </Box>
            </Box>
          ) : null}
        </Box>
      </ArtifactFrame>

      <Typography
        data-testid={UI_IDENTIFIERS.Construction.USE_CASE_FLOWS_LINK}
        sx={{ fontFamily: t.body, fontSize: 12, color: t.muted }}
      >
        {useCaseFlowsSentence(useCaseIds.length)}
        {onOpenDynamic !== undefined && useCaseIds.length > 0 ? (
          <>
            {' '}
            <Link
              component="button"
              sx={{ fontFamily: t.mono, fontSize: 11.5, fontWeight: 700, color: t.accent2 }}
              underline="hover"
              onClick={onOpenDynamic}
            >
              Open on the contract’s Dynamic tab →
            </Link>
          </>
        ) : null}
      </Typography>
    </Box>
  );
}

function SectionLabel({ text }: { text: string }): ReactElement {
  const t = useTokens();
  return (
    <Typography
      sx={{
        fontFamily: t.mono,
        fontWeight: 700,
        fontSize: 9.5,
        letterSpacing: '0.06em',
        color: t.muted,
      }}
    >
      {text.toUpperCase()}
    </Typography>
  );
}
