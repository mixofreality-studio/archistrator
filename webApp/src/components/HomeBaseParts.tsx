/**
 * Presentational pieces for the home base: a phase progress card, an artifact
 * table-of-contents row, and the static economics strip placeholder. Ported from
 * the frozen UX mock and bound to the real PhaseCardView / ArtifactMeta view
 * models. Pure presentation — the screen owns selection + navigation.
 */
import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import Button from '@mui/material/Button';
import Chip from '@mui/material/Chip';
import LinearProgress from '@mui/material/LinearProgress';
import LockOutlinedIcon from '@mui/icons-material/LockOutlined';
import type { ArtifactMeta, PhaseCardView } from '../api/adapters';
import type { ProjectState, PlanningAssumptionsModel } from '../api/types';
import { raise, type Tokens } from '../theme/themes';
import { useTokens } from '../theme/ThemeContext';
import { UI_IDENTIFIERS } from '../constants/UIIdentifiers';

export function PhaseCard({
  phase,
  onResume,
}: {
  phase: PhaseCardView;
  onResume: () => void;
}): ReactNode {
  const t = useTokens();
  const pct = phase.total > 0 ? (phase.done / phase.total) * 100 : 0;
  return (
    <Paper
      data-testid={UI_IDENTIFIERS.HomeBase.phaseCard(phase.id)}
      sx={{
        p: 2.5,
        opacity: phase.locked ? 0.6 : 1,
        boxShadow: phase.active ? raise(t, 4) : 'none',
        display: 'flex',
        flexDirection: 'column',
        gap: 1,
      }}
    >
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
        <Typography sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 13, color: t.muted }}>
          {`PHASE ${String(phase.index)}`}
        </Typography>
        {phase.locked ? <LockOutlinedIcon sx={{ fontSize: 15, opacity: 0.6 }} /> : null}
        <Box sx={{ flexGrow: 1 }} />
        {phase.active ? (
          <Chip label="ACTIVE" size="small" sx={{ bgcolor: t.accent, color: t.accentText }} />
        ) : null}
        {!phase.locked && !phase.active && phase.total > 0 && (
          <Chip label="DONE" size="small" sx={{ bgcolor: t.committedBg, color: t.committedFg }} />
        )}
      </Box>
      <Typography sx={{ color: t.ink }} variant="h6">
        {phase.title}
      </Typography>
      <Typography sx={{ color: t.muted, mb: 0.5 }} variant="caption">
        {phase.subtitle}
      </Typography>
      {phase.total > 0 && (
        <>
          <LinearProgress
            sx={{
              height: 8,
              borderRadius: 0,
              border: `1.5px solid ${t.line}`,
              bgcolor: t.paperAlt,
              '& .MuiLinearProgress-bar': { bgcolor: phase.active ? t.accent : t.committedDot },
            }}
            value={pct}
            variant="determinate"
          />
          <Box
            sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', mt: 0.5 }}
          >
            <Typography sx={{ fontFamily: t.mono, fontSize: 12, color: t.ink }}>
              {phase.done}/{phase.total} committed
            </Typography>
            {phase.active ? (
              <Button size="small" sx={{ minWidth: 0, px: 1 }} onClick={onResume}>
                resume →
              </Button>
            ) : null}
          </Box>
        </>
      )}
      {/* Phases with no authored-artifact slots (Construction) still need an entry
          affordance once they are reachable (active / current phase). */}
      {phase.total === 0 && phase.active && !phase.locked ? <Box sx={{ display: 'flex', justifyContent: 'flex-end', mt: 0.5 }}>
          <Button size="small" sx={{ minWidth: 0, px: 1 }} onClick={onResume}>
            open console →
          </Button>
        </Box> : null}
    </Paper>
  );
}

export function TocRow({
  artifact,
  selected,
  onClick,
}: {
  artifact: ArtifactMeta;
  selected: boolean;
  onClick: () => void;
}): ReactNode {
  const t = useTokens();
  const dot =
    artifact.stage === 'committed'
      ? t.committedDot
      : artifact.stage === 'awaitingReview'
        ? t.accent
        : 'transparent';
  const muted = artifact.stage === 'empty';
  return (
    <Box
      data-testid={UI_IDENTIFIERS.HomeBase.tocRow(artifact.kind)}
      sx={{
        display: 'flex',
        alignItems: 'center',
        gap: 1.25,
        px: 2,
        py: 1.1,
        cursor: 'pointer',
        borderLeft: `3px solid ${selected ? t.accent : 'transparent'}`,
        bgcolor: selected ? t.awaitingBg : 'transparent',
        opacity: muted ? 0.5 : 1,
        '&:hover': { bgcolor: selected ? t.awaitingBg : t.paperAlt },
        borderBottom: `1px solid ${t.line}`,
      }}
      onClick={onClick}
    >
      <Box
        sx={{
          width: 9,
          height: 9,
          borderRadius: '50%',
          flexShrink: 0,
          bgcolor: dot,
          border: `1.5px solid ${muted ? t.muted : t.line}`,
        }}
      />
      <Typography
        sx={{
          fontFamily: selected ? t.mono : t.body,
          fontWeight: selected ? 700 : 500,
          fontSize: 13.5,
          color: selected ? t.awaitingFg : t.ink,
        }}
      >
        {artifact.title}
      </Typography>
    </Box>
  );
}

/** Extract revenueSharePercent from the committed planningAssumptions slot, if present. */
function revenueShareValue(project: ProjectState): string {
  const slot = project.slots.find((s) => s.kind === 'planningAssumptions');
  const pa = slot?.model.model as PlanningAssumptionsModel | undefined;
  if (pa === undefined) return '—';
  const pct = pa.terms.revenueSharePercent;
  if (pct === 0) return '—';
  return `${String(pct)}%`;
}

export function EconomicsStrip({ project }: { project: ProjectState }): ReactNode {
  const t = useTokens();
  const revenueShare = revenueShareValue(project);
  return (
    <Paper
      data-testid={UI_IDENTIFIERS.HomeBase.ECONOMICS_STRIP}
      sx={{
        p: 2,
        mb: 4,
        display: 'flex',
        alignItems: 'center',
        gap: 3,
        flexWrap: 'wrap',
        bgcolor: t.paperAlt,
      }}
    >
      <Typography sx={{ color: t.muted }} variant="subtitle2">
        ECONOMICS
      </Typography>
      <Metric hint="set at SDP review" label="build cost" t={t} value="—" />
      <Metric hint="from planning assumptions" label="revenue share" t={t} value={revenueShare} />
      <Metric hint="after first deploy" label="operated net" t={t} value="—" />
      <Box sx={{ flexGrow: 1 }} />
      <Chip
        icon={<LockOutlinedIcon sx={{ fontSize: 14 }} />}
        label="awaits Phase 2"
        size="small"
        sx={{ color: t.muted }}
        variant="outlined"
      />
    </Paper>
  );
}

function Metric({
  t,
  label,
  value,
  hint,
}: {
  t: Tokens;
  label: string;
  value: string;
  hint: string;
}): ReactNode {
  return (
    <Box>
      <Typography
        sx={{
          fontFamily: t.mono,
          fontSize: 10,
          letterSpacing: '0.1em',
          color: t.muted,
          textTransform: 'uppercase',
        }}
      >
        {label}
      </Typography>
      <Typography
        sx={{ fontFamily: t.display, fontWeight: 700, fontSize: 22, lineHeight: 1.1, color: t.ink }}
      >
        {value}
      </Typography>
      <Typography sx={{ fontFamily: t.mono, fontSize: 10, color: t.muted }}>{hint}</Typography>
    </Box>
  );
}
