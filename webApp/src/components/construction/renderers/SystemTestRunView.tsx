import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Chip from '@mui/material/Chip';
import Typography from '@mui/material/Typography';
import { UI_IDENTIFIERS } from '../../../utilities/constants/UIIdentifiers';
import type { ArtifactRendererProps } from '../artifactRenderers';
import { ScenarioBrowser } from './ScenarioBrowser';
import { StatTile } from '../primitives/StatTile';
import {
  scenarioChipFor,
  scenarioRunStatus,
  systemTestRunSummaryFor,
} from './systemTestRunSummary.ts';

/**
 * System Testing (N-IT): runs the N-STP plan against the REAL built software and
 * drives every scenario from red → green. A selector picks a scenario; only that
 * one renders (sequence diagram, steps coloured by last-run status) plus a
 * green/total summary. A scenario is green only when every step passed.
 *
 * NEVER RUN IS NOT FAILING (fix round B, designer P1-12): until anything has been
 * attempted — no attempt on this row, no step status anywhere — the view says "not
 * run · N scenarios planned", its chips are muted, there is no green/total tile,
 * and the diagram reads in `notRun` mode: plan semantics, but NEUTRAL ink — the
 * negative/boundary case chips and the "target" tag are not red (designer final
 * items; scenarioInk.ts). Red is reserved for a step that recorded red
 * (systemTestRunSummary.ts).
 */
export function SystemTestRunView({ vm, project, t }: ArtifactRendererProps): ReactNode {
  const scenarios = project?.testingState?.systemTestPlan?.scenarios ?? [];

  if (scenarios.length === 0) {
    return (
      <Typography
        data-testid={UI_IDENTIFIERS.Construction.SYSTEM_TEST_VIEW}
        sx={{ color: t.muted, fontSize: 12.5 }}
      >
        No system-test plan to run yet. Once N-STP authors the black-box scenarios, this activity
        runs them against the integrated system and turns each from red to green.
      </Typography>
    );
  }

  const summary = systemTestRunSummaryFor(scenarios, vm.row);

  return (
    <Box
      data-testid={UI_IDENTIFIERS.Construction.SYSTEM_TEST_VIEW}
      sx={{ display: 'flex', flexDirection: 'column', gap: 1.5, minWidth: 0 }}
    >
      <Typography
        sx={{
          fontFamily: t.mono,
          fontWeight: 700,
          fontSize: 11,
          letterSpacing: '0.06em',
          color: summary.attempted ? t.ink : t.muted,
        }}
      >
        {`SYSTEM TESTING · ${summary.headline}`}
      </Typography>
      {summary.tile !== undefined ? (
        <Box sx={{ display: 'flex', gap: 1, flexWrap: 'wrap' }}>
          <StatTile
            label={summary.tile.label}
            t={t}
            tone={summary.tile.tone}
            value={summary.tile.value}
          />
        </Box>
      ) : null}
      <ScenarioBrowser
        mode={summary.attempted ? 'run' : 'notRun'}
        scenarios={scenarios}
        statusChip={(s) => {
          const chip = scenarioChipFor(scenarioRunStatus(s));
          const fg =
            chip.tone === 'good' ? t.committedFg : chip.tone === 'bad' ? t.dangerFg : t.muted;
          const border =
            chip.tone === 'good' ? t.committedDot : chip.tone === 'bad' ? t.dangerFg : t.line;
          return (
            <Chip
              data-scenario-status={chip.label}
              label={chip.label}
              size="small"
              sx={{
                height: 18,
                fontSize: 8.5,
                bgcolor: chip.tone === 'good' ? t.committedBg : t.paperAlt,
                color: fg,
                border: `1px solid ${border}`,
              }}
            />
          );
        }}
        t={t}
      />
    </Box>
  );
}
