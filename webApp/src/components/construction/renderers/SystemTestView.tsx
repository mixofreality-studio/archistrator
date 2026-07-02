import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import Chip from '@mui/material/Chip';
import { UI_IDENTIFIERS } from '../../../constants/UIIdentifiers';
import type { ArtifactRendererProps } from '../artifactRenderers';
import { StatTile } from '../primitives/StatTile';
import { RecordTable } from '../primitives/RecordTable';

/**
 * The System Test (N-IT) artifact renderer: a run summary (pass/fail + open
 * defects) plus the run and defect tables, read from project.testingState.
 * The first proof of the classification→renderer seam.
 */
export function SystemTestView({ project, t }: ArtifactRendererProps): ReactNode {
  const ts = project?.testingState;
  const runs = ts?.testRuns ?? [];
  const defects = ts?.defects ?? [];
  const totalPassed = runs.reduce((n, r) => n + r.passed, 0);
  const totalFailed = runs.reduce((n, r) => n + r.failed, 0);
  const openDefects = defects.length;

  if (ts === undefined || (runs.length === 0 && defects.length === 0)) {
    return (
      <Typography
        data-testid={UI_IDENTIFIERS.Construction.SYSTEM_TEST_VIEW}
        sx={{ color: t.muted, fontSize: 12.5 }}
      >
        No system-test runs recorded yet. Runs and defects appear here once the System Test activity (N-IT)
        executes against the integrated system.
      </Typography>
    );
  }

  const sevBad = (s: string): boolean => s === 'critical' || s === 'high';

  return (
    <Box
      data-testid={UI_IDENTIFIERS.Construction.SYSTEM_TEST_VIEW}
      sx={{ display: 'flex', flexDirection: 'column', gap: 1.5, minWidth: 0 }}
    >
      <Typography sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 11, letterSpacing: '0.06em', color: t.ink }}>
        {`SYSTEM TEST · ${String(runs.length)} run${runs.length === 1 ? '' : 's'}`}
      </Typography>
      <Box sx={{ display: 'flex', gap: 1, flexWrap: 'wrap' }}>
        <StatTile label="passed" value={totalPassed} tone="good" t={t} />
        <StatTile label="failed" value={totalFailed} tone={totalFailed > 0 ? 'bad' : 'good'} t={t} />
        <StatTile label="open defects" value={openDefects} tone={openDefects > 0 ? 'bad' : 'good'} t={t} />
      </Box>

      {runs.length > 0 ? (
        <RecordTable
          columns={[
            { key: 'id', label: 'Run' },
            { key: 'passed', label: 'Passed' },
            { key: 'failed', label: 'Failed' },
            { key: 'note', label: 'Note' },
          ]}
          rows={runs.map((r) => ({ id: r.id, passed: r.passed, failed: r.failed, note: r.note }))}
          t={t}
        />
      ) : null}

      {defects.length > 0 ? (
        <>
          <Typography sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 11, letterSpacing: '0.06em', color: t.ink }}>
            DEFECTS
          </Typography>
          <RecordTable
            columns={[
              { key: 'id', label: 'ID' },
              { key: 'title', label: 'Title' },
              { key: 'severity', label: 'Severity' },
              { key: 'note', label: 'Note' },
            ]}
            rows={defects.map((dfct) => ({
              id: dfct.id,
              title: dfct.title,
              severity: (
                <Chip
                  label={dfct.severity}
                  size="small"
                  sx={{
                    height: 18,
                    fontSize: 8.5,
                    bgcolor: sevBad(dfct.severity) ? t.awaitingBg : t.paperAlt,
                    color: sevBad(dfct.severity) ? t.awaitingFg : t.muted,
                  }}
                />
              ),
              note: dfct.note,
            }))}
            t={t}
          />
        </>
      ) : null}
    </Box>
  );
}
