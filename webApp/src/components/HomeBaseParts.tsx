/**
 * Presentational pieces for the home base: an artifact table-of-contents row and
 * the static economics strip placeholder. Ported from the frozen UX mock and
 * bound to the real ArtifactMeta view model. Pure presentation — the screen owns
 * selection + navigation.
 *
 * `PhaseCard` lived here too, one per Method phase, with a committed/total
 * progress bar and a `resume →` / `open console →` button into that phase's own
 * rail. Stage 5 §7.4 replaced all three with the home base's ONE plan card (Task
 * 11) and deleted the rails they opened (Task 13), so it went with
 * `toPhaseCards`, the adapter that fed it.
 */
import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import Chip from '@mui/material/Chip';
import LockOutlinedIcon from '@mui/icons-material/LockOutlined';
import type { ArtifactMeta } from '../contracts/adapters';
import type { ProjectState, PlanningAssumptionsModel } from '../contracts/types';
import type { Tokens } from '../utilities/theme/themes';
import { useTokens } from '../utilities/theme/ThemeContext';
import { UI_IDENTIFIERS } from '../utilities/constants/UIIdentifiers';

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
