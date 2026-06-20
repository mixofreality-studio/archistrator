/**
 * App-A Activity Tracking panel — right-anchored MUI Drawer opened on Tracker
 * node click. Shows the activity's METHOD life cycle (binary-exit phases +
 * weights) derived from its committed BuildStatus, plus the real CPM fields
 * (EST · float · critical-path · workerClass).
 *
 * OMITTED intentionally (no real signal for this project):
 *   - Effort / spend % tile and bar (no cost data; per EV-chart pattern)
 *   - Projection note (no effort-vs-progress signal)
 *   - PR / branch cluster (only rendered when gitFor returns a real row)
 *
 * Honest rule: render real/derivable fields only, exactly like the EV chart
 * omits actual-cost.
 */
import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Drawer from '@mui/material/Drawer';
import Typography from '@mui/material/Typography';
import IconButton from '@mui/material/IconButton';
import Chip from '@mui/material/Chip';
import LinearProgress from '@mui/material/LinearProgress';
import CloseIcon from '@mui/icons-material/Close';
import CheckCircleIcon from '@mui/icons-material/CheckCircle';
import RadioButtonUncheckedIcon from '@mui/icons-material/RadioButtonUnchecked';

import type { NetworkNodeView } from '../../api/projectAdapters';
import type { BuildStatus } from '../../api/constructionAdapters';
import type { ConstructionRow, GitRow } from '../../api/types';
import { useTokens } from '../../theme/ThemeContext';
import type { Tokens } from '../../theme/themes';
import { KindBadge, type ActivityKind } from './KindBadge';
import { StatusChip } from './status';
import { GitRowMeta } from '../GitStatus';
import { phaseStateFor, progressPct, type PhaseState } from './lifecycleTemplates';
import { UI_IDENTIFIERS } from '../../constants/UIIdentifiers';

// ---------------------------------------------------------------------------
// Public props
// ---------------------------------------------------------------------------

export interface ActivityLifecyclePanelProps {
  activityId: string | null;
  /** Human-readable activity title from the activity-list slot (e.g. "Build Billing Gateway Access"). Falls back to activityId when absent. */
  activityTitle?: string | undefined;
  node: NetworkNodeView | undefined;
  row: ConstructionRow | undefined;
  /** The derived status from the tracker status map (includes eligible/blocked). */
  derivedStatus: BuildStatus;
  git: GitRow | undefined;
  onClose: () => void;
}

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

