/**
 * The left-column activity list for the Artifacts tab. Displays every activity
 * that has a construction row (joined with the activity-list name), with kind
 * badge, status chip, progress bar, and artifact count. Ported from the ux-mock
 * ArtifactsTab.tsx ListRow + list container, bound to real ConstructionRow data.
 */
import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import type { Tokens } from '../../theme/themes';
import type { ConstructionRow } from '../../api/types';
import { StatusChip } from './status';
import type { BuildStatus } from '../../api/constructionAdapters';
import { KindBadge, kindColor } from './KindBadge';

/** A view-model row joining a ConstructionRow with the activity-list display name. */
export interface ArtifactActivityVM {
  activityId: string;
  name: string;
  row: ConstructionRow;
}

/** Compute progress percentage for a construction row (produced count / total). */
function progressOf(row: ConstructionRow): number {
  const total = row.produced?.length ?? 0;
  if (total === 0) return 0;
  const done = row.produced?.filter((a) => a.produced).length ?? 0;
  return Math.round((done / total) * 100);
}

function ListRow({
  onClick,
  selected,
  t,
  vm,
}: {
  onClick: () => void;
  selected: boolean;
  t: Tokens;
  vm: ArtifactActivityVM;
}): ReactNode {
  const pct = progressOf(vm.row);
  const artifactCount = vm.row.produced?.length ?? 0;
  // The ConstructionRow.status is a subset of BuildStatus; cast is safe.
  const status = vm.row.status as BuildStatus;

  return (
    <Box
      sx={{
        px: 1.5,
        py: 1,
        cursor: 'pointer',
        borderLeft: `3px solid ${selected ? t.accent : 'transparent'}`,
        bgcolor: selected ? t.awaitingBg : 'transparent',
        borderBottom: `1px solid ${t.line}`,
        '&:hover': { bgcolor: selected ? t.awaitingBg : t.paperAlt },
      }}
      onClick={onClick}
    >
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75, flexWrap: 'wrap' }}>
        <Typography
          sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 11, color: selected ? t.awaitingFg : t.ink }}
        >
          {vm.activityId}
        </Typography>
        <KindBadge kind={vm.row.kind} size="xs" t={t} />
        <Box sx={{ flexGrow: 1 }} />
        <StatusChip size="xs" status={status} t={t} />
      </Box>
      <Typography sx={{ fontFamily: t.body, fontSize: 12.5, color: t.ink, lineHeight: 1.25, mt: 0.25 }}>
        {vm.name}
      </Typography>
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75, mt: 0.4 }}>
        <Box
          sx={{
            flexGrow: 1,
            height: 4,
            bgcolor: t.bg,
            border: `1px solid ${t.line}`,
            borderRadius: 99,
            overflow: 'hidden',
          }}
        >
          <Box sx={{ width: `${pct.toString()}%`, height: '100%', bgcolor: kindColor(t, vm.row.kind).fg }} />
        </Box>
        <Typography sx={{ fontFamily: t.mono, fontSize: 9, color: t.muted }}>{pct}%</Typography>
        <Typography sx={{ fontFamily: t.mono, fontSize: 9, color: t.muted }}>· {artifactCount} artifacts</Typography>
      </Box>
    </Box>
  );
}

export function ArtifactActivityList({
  activities,
  onSelect,
  selectedId,
  t,
}: {
  activities: ArtifactActivityVM[];
  onSelect: (id: string) => void;
  selectedId: string;
  t: Tokens;
}): ReactNode {
  return (
    <Paper sx={{ p: 0, overflow: 'hidden', position: { md: 'sticky' }, top: 8 }}>
      <Box
        sx={{
          px: 2,
          py: 1.1,
          bgcolor: t.paperAlt,
          borderBottom: `1.5px solid ${t.line}`,
        }}
      >
        <Typography
          sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 11, letterSpacing: '0.08em', color: t.ink }}
        >
          ALL ACTIVITIES · {activities.length}
        </Typography>
      </Box>
      <Box sx={{ maxHeight: { md: 620 }, overflowY: 'auto' }}>
        {activities.map((vm) => (
          <ListRow
            key={vm.activityId}
            selected={vm.activityId === selectedId}
            t={t}
            vm={vm}
            onClick={() => { onSelect(vm.activityId); }}
          />
        ))}
      </Box>
    </Paper>
  );
}
