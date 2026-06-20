/**
 * The planning-assumptions artifact: the authored resources / calendar / declared
 * usage / settlement terms the network and SDP estimates are built on, plus a
 * load-bearing risk-flags panel surfaced from the assumptions. Ported visual
 * design from ux-mock RiskFlagsPanel + planning surface, bound to the real typed
 * PlanningAssumptions candidate model.
 */
import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import Chip from '@mui/material/Chip';
import WarningAmberIcon from '@mui/icons-material/WarningAmber';
import type { ProjectArtifactModelEnvelope } from '../../api/types';
import { narrowProject } from '../../api/projectAdapters';
import { useTokens } from '../../theme/ThemeContext';
import type { Tokens } from '../../theme/themes';
import { AuthoredBadge } from './computed';

function Stat({ t, label, value, sub }: { t: Tokens; label: string; value: string; sub: string }): ReactNode {
  return (
    <Box>
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75 }}>
        <Typography sx={{ fontFamily: t.mono, fontSize: 9.5, letterSpacing: '0.14em', color: t.muted }}>{label}</Typography>
        <AuthoredBadge t={t} />
      </Box>
      <Typography sx={{ fontFamily: t.display, fontWeight: 800, fontSize: 26, color: t.ink, lineHeight: 1.1 }}>{value}</Typography>
      <Typography sx={{ fontFamily: t.mono, fontSize: 10.5, color: t.muted }}>{sub}</Typography>
    </Box>
  );
}

export function PlanningAssumptionsView({
  envelope,
}: {
  envelope: ProjectArtifactModelEnvelope | undefined;
}): ReactNode {
  const t = useTokens();
  const model = narrowProject(envelope, 'planningAssumptions');

  if (model === undefined) {
    return (
      <Typography sx={{ py: 6, textAlign: 'center', color: t.muted, fontFamily: t.mono }}>
        No planning assumptions drafted yet.
      </Typography>
    );
  }

  const resources = model.resources;
  const usage = model.declaredUsage;
  const terms = model.terms;
  // The authored notes carry the load-bearing risk flags — split on lines so each
  // reads as a flag in the panel.
  const flags = model.notes
    .split('\n')
    .map((s) => s.trim())
    .filter((s) => s.length > 0);

  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2, maxWidth: 1040 }}>
      {/* summary strip */}
      <Paper sx={{ p: 2, display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 3 }}>
        <Stat label="RESOURCES" sub="named staff" t={t} value={String(resources.length)} />
        <Stat label="CALENDAR" sub="working days / week" t={t} value={String(model.calendarDaysPerWeek)} />
        <Stat label="DECLARED DAU" sub="daily active users" t={t} value={usage.expectedDailyActiveUsers.toLocaleString()} />
        <Stat label="REVENUE SHARE" sub="settlement rate" t={t} value={`${String(terms.revenueSharePercent)}%`} />
        <Box sx={{ flexGrow: 1 }} />
        <Box sx={{ display: 'flex', flexDirection: 'column', gap: 0.5, maxWidth: 460 }}>
          <Typography sx={{ fontFamily: t.mono, fontSize: 9.5, letterSpacing: '0.14em', color: t.muted }}>RESOURCES</Typography>
          <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 0.5 }}>
            {resources.map((r) => (
              <Chip key={r} label={r} size="small" sx={{ fontSize: 10.5, color: t.ink, bgcolor: t.paperAlt }} variant="outlined" />
            ))}
          </Box>
        </Box>
      </Paper>

      {/* declared usage detail */}
      <Paper sx={{ p: 2 }}>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 1 }}>
          <Typography sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 12, letterSpacing: '0.06em', color: t.ink }}>DECLARED USAGE</Typography>
          <AuthoredBadge t={t} />
        </Box>
        <Box sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', sm: 'repeat(3, 1fr)' }, gap: 2 }}>
          <KV k="daily active users" t={t} v={usage.expectedDailyActiveUsers.toLocaleString()} />
          <KV k="requests / minute" t={t} v={String(usage.requestsPerMinute)} />
          <KV k="avg payload bytes" t={t} v={usage.avgPayloadBytes.toLocaleString()} />
        </Box>
      </Paper>

      {/* risk flags from the authored notes */}
      {flags.length > 0 && (
        <Paper sx={{ p: 0, overflow: 'hidden', borderLeft: `5px solid ${t.awaitingFg}` }}>
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, px: 2.5, py: 1.5, bgcolor: t.awaitingBg, borderBottom: `1.5px solid ${t.line}` }}>
            <WarningAmberIcon sx={{ fontSize: 18, color: t.awaitingFg }} />
            <Typography sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 12.5, letterSpacing: '0.06em', color: t.awaitingFg }}>
              LOAD-BEARING RISK FLAGS
            </Typography>
            <AuthoredBadge t={t} />
            <Box sx={{ flexGrow: 1 }} />
            <Typography sx={{ fontFamily: t.mono, fontSize: 10.5, color: t.awaitingFg, opacity: 0.85 }}>
              flow into the network, risk model &amp; SDP
            </Typography>
          </Box>
          <Box sx={{ p: 2, display: 'flex', flexDirection: 'column', gap: 1.25 }}>
            {flags.map((f, i) => (
              <Box key={i} sx={{ display: 'flex', gap: 1.5, alignItems: 'flex-start' }}>
                <Chip label={`#${String(i + 1)}`} size="small" sx={{ mt: 0.25, height: 20, fontSize: 9, color: t.awaitingFg, bgcolor: t.awaitingBg, flexShrink: 0 }} />
                <Typography sx={{ fontFamily: t.body, fontSize: 12.5, lineHeight: 1.5, color: t.ink }}>{f}</Typography>
              </Box>
            ))}
          </Box>
        </Paper>
      )}
    </Box>
  );
}

function KV({ t, k, v }: { t: Tokens; k: string; v: string }): ReactNode {
  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 0.25 }}>
      <Typography sx={{ fontFamily: t.mono, fontSize: 10, color: t.muted }}>{k}</Typography>
      <Typography sx={{ fontFamily: t.mono, fontSize: 16, fontWeight: 700, color: t.ink }}>{v}</Typography>
    </Box>
  );
}
