/**
 * THE PM COVERAGE TABLE — read-only, on the core-use-cases review task (FPC Task 2.14).
 *
 * Every test of a surface is derived from the core use cases' activity diagrams, so the
 * question the reviewer owes the customer is one the rules cannot answer for them: does each
 * path the raw use case promises reach a surface the actor uses, and end in an outcome the
 * actor is shown? One row per derived path: the guards taken, the door (the Client the actor's
 * first stimulus enters through, with its surface), the outcome nodes verbatim, and the
 * derivation's own defect codes. A door or an outcome cell that reads NONE is the finding.
 *
 * The product manager's send-back at this gate holds it for a human under every review preset;
 * approving over it is an override recorded with a reason, which the SubmitBar's notes carry.
 * The panel states that beside the table rather than leaving the reviewer to find it by being
 * refused.
 *
 * Derived on read by the server and stored nowhere. Pure and props-only: the container owns the
 * read (`hooks/useUseCaseCoverage.ts`), as it does for the testing phases' reads.
 */
import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';

import type { AsyncArtifactData } from '../construction/artifactRenderers';
import type { UseCaseCoverageView } from '../../contracts/types';
import {
  COVERAGE_OVERRIDE_NOTE,
  doorLabel,
  listLabel,
  pathIsFinding,
} from './useCaseCoverageLogic.ts';
import { useTokens } from '../../utilities/theme/ThemeContext';
import { UI_IDENTIFIERS } from '../../utilities/constants/UIIdentifiers';

const COVERAGE_COLUMNS = ['Path', 'Guards', 'Door', 'Outcomes the actor sees', 'Problems'] as const;

export function UseCaseCoveragePanel({
  coverage,
}: {
  coverage: AsyncArtifactData<UseCaseCoverageView>;
}): ReactNode {
  const t = useTokens();
  const testId = UI_IDENTIFIERS.Activity.COVERAGE_PANEL;
  if (coverage.isLoading) {
    return (
      <Typography data-testid={testId} sx={{ fontSize: 12.5, color: t.muted }}>
        Deriving the use-case coverage…
      </Typography>
    );
  }
  if (coverage.error !== null) {
    return (
      <Typography data-testid={testId} sx={{ fontSize: 12.5, color: t.dangerFg }}>
        {`Could not derive the use-case coverage: ${coverage.error.message}`}
      </Typography>
    );
  }
  const view = coverage.data;
  if (view === undefined || view.useCases.length === 0) {
    return (
      <Typography data-testid={testId} sx={{ fontSize: 12.5, color: t.muted }}>
        No use case derives a path yet: the core use cases carry no activity diagram, so there is no
        coverage to judge.
      </Typography>
    );
  }
  return (
    <Paper data-testid={testId} sx={{ p: 2, display: 'flex', flexDirection: 'column', gap: 1.5 }}>
      <Typography sx={{ fontSize: 13.5, fontWeight: 600, color: t.ink }}>
        Use-case coverage
      </Typography>
      <Typography sx={{ fontSize: 12.5, color: t.muted, lineHeight: 1.5 }}>
        {COVERAGE_OVERRIDE_NOTE}
      </Typography>
      {view.useCases.map((uc) => (
        <Box key={uc.useCase} sx={{ overflowX: 'auto' }}>
          <Typography sx={{ fontSize: 12.5, fontWeight: 600, color: t.ink, mb: 0.5 }}>
            {uc.useCase}
          </Typography>
          <Box component="table" sx={{ borderCollapse: 'collapse', width: '100%', fontSize: 12 }}>
            <thead>
              <tr>
                {COVERAGE_COLUMNS.map((c) => (
                  <Box
                    component="th"
                    key={c}
                    sx={{ textAlign: 'left', color: t.muted, fontWeight: 500, p: 0.5 }}
                  >
                    {c}
                  </Box>
                ))}
              </tr>
            </thead>
            <tbody>
              {uc.paths.map((p) => (
                <Box
                  component="tr"
                  data-finding={pathIsFinding(p) ? 'true' : 'false'}
                  data-testid={UI_IDENTIFIERS.Activity.COVERAGE_ROW}
                  key={p.scenario}
                  sx={{ verticalAlign: 'top', color: pathIsFinding(p) ? t.dangerFg : t.ink }}
                >
                  <Box component="td" sx={{ p: 0.5 }}>
                    {p.scenario}
                  </Box>
                  <Box component="td" sx={{ p: 0.5 }}>
                    {listLabel(p.guards, '—')}
                  </Box>
                  <Box component="td" sx={{ p: 0.5 }}>
                    {doorLabel(p)}
                  </Box>
                  <Box component="td" sx={{ p: 0.5 }}>
                    {listLabel(p.outcomeNodes, 'NONE')}
                  </Box>
                  <Box component="td" sx={{ p: 0.5 }}>
                    {listLabel(p.problems, '—')}
                  </Box>
                </Box>
              ))}
            </tbody>
          </Box>
        </Box>
      ))}
    </Paper>
  );
}
