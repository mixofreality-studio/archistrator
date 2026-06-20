/**
 * One solution-option artifact (normal / decompressed / subcritical / compressed)
 * — the option's defining knobs: staffing cap, calendar, schedule buffer, and the
 * per-worker-class build-cost rates. Duration / cost / risk are NOT stored on the
 * Solution slot (they are computed by the estimate Engines and joined into the SDP
 * review), so this view surfaces the AUTHORED knobs and points at the SDP review
 * for the computed figures. Ported visual design from ux-mock SolutionView, bound
 * to the real typed Solution candidate model.
 */
import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import StarIcon from '@mui/icons-material/Star';
import type { ProjectArtifactKind, ProjectArtifactModelEnvelope } from '../../api/types';
import { SOLUTION_LABELS } from '../../api/types';
import { toSolutionView, formatMoney, solutionAccentColor } from '../../api/projectAdapters';
import { useTokens } from '../../theme/ThemeContext';
import type { Tokens } from '../../theme/themes';
import { AuthoredBadge, ComputedBadge } from './computed';

const SOLUTION_LENS: Partial<Record<ProjectArtifactKind, string>> = {
  normalSolution: 'Minimum staffing for unimpeded critical-path progress.',
  decompressedSolution: 'Extended duration to drop criticality risk toward the tipping point.',
  subcriticalSolution: 'Deliberately understaffed — longer, costlier, riskier than normal.',
  compressedSolution: 'Shorter duration via parallel work then top resources.',
};

function Fig({ t, k, v, strong }: { t: Tokens; k: string; v: string; strong?: boolean }): ReactNode {
  return (
    <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'baseline', gap: 1, py: 0.25 }}>
      <Typography sx={{ fontFamily: t.body, fontSize: 12.5, color: t.muted }}>{k}</Typography>
      <Typography sx={{ fontFamily: t.mono, fontSize: strong === true ? 15 : 13, fontWeight: strong === true ? 700 : 500, color: t.ink }}>{v}</Typography>
    </Box>
  );
}

export function SolutionView({
  envelope,
  kind,
}: {
  envelope: ProjectArtifactModelEnvelope | undefined;
  kind: ProjectArtifactKind;
}): ReactNode {
  const t = useTokens();
  const view = toSolutionView(envelope, kind);
  const color = solutionAccentColor(t, kind);
  const title = SOLUTION_LABELS[kind] ?? kind;
  const recommended = kind === 'decompressedSolution';

  if (view === undefined) {
    return (
      <Typography sx={{ py: 6, textAlign: 'center', color: t.muted, fontFamily: t.mono }}>
        No {title.toLowerCase()} solution drafted yet.
      </Typography>
    );
  }

  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2, maxWidth: 1000 }}>
      <Paper sx={{ p: 0, overflow: 'hidden', borderLeft: `5px solid ${color}` }}>
        <Box sx={{ px: 2.5, py: 1.75, display: 'flex', alignItems: 'center', gap: 1.5, bgcolor: t.paperAlt, borderBottom: `1.5px solid ${t.line}` }}>
          <Typography sx={{ fontFamily: t.display, fontWeight: 800, fontSize: 22, color: t.ink }}>{title}</Typography>
          {recommended ? <StarIcon sx={{ fontSize: 16, color: t.accent }} /> : null}
          <Box sx={{ flexGrow: 1 }} />
          <AuthoredBadge label="knobs" t={t} />
        </Box>

        <Box sx={{ px: 2.5, py: 1, borderBottom: `1px solid ${t.line}` }}>
          <Typography sx={{ fontFamily: t.mono, fontSize: 12, color: t.muted }}>{SOLUTION_LENS[kind] ?? ''}</Typography>
        </Box>

        <Box sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', md: '1fr 1fr' }, gap: 0 }}>
          {/* knobs */}
          <Box sx={{ p: 2.5, borderRight: { md: `1px solid ${t.line}` }, borderBottom: { xs: `1px solid ${t.line}`, md: 'none' } }}>
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.6, mb: 1 }}>
              <Typography sx={{ fontFamily: t.mono, fontSize: 10, letterSpacing: '0.1em', color: t.muted }}>DEFINING KNOBS</Typography>
              <AuthoredBadge t={t} />
            </Box>
            <Fig strong k="Staffing cap" t={t} v={`${String(view.staffingCap)} concurrent`} />
            <Fig k="Calendar" t={t} v={`${String(view.calendarDaysPerWeek)} d/wk`} />
            <Fig k="Schedule buffer" t={t} v={`${String(view.bufferDays)} d`} />
          </Box>
          {/* class rates */}
          <Box sx={{ p: 2.5 }}>
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.6, mb: 1 }}>
              <Typography sx={{ fontFamily: t.mono, fontSize: 10, letterSpacing: '0.1em', color: t.muted }}>BUILD-COST RATES</Typography>
              <AuthoredBadge t={t} />
            </Box>
            {view.classRates.length === 0 ? (
              <Typography sx={{ fontFamily: t.mono, fontSize: 11.5, color: t.muted }}>No class rates specified.</Typography>
            ) : (
              view.classRates.map((r) => <Fig k={r.workerClass} key={r.workerClass} t={t} v={`${formatMoney(r.rate)} / day`} />)
            )}
          </Box>
        </Box>

        <Box sx={{ px: 2.5, py: 1.5, bgcolor: t.committedBg, borderTop: `1.5px solid ${t.line}`, display: 'flex', alignItems: 'center', gap: 1 }}>
          <ComputedBadge t={t} />
          <Typography sx={{ fontFamily: t.body, fontSize: 13, color: t.committedFg }}>
            Duration, cost &amp; risk for this option are <b>computed</b> by the estimate Engines and joined into the SDP review.
          </Typography>
        </Box>
      </Paper>
    </Box>
  );
}
