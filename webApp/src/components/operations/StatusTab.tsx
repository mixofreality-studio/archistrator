/**
 * Status / Health — a status-page-style SLO LISTVIEW backed by the live
 * operations view (readRuntimeStatus → health.sloMet / slos[] / recentEvents[]).
 * Scan every SLO at a glance (SloMet / healthy / component), plus the at-a-glance
 * rollup banner and the RuntimeStatusChanged health timeline. Degrades to an
 * awaiting state when the read is quiet. [operatedRuntimeAccess]
 */
import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import Tooltip from '@mui/material/Tooltip';
import type { Tokens } from '../../theme/themes';
import { useTokens } from '../../theme/ThemeContext';
import type { OperationsSlo, OperationsView } from '../../api/operations';
import { normalizePhase, sloSummary, formatEventTime } from '../../api/operationsAdapters';
import { UI_IDENTIFIERS } from '../../constants/UIIdentifiers';
import { PhaseChip, SloPill, phaseColor } from './phase';
import { AwaitingPanel } from './AwaitingPanel';

export function StatusTab({ view }: { view: OperationsView | undefined }): ReactNode {
  const t = useTokens();

  if (view === undefined) {
    return (
      <AwaitingPanel
        detail="This operated app has no observed runtime status — it may not be deployed, or the reconcile read is still converging. readRuntimeStatus is infra-observed; the console re-reads on the reconcile tick."
        title="No runtime status yet"
      />
    );
  }

  const rollup = normalizePhase(view.phase);
  const sum = sloSummary(view);

  return (
    <Box
      data-testid={UI_IDENTIFIERS.Operations.STATUS_TAB}
      sx={{ display: 'flex', flexDirection: 'column', gap: 2.5, maxWidth: 1080 }}
    >
      {/* the at-a-glance rollup banner */}
      <Paper sx={{ p: 0, overflow: 'hidden', borderLeft: `5px solid ${phaseColor(t, rollup)}` }}>
        <Box sx={{ px: 2.25, py: 1.5, bgcolor: t.paperAlt, display: 'flex', alignItems: 'center', gap: 2, flexWrap: 'wrap' }}>
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
            <Typography sx={{ fontFamily: t.display, fontWeight: 800, fontSize: 18, color: t.ink }}>Service health</Typography>
            <PhaseChip phase={rollup} t={t} />
          </Box>
          <Box sx={{ flexGrow: 1 }} />
          <Stat label="SLOs met" n={sum.healthy} of={sum.total} t={t} tone={sum.breaching > 0 ? 'warn' : 'ok'} />
          <Stat label="breaching" n={sum.breaching} t={t} tone={sum.breaching > 0 ? 'warn' : 'ok'} />
          <Tooltip title="Observed at the last reconcile tick. aiarch OBSERVES infrastructure-driven convergence; it does not command it.">
            <Typography sx={{ fontFamily: t.mono, fontSize: 10, color: t.muted }}>
              {view.health.detail.length > 0 ? view.health.detail : 'readRuntimeStatus'}
            </Typography>
          </Tooltip>
        </Box>
      </Paper>

      {/* SLO listview */}
      <Box>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 1 }}>
          <Typography sx={{ fontFamily: t.display, fontWeight: 700, fontSize: 16, color: t.ink }}>SLO listview</Typography>
          <Typography sx={{ fontFamily: t.mono, fontSize: 10.5, color: t.muted }}>· per-SLO posture from readRuntimeStatus</Typography>
        </Box>
        {view.slos.length === 0 ? (
          <Paper sx={{ p: 2.5, textAlign: 'center' }}>
            <Typography sx={{ fontFamily: t.mono, fontSize: 12, color: t.muted }}>No SLOs observed yet.</Typography>
          </Paper>
        ) : (
          <Paper sx={{ p: 0, overflow: 'hidden' }}>
            {view.slos.map((s, i) => (
              <SloListRow key={`${s.component}-${String(i)}`} last={i === view.slos.length - 1} s={s} t={t} />
            ))}
          </Paper>
        )}
      </Box>

      {/* health timeline */}
      <Box>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 1 }}>
          <Typography sx={{ fontFamily: t.display, fontWeight: 700, fontSize: 16, color: t.ink }}>Health timeline</Typography>
          <Typography sx={{ fontFamily: t.mono, fontSize: 10.5, color: t.muted }}>· RuntimeStatusChanged (operatedSystemStateAccess)</Typography>
        </Box>
        {view.recentEvents.length === 0 ? (
          <Paper sx={{ p: 2.5, textAlign: 'center' }}>
            <Typography sx={{ fontFamily: t.mono, fontSize: 12, color: t.muted }}>No recent transitions.</Typography>
          </Paper>
        ) : (
          <Paper sx={{ p: 0, overflow: 'hidden' }}>
            {view.recentEvents.map((e, i) => (
              <Box
                key={`${e.at}-${String(i)}`}
                sx={{ display: 'flex', alignItems: 'flex-start', gap: 1.5, px: 2, py: 1.1, borderBottom: i === view.recentEvents.length - 1 ? 'none' : `1px solid ${t.line}` }}
              >
                <Typography sx={{ fontFamily: t.mono, fontSize: 10.5, color: t.muted, minWidth: 96, pt: 0.2 }}>{formatEventTime(e.at)}</Typography>
                <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5, pt: 0.1 }}>
                  <PhaseChip phase={normalizePhase(e.from)} size="xs" t={t} />
                  <Typography sx={{ color: t.muted, fontSize: 12 }}>→</Typography>
                  <PhaseChip phase={normalizePhase(e.to)} size="xs" t={t} />
                </Box>
                <Typography sx={{ fontFamily: t.body, fontSize: 12.5, color: t.ink, lineHeight: 1.4, flexGrow: 1 }}>{e.note}</Typography>
              </Box>
            ))}
          </Paper>
        )}
      </Box>
    </Box>
  );
}

