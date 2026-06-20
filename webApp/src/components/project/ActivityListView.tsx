/**
 * The activity-list artifact — coding + noncoding activities in 5-day quanta,
 * grouped by worker class, scannable, computed-vs-authored aware. Ported visual
 * design from ux-mock ActivityList, bound to the real typed ActivityList model via
 * api/projectAdapters.toActivityListView.
 */
import { useState, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import Chip from '@mui/material/Chip';
import Collapse from '@mui/material/Collapse';
import ExpandMoreIcon from '@mui/icons-material/ExpandMore';
import type { ProjectArtifactModelEnvelope } from '../../api/types';
import { toActivityListView, type ActivityRowView } from '../../api/projectAdapters';
import { useTokens } from '../../theme/ThemeContext';
import type { Tokens } from '../../theme/themes';
import { AuthoredBadge, ComputedBadge } from './computed';

function RiskChip({ row, t }: { row: ActivityRowView; t: Tokens }): ReactNode {
  const high = row.riskBucket >= 8;
  const mid = row.riskBucket >= 3 && row.riskBucket < 8;
  const fg = high ? t.accentText : mid ? t.awaitingFg : t.muted;
  const bg = high ? t.accent : mid ? t.awaitingBg : 'transparent';
  return (
    <Box
      sx={{
        fontFamily: t.mono,
        fontSize: 10.5,
        fontWeight: 700,
        color: fg,
        bgcolor: bg,
        border: `1.5px solid ${high ? t.accent : t.line}`,
        borderRadius: t.radius / 8 + 0.5,
        px: 0.6,
        py: 0.1,
        whiteSpace: 'nowrap',
      }}
    >
      {`risk ${String(row.riskBucket)}`}
    </Box>
  );
}

function Stat({ t, label, value, sub }: { t: Tokens; label: string; value: string; sub: string }): ReactNode {
  return (
    <Box>
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75 }}>
        <Typography sx={{ fontFamily: t.mono, fontSize: 9.5, letterSpacing: '0.14em', color: t.muted }}>{label}</Typography>
        <ComputedBadge t={t} />
      </Box>
      <Typography sx={{ fontFamily: t.display, fontWeight: 800, fontSize: 26, color: t.ink, lineHeight: 1.1 }}>{value}</Typography>
      <Typography sx={{ fontFamily: t.mono, fontSize: 10.5, color: t.muted }}>{sub}</Typography>
    </Box>
  );
}

export function ActivityListView({
  envelope,
}: {
  envelope: ProjectArtifactModelEnvelope | undefined;
}): ReactNode {
  const t = useTokens();
  const view = toActivityListView(envelope);
  const [open, setOpen] = useState<Set<string>>(() => new Set(view.groups.slice(0, 2).map((g) => g.group)));
  const toggle = (id: string): void =>
    { setOpen((s) => {
      const n = new Set(s);
      if (n.has(id)) n.delete(id);
      else n.add(id);
      return n;
    }); };

  if (view.totalActivities === 0) {
    return (
      <Typography sx={{ py: 6, textAlign: 'center', color: t.muted, fontFamily: t.mono }}>
        No activities drafted yet.
      </Typography>
    );
  }

  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2, maxWidth: 1040 }}>
      <Paper sx={{ p: 2, display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 3 }}>
        <Stat label="ACTIVITIES" sub={`${String(view.codingCount)} coding · ${String(view.noncodingCount)} noncoding`} t={t} value={String(view.totalActivities)} />
        <Stat label="EFFORT" sub="person-days" t={t} value={view.totalPersonDays.toLocaleString()} />
        <Stat label="QUANTA" sub="5-day quanta" t={t} value={(view.totalPersonDays / 5).toFixed(1)} />
        <Box sx={{ flexGrow: 1 }} />
        <Box sx={{ display: 'flex', flexDirection: 'column', gap: 0.5 }}>
          <Typography sx={{ fontFamily: t.mono, fontSize: 9.5, letterSpacing: '0.14em', color: t.muted }}>WORKER CLASSES</Typography>
          <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 0.5, maxWidth: 460 }}>
            {view.groups.map((g) => (
              <Chip key={g.group} label={`${g.group} · ${String(g.count)}`} size="small" sx={{ fontSize: 10, color: t.ink, bgcolor: t.paperAlt }} variant="outlined" />
            ))}
          </Box>
        </Box>
      </Paper>

      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
        <Typography sx={{ fontFamily: t.mono, fontSize: 12, color: t.muted }}>
          One detailed-design + one construction activity per component, plus integration and noncoding. Inputs are
        </Typography>
        <AuthoredBadge t={t} />
      </Box>

      {view.groups.map((g) => {
        const isOpen = open.has(g.group);
        return (
          <Paper key={g.group} sx={{ overflow: 'hidden' }}>
            <Box
              sx={{ display: 'flex', alignItems: 'center', gap: 1.5, px: 2, py: 1.25, cursor: 'pointer', bgcolor: t.paperAlt, borderBottom: isOpen ? `1.5px solid ${t.line}` : 'none' }}
              onClick={() => { toggle(g.group); }}
            >
              <Typography sx={{ fontFamily: t.display, fontWeight: 700, fontSize: 16, color: t.ink, flexGrow: 1, minWidth: 0 }}>{g.group}</Typography>
              <Chip label={`${String(g.count)} activities`} size="small" sx={{ bgcolor: t.paper, color: t.ink }} />
              <Chip label={`${String(g.totalDays)} d`} size="small" sx={{ bgcolor: t.committedBg, color: t.committedFg }} />
              <ExpandMoreIcon sx={{ color: t.muted, transform: isOpen ? 'rotate(180deg)' : 'none', transition: '120ms' }} />
            </Box>
            <Collapse in={isOpen}>
              <Box>
                <Box sx={{ display: 'grid', gridTemplateColumns: '1fr 110px 140px 96px', gap: 1, px: 2, py: 0.75, borderBottom: `1.5px solid ${t.line}` }}>
                  {['ACTIVITY', 'DURATION', 'KIND', 'RISK'].map((h) => (
                    <Typography key={h} sx={{ fontFamily: t.mono, fontSize: 9.5, letterSpacing: '0.1em', color: t.muted }}>{h}</Typography>
                  ))}
                </Box>
                {g.rows.map((r, i) => (
                  <Box
                    key={`${r.name}-${String(i)}`}
                    sx={{ display: 'grid', gridTemplateColumns: '1fr 110px 140px 96px', gap: 1, px: 2, py: 0.9, alignItems: 'center', borderBottom: `1px solid ${t.line}`, '&:last-of-type': { borderBottom: 'none' } }}
                  >
                    <Typography sx={{ fontFamily: t.body, fontSize: 13, color: t.ink, borderLeft: `4px solid ${r.coding ? t.accent2 : t.muted}`, pl: 0.75 }}>{r.name}</Typography>
                    <Typography sx={{ fontFamily: t.mono, fontSize: 12, color: t.muted }}>{r.effortDays} days</Typography>
                    <Typography sx={{ fontFamily: t.mono, fontSize: 11, color: t.muted }}>{r.coding ? 'coding' : 'noncoding'}</Typography>
                    <RiskChip row={r} t={t} />
                  </Box>
                ))}
              </Box>
            </Collapse>
          </Paper>
        );
      })}
    </Box>
  );
}
