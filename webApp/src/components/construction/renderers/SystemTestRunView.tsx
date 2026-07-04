import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Chip from '@mui/material/Chip';
import Typography from '@mui/material/Typography';
import { UI_IDENTIFIERS } from '../../../utilities/constants/UIIdentifiers';
import type { TestScenarioView } from '../../../contracts/types';
import type { ArtifactRendererProps } from '../artifactRenderers';
import { ScenarioBrowser } from './ScenarioBrowser';
import { StatTile } from '../primitives/StatTile';

const scenarioGreen = (s: TestScenarioView): boolean =>
  (s.cases ?? []).length > 0 &&
  (s.cases ?? []).every(
    (c) => (c.steps ?? []).length > 0 && (c.steps ?? []).every((st) => st.status === 'green'),
  );

/**
 * System Testing (N-IT): runs the N-STP plan against the REAL built software and
 * drives every scenario from red → green. A selector picks a scenario; only that
 * one renders (sequence diagram in run mode, steps coloured by last-run status)
 * plus a green/total summary. A scenario is green only when every step passed.
 */
export function SystemTestRunView({ project, t }: ArtifactRendererProps): ReactNode {
  const scenarios = project?.testingState?.systemTestPlan?.scenarios ?? [];

  if (scenarios.length === 0) {
    return (
      <Typography
        data-testid={UI_IDENTIFIERS.Construction.SYSTEM_TEST_VIEW}
        sx={{ color: t.muted, fontSize: 12.5 }}
      >
        No system-test plan to run yet. Once N-STP authors the black-box scenarios, this activity runs them
        against the integrated system and turns each from red to green.
      </Typography>
    );
  }

  const greenCount = scenarios.filter(scenarioGreen).length;

  return (
    <Box
      data-testid={UI_IDENTIFIERS.Construction.SYSTEM_TEST_VIEW}
      sx={{ display: 'flex', flexDirection: 'column', gap: 1.5, minWidth: 0 }}
    >
      <Typography sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 11, letterSpacing: '0.06em', color: t.ink }}>
        SYSTEM TESTING · first run against the real build
      </Typography>
      <Box sx={{ display: 'flex', gap: 1, flexWrap: 'wrap' }}>
        <StatTile
          label="scenarios green"
          t={t}
          tone={greenCount === scenarios.length ? 'good' : 'bad'}
          value={`${String(greenCount)}/${String(scenarios.length)}`}
        />
      </Box>
      <ScenarioBrowser
        mode="run"
        scenarios={scenarios}
        statusChip={(s) => (
          <Chip
            label={scenarioGreen(s) ? 'green' : 'failing'}
            size="small"
            sx={{
              height: 18, fontSize: 8.5,
              bgcolor: scenarioGreen(s) ? t.committedBg : t.paperAlt,
              color: scenarioGreen(s) ? t.committedFg : t.dangerFg,
              border: `1px solid ${scenarioGreen(s) ? t.committedDot : t.dangerFg}`,
            }}
          />
        )}
        t={t}
      />
    </Box>
  );
}