export function ActivityLifecyclePanel({
  activityId,
  activityTitle,
  node,
  row,
  derivedStatus,
  git,
  onClose,
}: ActivityLifecyclePanelProps): ReactNode {
  const t = useTokens();

  return (
    <Drawer
      anchor="right"
      open={activityId !== null}
      slotProps={{
        paper: {
          sx: {
            width: { xs: '100%', sm: 480 },
            bgcolor: t.paper,
            backgroundImage: 'none',
          },
        },
      }}
      onClose={onClose}
    >
      {activityId !== null && (
        <Box sx={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
          {/* ---- Header -------------------------------------------------- */}
          <PanelHeader
            activityId={activityId}
            activityTitle={activityTitle}
            derivedStatus={derivedStatus}
            git={git}
            node={node}
            row={row}
            t={t}
            onClose={onClose}
          />

          {/* ---- Body ---------------------------------------------------- */}
          <Box sx={{ flexGrow: 1, overflowY: 'auto', px: 2.5, py: 2 }}>
            {row !== undefined ? (
              <PanelBody
                derivedStatus={derivedStatus}
                kind={row.kind}
                node={node}
                t={t}
              />
            ) : (
              <NoConstructionData activityId={activityId} derivedStatus={derivedStatus} node={node} t={t} />
            )}
          </Box>
        </Box>
      )}
    </Drawer>
  );
}

// ---------------------------------------------------------------------------
// Header
// ---------------------------------------------------------------------------

function PanelHeader({
  activityId,
  activityTitle,
  row,
  node,
  derivedStatus,
  git,
  t,
  onClose,
}: {
  activityId: string;
  activityTitle: string | undefined;
  row: ConstructionRow | undefined;
  node: NetworkNodeView | undefined;
  derivedStatus: BuildStatus;
  git: GitRow | undefined;
  t: Tokens;
  onClose: () => void;
}): ReactNode {
  return (
    <Box
      data-testid={UI_IDENTIFIERS.Construction.ACTIVITY_LIFECYCLE_PANEL}
      sx={{
        flexShrink: 0,
        px: 2.5,
        py: 2,
        borderBottom: `1.5px solid ${t.line}`,
        borderTop: `4px solid ${t.accent}`,
        bgcolor: t.paperAlt,
      }}
    >
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
        <Typography
          sx={{
            fontFamily: t.mono,
            fontSize: 10.5,
            letterSpacing: '0.18em',
            color: t.accent,
          }}
        >
          ACTIVITY TRACKING · APP A
        </Typography>
        <Box sx={{ flexGrow: 1 }} />
        <IconButton
          aria-label="close activity lifecycle panel"
          size="small"
          sx={{ color: t.ink }}
          onClick={onClose}
        >
          <CloseIcon fontSize="small" />
        </IconButton>
      </Box>

      {/* Activity id (mono, small) + human-readable title (large heading) */}
      <Typography
        sx={{
          fontFamily: t.mono,
          fontSize: 11,
          fontWeight: 700,
          color: t.muted,
          mt: 0.25,
          letterSpacing: '0.06em',
        }}
      >
        {activityId}
      </Typography>
      <Typography
        sx={{
          fontFamily: t.display,
          fontWeight: 800,
          fontSize: 22,
          color: t.ink,
          lineHeight: 1.15,
        }}
      >
        {activityTitle ?? activityId}
      </Typography>

      {/* Chips row */}
      <Box
        sx={{
          display: 'flex',
          alignItems: 'center',
          gap: 0.75,
          mt: 0.5,
          flexWrap: 'wrap',
        }}
      >
        {row !== undefined && <KindBadge kind={row.kind} size="xs" t={t} />}
        <StatusChip size="xs" status={derivedStatus} t={t} />
        {node !== undefined && node.workerClass.length > 0 && (
          <WorkerChip t={t} workerClass={node.workerClass} />
        )}
      </Box>

      {/* Git row — only when a real row exists (honest-empty for this project) */}
      {git !== undefined && (
        <Box sx={{ mt: 0.75 }}>
          <GitRowMeta git={git} />
        </Box>
      )}
    </Box>
  );
}

// ---------------------------------------------------------------------------
// Body when ConstructionRow exists (kind is known → full lifecycle)
// ---------------------------------------------------------------------------

function PanelBody({
  kind,
  derivedStatus,
  node,
  t,
}: {
  kind: ActivityKind;
  derivedStatus: BuildStatus;
  node: NetworkNodeView | undefined;
  t: Tokens;
}): ReactNode {
  const phases = phaseStateFor(kind, derivedStatus);
  const pct = progressPct(phases);
  const doneCount = phases.filter((p) => p.done).length;
  const totalCount = phases.length;

  return (
    <Box>
      {/* ---- Metric tiles ------------------------------------------------ */}
      <Box
        sx={{
          display: 'grid',
          gridTemplateColumns: node !== undefined ? '1fr 1fr' : '1fr',
          gap: 1,
          mb: 2,
        }}
      >
        {node !== undefined && (
          <NumCell
            accent={node.onCriticalPath}
            hint={
              node.onCriticalPath ? 'critical · float 0' : `float ${String(node.float)}d`
            }
            label="EST"
            t={t}
            value={`${String(node.days)}d`}
          />
        )}
        <NumCell
          accent={false}
          hint={`${String(doneCount)} of ${String(totalCount)}`}
          label="PHASE"
          t={t}
          value={`${String(doneCount)}/${String(totalCount)}`}
        />
      </Box>

      {/* ---- Progress bar ------------------------------------------------ */}
      <Box sx={{ mb: 2 }}>
        <Box sx={{ display: 'flex', justifyContent: 'space-between', mb: 0.4 }}>
          <Typography sx={{ fontFamily: t.mono, fontSize: 9.5, color: t.muted }}>
            progress (Σ done-phase weights)
          </Typography>
          <Typography
            sx={{
              fontFamily: t.mono,
              fontSize: 9.5,
              fontWeight: 700,
              color: t.committedDot,
            }}
          >
            {String(pct)}%
          </Typography>
        </Box>
        <LinearProgress
          sx={{
            height: 7,
            borderRadius: 99,
            bgcolor: t.bg,
            border: `1px solid ${t.line}`,
            '& .MuiLinearProgress-bar': { bgcolor: t.committedDot, borderRadius: 99 },
          }}
          value={Math.min(pct, 100)}
          variant="determinate"
        />
      </Box>

      {/* ---- Life-cycle phases ------------------------------------------- */}
      <Typography
        sx={{
          fontFamily: t.mono,
          fontSize: 10,
          letterSpacing: '0.1em',
          color: t.muted,
          mb: 0.75,
        }}
      >
        {kind.toUpperCase()} LIFE CYCLE · binary exit + weight (App A Table A-1)
      </Typography>
      <Box sx={{ display: 'flex', flexDirection: 'column', gap: 0.6, mb: 2 }}>
        {phases.map((p) => (
          <PhaseRow key={p.id} p={p} t={t} />
        ))}
        <Box
          sx={{
            display: 'flex',
            alignItems: 'center',
            gap: 1,
            mt: 0.4,
            px: 1,
          }}
        >
          <Typography sx={{ fontFamily: t.mono, fontSize: 10, color: t.muted, flexGrow: 1 }}>
            Σ weights
          </Typography>
          <Typography sx={{ fontFamily: t.mono, fontSize: 10, color: t.muted }}>
            {String(phases.reduce((s, p) => s + p.weight, 0))}% · earned {String(pct)}%
          </Typography>
        </Box>
      </Box>
    </Box>
  );
}

// ---------------------------------------------------------------------------
// Body when ConstructionRow is absent (kind unknown — show CPM facts only)
// ---------------------------------------------------------------------------

function NoConstructionData({
  activityId,
  derivedStatus,
  node,
  t,
}: {
  activityId: string;
  derivedStatus: BuildStatus;
  node: NetworkNodeView | undefined;
  t: Tokens;
}): ReactNode {
  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1.5 }}>
      {node !== undefined && (
        <Box sx={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 1, mb: 0.5 }}>
          <NumCell
            accent={node.onCriticalPath}
            hint={
              node.onCriticalPath ? 'critical · float 0' : `float ${String(node.float)}d`
            }
            label="EST"
            t={t}
            value={`${String(node.days)}d`}
          />
        </Box>
      )}
      <Box
        sx={{
          p: 1.5,
          border: `1.5px dashed ${t.line}`,
          borderRadius: 1,
          bgcolor: t.paperAlt,
        }}
      >
        <Typography sx={{ fontFamily: t.body, fontSize: 12.5, color: t.muted, lineHeight: 1.55 }}>
          <b>{activityId}</b> — status:{' '}
          <span style={{ color: t.ink }}>{derivedStatus}</span>. No construction
          row recorded yet — the kind-specific lifecycle will appear once this
          activity enters construction.
        </Typography>
      </Box>
    </Box>
  );
}

