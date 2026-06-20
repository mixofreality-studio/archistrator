/**
 * The risk-model artifact — the per-option criticality + activity risk
 * decomposition into a composite risk, as a comparison table. Bound to the typed
 * RiskModel candidate model via api/projectAdapters.toRiskRows.
 */
import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import type { ProjectArtifactModelEnvelope } from '../../api/types';
import { SOLUTION_LABELS } from '../../api/types';
import { toRiskRows, solutionAccentColor } from '../../api/projectAdapters';
import { useTokens } from '../../theme/ThemeContext';
import type { Tokens } from '../../theme/themes';
import { ComputedBadge } from './computed';

export function RiskModelView({
  envelope,
}: {
  envelope: ProjectArtifactModelEnvelope | undefined;
}): ReactNode {
  const t = useTokens();
  const rows = toRiskRows(envelope);

  if (rows.length === 0) {
    return (
      <Typography sx={{ py: 6, textAlign: 'center', color: t.muted, fontFamily: t.mono }}>
        No risk model drafted yet.
      </Typography>
    );
  }

  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2, maxWidth: 880 }}>
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
        <Typography sx={{ fontFamily: t.mono, fontSize: 12, color: t.muted }}>
          Criticality risk + activity risk → composite, per option. All
        </Typography>
        <ComputedBadge t={t} />
      </Box>

      <Paper sx={{ p: 0, overflow: 'hidden' }}>
        <Box sx={{ display: 'grid', gridTemplateColumns: '1.4fr 1fr 1fr 1fr' }}>
          {['OPTION', 'CRITICALITY', 'ACTIVITY', 'COMPOSITE'].map((h) => (
            <Box key={h} sx={{ px: 1.5, py: 0.9, borderBottom: `1.5px solid ${t.line}`, bgcolor: t.paperAlt }}>
              <Typography sx={{ fontFamily: t.mono, fontSize: 9.5, letterSpacing: '0.06em', color: t.muted }}>{h}</Typography>
            </Box>
          ))}
          {rows.map((r) => (
            <Box key={r.solutionKind} sx={{ display: 'contents' }}>
              <Box sx={{ px: 1.5, py: 1, borderBottom: `1px solid ${t.line}`, display: 'flex', alignItems: 'center', gap: 0.6 }}>
                <Box sx={{ width: 9, height: 9, bgcolor: solutionAccentColor(t, r.solutionKind), border: `1.5px solid ${t.line}` }} />
                <Typography sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 11.5, color: t.ink }}>
                  {SOLUTION_LABELS[r.solutionKind] ?? r.solutionKind}
                </Typography>
              </Box>
              <Cell t={t}>{r.criticalityRisk.toFixed(2)}</Cell>
              <Cell t={t}>{r.activityRisk.toFixed(2)}</Cell>
              <Cell strong t={t}>{r.composite.toFixed(2)}</Cell>
            </Box>
          ))}
        </Box>
      </Paper>
    </Box>
  );
}

function Cell({ t, children, strong }: { t: Tokens; children: ReactNode; strong?: boolean }): ReactNode {
  return (
    <Box sx={{ px: 1.5, py: 1, borderBottom: `1px solid ${t.line}`, display: 'flex', alignItems: 'center' }}>
      <Typography sx={{ fontFamily: t.mono, fontSize: strong === true ? 13 : 12, fontWeight: strong === true ? 700 : 500, color: t.ink }}>{children}</Typography>
    </Box>
  );
}
