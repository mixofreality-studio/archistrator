/**
 * InterventionQueue — the approval queue for the Interventions tab.
 *
 * Derives the queue HONESTLY from real constructionRows: any activity whose
 * `status` is `'in-review'` has reached the human-approval Code-Review gate
 * per the Intervention Policy (service life cycle: code review gate). Those
 * activities surface here as QueueCard rows.
 *
 * The queue count badge reflects the REAL number of in-review activities — no
 * fabrication. When no activities are in-review the caller's AwaitingPanel
 * ("No interventions pending") is rendered instead of this component (see
 * InterventionsTab for the fallback).
 *
 * Props:
 *   constructionRows   — the project's per-activity construction head-state map
 *   nameForId          — resolves activityId → display name (from activity list)
 *   gitFor             — returns a GitRow for a git-backed activity (undefined otherwise)
 *   onOpenDrawer       — called with the activityId when the operator clicks "Review"
 */
import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import Chip from '@mui/material/Chip';
import Button from '@mui/material/Button';
import BoltIcon from '@mui/icons-material/Bolt';
import type { ConstructionRow, ConstructionRows, GitRow } from '../../api/types';
import { useTokens } from '../../theme/ThemeContext';
import type { Tokens } from '../../theme/themes';
import { UI_IDENTIFIERS } from '../../constants/UIIdentifiers';
import { KindBadge, KIND_META } from './KindBadge';
import { StatusChip } from './status';
import { kindColor } from './KindBadge';
import { GitRowMeta } from '../GitStatus';

// ---------------------------------------------------------------------------
// QueueCard — one awaiting-approval activity row
// ---------------------------------------------------------------------------

function QueueCard({
  activityId,
  name,
  row,
  git,
  t,
  onOpen,
}: {
  activityId: string;
  name: string;
  row: ConstructionRow;
  git: GitRow | undefined;
  t: Tokens;
  onOpen: () => void;
}): ReactNode {
  const kc = kindColor(t, row.kind);
  const gateLabel = `gate · Code review · ${KIND_META[row.kind].label} life cycle`;
  const actionLabel =
    row.kind === 'service'
      ? 'Review contract / change'
      : row.kind === 'frontend'
        ? 'Open the design loop'
        : 'Review the test plan';

  return (
    <Paper
      data-testid={UI_IDENTIFIERS.Construction.interventionQueueCard(activityId)}
      sx={{ p: 0, overflow: 'hidden', borderLeft: `5px solid ${kc.fg}` }}
    >
      {/* header row */}
      <Box
        sx={{
          px: 2,
          py: 1.25,
          bgcolor: t.paperAlt,
          borderBottom: `1.5px solid ${t.line}`,
          display: 'flex',
          alignItems: 'center',
          gap: 1,
          flexWrap: 'wrap',
        }}
      >
        <Typography sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 12, color: t.ink }}>
          {activityId}
        </Typography>
        {name !== activityId && (
          <Typography sx={{ fontFamily: t.mono, fontSize: 10.5, color: t.muted }}>
            {name}
          </Typography>
        )}
        <KindBadge kind={row.kind} size="xs" t={t} />
        <StatusChip size="xs" status="in-review" t={t} />
        <Box sx={{ flexGrow: 1 }} />
        <Typography sx={{ fontFamily: t.mono, fontSize: 10, color: t.muted }}>
          phase · {row.phase}
        </Typography>
      </Box>

      {/* body */}
      <Box sx={{ px: 2, py: 1.5 }}>
        <Typography
          sx={{ fontFamily: t.body, fontWeight: 700, fontSize: 13.5, color: t.ink, lineHeight: 1.35 }}
        >
          {name} reached CODE REVIEW — the computed reviewer set needs your gate.
        </Typography>

        {git !== undefined && (
          <Box sx={{ mt: 1 }}>
            <GitRowMeta dense git={git} />
          </Box>
        )}

        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mt: 1.25 }}>
          <Typography sx={{ fontFamily: t.mono, fontSize: 9.5, color: t.muted }}>
            {gateLabel}
          </Typography>
          <Box sx={{ flexGrow: 1 }} />
          <Button
            data-testid={UI_IDENTIFIERS.Construction.interventionReviewButton(activityId)}
            size="small"
            sx={{ py: 0.25 }}
            variant="contained"
            onClick={onOpen}
          >
            {actionLabel}
          </Button>
        </Box>
      </Box>
    </Paper>
  );
}

// ---------------------------------------------------------------------------
// InterventionQueue (public export)
// ---------------------------------------------------------------------------

export function InterventionQueue({
  constructionRows,
  nameForId,
  gitFor,
  onOpenDrawer,
}: {
  constructionRows: ConstructionRows;
  nameForId: (id: string) => string;
  gitFor: (activityId: string) => GitRow | undefined;
  onOpenDrawer: (activityId: string) => void;
}): ReactNode {
  const t = useTokens();

  // Derive the queue HONESTLY: only activities whose status is 'in-review'.
  const inReview: { activityId: string; name: string; row: ConstructionRow }[] = Object.values(
    constructionRows,
  )
    .filter((row) => row.status === 'in-review')
    .map((row) => ({
      activityId: row.activityId,
      name: nameForId(row.activityId),
      row,
    }));

  // Sort by activityId for stable ordering.
  inReview.sort((a, b) => a.activityId.localeCompare(b.activityId));

  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
      {/* queue header */}
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
        <BoltIcon sx={{ fontSize: 18, color: t.accent }} />
        <Typography
          sx={{ fontFamily: t.display, fontWeight: 700, fontSize: 18, color: t.ink }}
        >
          Waiting for your approval
        </Typography>
        <Chip
          data-testid={UI_IDENTIFIERS.Construction.INTERVENTION_QUEUE_COUNT}
          label={`${inReview.length.toString()} waiting`}
          size="small"
          sx={{
            height: 19,
            fontSize: 9,
            fontWeight: 700,
            bgcolor: t.awaitingBg,
            color: t.awaitingFg,
          }}
        />
      </Box>

      {/* cards */}
      {inReview.map(({ activityId, name, row }) => (
        <QueueCard
          activityId={activityId}
          git={gitFor(activityId)}
          key={activityId}
          name={name}
          row={row}
          t={t}
          onOpen={() => { onOpenDrawer(activityId); }}
        />
      ))}
    </Box>
  );
}