// ---------------------------------------------------------------------------
// Sub-components
// ---------------------------------------------------------------------------

function PhaseRow({ p, t }: { p: PhaseState; t: Tokens }): ReactNode {
  return (
    <Box
      sx={{
        display: 'flex',
        alignItems: 'flex-start',
        gap: 1,
        px: 1,
        py: 0.7,
        borderRadius: 1,
        border: `1.5px solid ${p.active ? t.accent : t.line}`,
        bgcolor: p.done ? t.committedBg : p.active ? t.awaitingBg : 'transparent',
      }}
    >
      {p.done ? (
        <CheckCircleIcon
          sx={{ fontSize: 16, color: t.committedDot, mt: 0.1, flexShrink: 0 }}
        />
      ) : (
        <RadioButtonUncheckedIcon
          sx={{ fontSize: 16, color: p.active ? t.accent : t.muted, mt: 0.1, flexShrink: 0 }}
        />
      )}
      <Box sx={{ minWidth: 0, flexGrow: 1 }}>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75 }}>
          <Typography
            sx={{
              fontFamily: t.body,
              fontWeight: 700,
              fontSize: 12.5,
              color: p.done ? t.committedFg : t.ink,
            }}
          >
            {p.name}
          </Typography>
          {p.active ? (
            <Chip
              label="IN FLIGHT"
              size="small"
              sx={{
                height: 16,
                fontSize: 8,
                color: t.awaitingFg,
                bgcolor: 'transparent',
                border: `1px solid ${t.awaitingFg}`,
              }}
            />
          ) : null}
          <Box sx={{ flexGrow: 1 }} />
          <Typography
            sx={{
              fontFamily: t.mono,
              fontSize: 10,
              fontWeight: 700,
              color: p.done ? t.committedFg : t.muted,
            }}
          >
            {String(p.weight)}%
          </Typography>
        </Box>
        <Typography
          sx={{ fontFamily: t.mono, fontSize: 9.5, color: t.muted, lineHeight: 1.4, mt: 0.15 }}
        >
          exit: {p.exitCriterion}
        </Typography>
      </Box>
    </Box>
  );
}

function NumCell({
  t,
  label,
  value,
  hint,
  accent,
}: {
  t: Tokens;
  label: string;
  value: string;
  hint: string;
  accent: boolean;
}): ReactNode {
  return (
    <Box
      sx={{
        p: 1,
        border: `1.5px solid ${t.line}`,
        borderRadius: 1,
        bgcolor: t.paperAlt,
      }}
    >
      <Typography sx={{ fontFamily: t.mono, fontSize: 8.5, letterSpacing: '0.08em', color: t.muted }}>
        {label}
      </Typography>
      <Typography
        sx={{
          fontFamily: t.display,
          fontWeight: 800,
          fontSize: 22,
          lineHeight: 1.1,
          color: accent ? t.accent : t.ink,
        }}
      >
        {value}
      </Typography>
      <Typography sx={{ fontFamily: t.mono, fontSize: 8.5, color: t.muted }}>{hint}</Typography>
    </Box>
  );
}

function WorkerChip({ t, workerClass }: { t: Tokens; workerClass: string }): ReactNode {
  return (
    <Box
      sx={{
        display: 'inline-flex',
        alignItems: 'center',
        px: 0.6,
        py: 0.1,
        borderRadius: 99,
        border: `1px solid ${t.line}`,
        bgcolor: t.paperAlt,
        fontFamily: t.mono,
        fontSize: 9,
        fontWeight: 700,
        letterSpacing: '0.04em',
        color: t.muted,
        whiteSpace: 'nowrap',
      }}
    >
      {workerClass}
    </Box>
  );
}