function Stat({ t, n, of, label, tone }: { t: Tokens; n: number; of?: number; label: string; tone: 'ok' | 'warn' }): ReactNode {
  const color = tone === 'warn' ? t.awaitingFg : t.committedFg;
  return (
    <Box sx={{ textAlign: 'center' }}>
      <Typography sx={{ fontFamily: t.display, fontWeight: 800, fontSize: 20, lineHeight: 1, color }}>
        {n}
        {of !== undefined && <Box component="span" sx={{ fontSize: 12, color: t.muted }}>/{of}</Box>}
      </Typography>
      <Typography sx={{ fontFamily: t.mono, fontSize: 9, letterSpacing: '0.08em', color: t.muted, textTransform: 'uppercase' }}>{label}</Typography>
    </Box>
  );
}

function SloListRow({ s, t, last }: { s: OperationsSlo; t: Tokens; last: boolean }): ReactNode {
  return (
    <Box
      sx={{
        display: 'flex',
        alignItems: 'center',
        gap: 1.5,
        px: 2,
        py: 1.25,
        borderBottom: last ? 'none' : `1px solid ${t.line}`,
        borderLeft: `4px solid ${s.sloMet ? 'transparent' : t.accent}`,
        '&:hover': { bgcolor: t.paperAlt },
      }}
    >
      <Box sx={{ minWidth: 0, flexGrow: 1 }}>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, flexWrap: 'wrap' }}>
          <Typography sx={{ fontFamily: t.body, fontWeight: 700, fontSize: 14, color: t.ink }}>{s.component}</Typography>
        </Box>
        <Typography sx={{ fontFamily: t.mono, fontSize: 10.5, color: t.muted, mt: 0.25 }}>{s.objective}</Typography>
      </Box>
      <Box sx={{ display: 'flex', flexDirection: 'column', alignItems: 'flex-end', gap: 0.5, flexShrink: 0 }}>
        <SloPill met={s.sloMet} t={t} />
        <Typography sx={{ fontFamily: t.mono, fontSize: 9, color: s.healthy ? t.committedFg : t.awaitingFg }}>
          {s.healthy ? 'healthy' : 'unhealthy'}
        </Typography>
      </Box>
    </Box>
  );
}
