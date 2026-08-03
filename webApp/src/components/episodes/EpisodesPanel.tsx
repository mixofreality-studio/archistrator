/**
 * Pure, props-only episodes panel — the SP1 capture-seam list view (Task 10).
 * Mounted per design-artifact page (Phase 1 + Phase 2, targetRef = the page's
 * artifact-kind slug) and per construction activity (targetRef = activityId) by
 * EpisodesPanelContainer, which owns the fetch (useEpisodes.ts) and the export
 * assembly. This component only renders what it is handed: no hooks/api import
 * (components layer, eslint.platform.config.js:53).
 *
 * One row per episode: outcome chip (succeeded/failed/cancelled/gap), duration,
 * model, worker class, token groups, turns, tool count, subagent count, and an
 * optional `badges` render-prop slot (spec: the audit spine adds
 * assurance/completeness badges later without forking this component — renders
 * nothing by default). Row click expands the lineage tree (workflow → activity
 * → episode → subagent spans) + <EpisodeTimeline>.
 *
 * Token labeling (fixture-proven, ledgered — see
 * server/internal/resourceaccess/agenticjob/agenticjobaccess.go's miner
 * comment): the terminal `usage` covers MAIN-LOOP turns only. Subagent tokens
 * appear in NEITHER usage nor streamedUsage, so usage is captioned "tokens
 * (main loop)" and, whenever the episode has subagent spans, a second caption
 * names how many spans' tokens are excluded — usage is never presented as
 * whole-episode spend.
 */
import { useState, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import Chip from '@mui/material/Chip';
import IconButton from '@mui/material/IconButton';
import Menu from '@mui/material/Menu';
import MenuItem from '@mui/material/MenuItem';
import Tooltip from '@mui/material/Tooltip';
import CircularProgress from '@mui/material/CircularProgress';
import Alert from '@mui/material/Alert';
import ExpandMoreIcon from '@mui/icons-material/ExpandMore';
import ExpandLessIcon from '@mui/icons-material/ExpandLess';
import FileDownloadOutlinedIcon from '@mui/icons-material/FileDownloadOutlined';

import type {
  EpisodeKind,
  EpisodeOutcome,
  EpisodeRecordView,
  EpisodeTimeline as EpisodeTimelineModel,
} from '../../contracts/types';
import { assertNever } from '../../contracts/exhaustive';
import { useTokens } from '../../utilities/theme/ThemeContext';
import type { Tokens } from '../../utilities/theme/themes';
import { UI_IDENTIFIERS } from '../../utilities/constants/UIIdentifiers';
import { EpisodeTimeline } from './EpisodeTimeline';

// ---------------------------------------------------------------------------
// Outcome chip — token-driven, mirrors construction/status.tsx's statusFill.
// ---------------------------------------------------------------------------

function outcomeFill(t: Tokens, outcome: EpisodeOutcome): { fg: string; bg: string } {
  switch (outcome) {
    case 'succeeded':
      return { fg: t.committedFg, bg: t.committedBg };
    case 'failed':
      return { fg: t.dangerFg, bg: t.awaitingBg };
    case 'cancelled':
      return { fg: t.awaitingFg, bg: t.awaitingBg };
    case 'gap':
      return { fg: t.muted, bg: 'transparent' };
    default:
      return assertNever(outcome);
  }
}

/** Display label per episode kind — same "raw union → presentable label"
 *  treatment as the outcome chip (2026-08-02 review minor (e)). */
const EPISODE_KIND_LABEL: Record<EpisodeKind, string> = {
  design: 'Design',
  construction: 'Construction',
  review: 'Review',
  rework: 'Rework',
  answer: 'Answer',
};

function OutcomeChip({
  t,
  outcome,
  episodeId,
}: {
  t: Tokens;
  outcome: EpisodeOutcome;
  episodeId: string;
}): ReactNode {
  const f = outcomeFill(t, outcome);
  return (
    <Chip
      data-testid={UI_IDENTIFIERS.Episodes.outcomeChip(episodeId)}
      label={outcome.toUpperCase()}
      size="small"
      sx={{
        height: 18,
        fontSize: 9.5,
        fontWeight: 700,
        fontFamily: t.mono,
        letterSpacing: '0.04em',
        color: f.fg,
        bgcolor: f.bg,
        border: outcome === 'gap' ? `1px dashed ${t.line}` : `1px solid ${f.fg}`,
      }}
    />
  );
}

// ---------------------------------------------------------------------------
// Formatting helpers
// ---------------------------------------------------------------------------

function formatDuration(startedAt: string, endedAt: string): string {
  const start = Date.parse(startedAt);
  const end = Date.parse(endedAt);
  if (Number.isNaN(start) || Number.isNaN(end) || end < start) return '—';
  const seconds = Math.round((end - start) / 1000);
  if (seconds < 60) return `${String(seconds)}s`;
  const minutes = Math.floor(seconds / 60);
  const remSeconds = seconds % 60;
  return `${String(minutes)}m ${String(remSeconds)}s`;
}

function toolCount(counts: Record<string, number> | undefined): number {
  if (counts === undefined) return 0;
  return Object.values(counts).reduce((sum, n) => sum + n, 0);
}

// ---------------------------------------------------------------------------
// Lineage tree — workflow → activity → episode → subagent spans.
// ---------------------------------------------------------------------------

function TreeRow({ t, depth, label }: { t: Tokens; depth: number; label: string }): ReactNode {
  return (
    <Typography
      sx={{
        fontFamily: t.mono,
        fontSize: 11,
        color: t.ink,
        pl: depth * 1.5,
        lineHeight: 1.7,
        '&::before': { content: depth > 0 ? '"└ "' : '""', color: t.muted },
      }}
    >
      {label}
    </Typography>
  );
}

function LineageTree({ t, episode }: { t: Tokens; episode: EpisodeRecordView }): ReactNode {
  const lineage = episode.lineage;
  const spans = episode.subagentSpans ?? [];
  const activityId = lineage?.activityId;
  const episodeDepth = activityId !== undefined && activityId.length > 0 ? 2 : 1;

  return (
    <Box data-testid={UI_IDENTIFIERS.Episodes.LINEAGE_TREE} sx={{ py: 0.5 }}>
      <TreeRow depth={0} label={`workflow ${lineage?.workflowId ?? '—'}`} t={t} />
      {activityId !== undefined && activityId.length > 0 ? (
        <TreeRow depth={1} label={`activity ${activityId}`} t={t} />
      ) : null}
      <TreeRow
        depth={episodeDepth}
        label={`episode ${episode.episodeId} (run ${lineage?.runId ?? '—'})`}
        t={t}
      />
      {spans.map((s) => (
        <TreeRow
          depth={episodeDepth + 1}
          key={s.toolUseId}
          label={`subagent span · ${s.toolUseId}`}
          t={t}
        />
      ))}
    </Box>
  );
}

// ---------------------------------------------------------------------------
// One episode row
// ---------------------------------------------------------------------------

function EpisodeRow({
  t,
  episode,
  expanded,
  timeline,
  timelineLoading,
  timelineError,
  badges,
  onToggle,
}: {
  t: Tokens;
  episode: EpisodeRecordView;
  expanded: boolean;
  timeline: EpisodeTimelineModel | undefined;
  timelineLoading: boolean;
  timelineError?: string | undefined;
  badges?: ((episode: EpisodeRecordView) => ReactNode) | undefined;
  onToggle: () => void;
}): ReactNode {
  const subagentCount = episode.subagentSpans?.length ?? 0;

  return (
    <Box
      data-testid={UI_IDENTIFIERS.Episodes.episodeRow(episode.episodeId)}
      sx={{
        border: `1px solid ${t.line}`,
        borderRadius: 1,
        bgcolor: t.paper,
        overflow: 'hidden',
      }}
    >
      <Box
        role="button"
        sx={{
          display: 'flex',
          alignItems: 'center',
          gap: 1.25,
          px: 1.25,
          py: 0.85,
          cursor: 'pointer',
          flexWrap: 'wrap',
          '&:hover': { bgcolor: t.paperAlt },
        }}
        tabIndex={0}
        onClick={onToggle}
        onKeyDown={(e) => {
          if (e.key !== 'Enter' && e.key !== ' ') return;
          // Space's default action is page-scroll on a focused, non-native
          // button-like element (role="button" div) — suppress it (2026-08-02
          // review minor (f)).
          e.preventDefault();
          onToggle();
        }}
      >
        <OutcomeChip episodeId={episode.episodeId} outcome={episode.outcome} t={t} />
        <Typography sx={{ fontFamily: t.mono, fontSize: 11, color: t.muted }}>
          {EPISODE_KIND_LABEL[episode.kind]}
        </Typography>
        <Typography sx={{ fontFamily: t.mono, fontSize: 11, fontWeight: 700, color: t.ink }}>
          {formatDuration(episode.startedAt, episode.endedAt)}
        </Typography>
        {episode.model !== undefined && (
          <Typography sx={{ fontFamily: t.mono, fontSize: 10.5, color: t.muted }}>
            {episode.model}
          </Typography>
        )}
        {episode.workerClass !== undefined && (
          <Chip
            label={episode.workerClass}
            size="small"
            sx={{
              height: 16,
              fontSize: 9,
              fontFamily: t.mono,
              bgcolor: t.chatArchitectBg,
              color: t.chatArchitectFg,
            }}
          />
        )}
        <Tooltip title="tokens (main loop)">
          <Typography sx={{ fontFamily: t.mono, fontSize: 10.5, color: t.muted }}>
            {episode.usage.in.toLocaleString()} in · {episode.usage.out.toLocaleString()} out
          </Typography>
        </Tooltip>
        <Typography sx={{ fontFamily: t.mono, fontSize: 10.5, color: t.muted }}>
          {episode.numTurns ?? '—'} turns
        </Typography>
        <Typography sx={{ fontFamily: t.mono, fontSize: 10.5, color: t.muted }}>
          {String(toolCount(episode.toolCallCounts))} tools
        </Typography>
        <Typography sx={{ fontFamily: t.mono, fontSize: 10.5, color: t.muted }}>
          {String(subagentCount)} subagent{subagentCount === 1 ? '' : 's'}
        </Typography>
        {badges?.(episode)}
        <Box sx={{ flexGrow: 1 }} />
        {expanded ? (
          <ExpandLessIcon sx={{ fontSize: 18, color: t.muted }} />
        ) : (
          <ExpandMoreIcon sx={{ fontSize: 18, color: t.muted }} />
        )}
      </Box>

      {subagentCount > 0 && (
        <Typography
          sx={{
            fontFamily: t.mono,
            fontSize: 9.5,
            color: t.muted,
            px: 1.25,
            pb: expanded ? 0 : 0.85,
          }}
        >
          excludes {String(subagentCount)} subagent span{subagentCount === 1 ? '' : 's'}
        </Typography>
      )}

      {expanded ? (
        <Box sx={{ px: 1.25, pb: 1.25, display: 'flex', flexDirection: 'column', gap: 1 }}>
          <LineageTree episode={episode} t={t} />
          <EpisodeTimeline error={timelineError} loading={timelineLoading} timeline={timeline} />
        </Box>
      ) : null}
    </Box>
  );
}

// ---------------------------------------------------------------------------
// Panel
// ---------------------------------------------------------------------------

export interface EpisodesPanelProps {
  episodes: EpisodeRecordView[];
  isLoading: boolean;
  error?: string | undefined;
  selectedEpisodeId: string | null;
  onSelectEpisode: (episodeId: string | null) => void;
  timeline: EpisodeTimelineModel | undefined;
  timelineLoading: boolean;
  /** A failed getEpisodeTimeline fetch for the expanded row (2026-08-02 review
   *  finding I1) — surfaced instead of an indistinguishable-from-success empty
   *  timeline. */
  timelineError?: string | undefined;
  onExportJson: () => void;
  onExportCsv: () => void;
  exportPending?: boolean | undefined;
  /** A failed Export assembly (2026-08-02 review finding I2) — surfaced instead
   *  of silent nothing. */
  exportError?: string | undefined;
  /** Render-prop slot for future badges (assurance/completeness — audit spine
   *  workstream). Renders nothing when omitted. */
  badges?: ((episode: EpisodeRecordView) => ReactNode) | undefined;
}

export function EpisodesPanel({
  episodes,
  isLoading,
  error,
  selectedEpisodeId,
  onSelectEpisode,
  timeline,
  timelineLoading,
  timelineError,
  onExportJson,
  onExportCsv,
  exportPending = false,
  exportError,
  badges,
}: EpisodesPanelProps): ReactNode {
  const t = useTokens();
  const [open, setOpen] = useState(true);
  const [exportAnchor, setExportAnchor] = useState<HTMLElement | null>(null);

  return (
    <Paper data-testid={UI_IDENTIFIERS.Episodes.PANEL} sx={{ p: 0, overflow: 'hidden' }}>
      <Box
        sx={{
          display: 'flex',
          alignItems: 'center',
          gap: 1,
          px: 2,
          py: 1.25,
          bgcolor: t.paperAlt,
          borderBottom: open ? `1.5px solid ${t.line}` : 'none',
          cursor: 'pointer',
        }}
        onClick={() => {
          setOpen((o) => !o);
        }}
      >
        <Typography
          sx={{
            fontFamily: t.mono,
            fontWeight: 700,
            fontSize: 12,
            letterSpacing: '0.1em',
            color: t.ink,
          }}
        >
          EPISODES
        </Typography>
        <Typography sx={{ fontFamily: t.mono, fontSize: 11, color: t.muted }}>
          {String(episodes.length)}
        </Typography>
        <Box sx={{ flexGrow: 1 }} />
        <IconButton
          data-testid={UI_IDENTIFIERS.Episodes.EXPORT_MENU_BUTTON}
          disabled={exportPending || episodes.length === 0}
          size="small"
          onClick={(e) => {
            e.stopPropagation();
            setExportAnchor(e.currentTarget);
          }}
        >
          {exportPending ? (
            <CircularProgress size={16} />
          ) : (
            <FileDownloadOutlinedIcon fontSize="small" />
          )}
        </IconButton>
        <Menu
          anchorEl={exportAnchor}
          open={exportAnchor !== null}
          onClick={(e) => {
            e.stopPropagation();
          }}
          onClose={() => {
            setExportAnchor(null);
          }}
        >
          <MenuItem
            data-testid={UI_IDENTIFIERS.Episodes.EXPORT_JSON}
            onClick={() => {
              setExportAnchor(null);
              onExportJson();
            }}
          >
            Export JSON
          </MenuItem>
          <MenuItem
            data-testid={UI_IDENTIFIERS.Episodes.EXPORT_CSV}
            onClick={() => {
              setExportAnchor(null);
              onExportCsv();
            }}
          >
            Export CSV
          </MenuItem>
        </Menu>
        {open ? (
          <ExpandLessIcon sx={{ fontSize: 18, color: t.muted }} />
        ) : (
          <ExpandMoreIcon sx={{ fontSize: 18, color: t.muted }} />
        )}
      </Box>

      {open ? (
        <Box sx={{ p: 1.5, display: 'flex', flexDirection: 'column', gap: 1 }}>
          {exportError !== undefined ? (
            <Alert
              data-testid={UI_IDENTIFIERS.Common.ERROR_ALERT}
              severity="error"
              sx={{ fontFamily: t.mono, fontSize: 12 }}
            >
              Export failed: {exportError}
            </Alert>
          ) : null}
          {error !== undefined ? (
            <Alert
              data-testid={UI_IDENTIFIERS.Common.ERROR_ALERT}
              severity="error"
              sx={{ fontFamily: t.mono, fontSize: 12 }}
            >
              {error}
            </Alert>
          ) : isLoading ? (
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, py: 2 }}>
              <CircularProgress size={16} />
              <Typography sx={{ fontFamily: t.mono, fontSize: 11.5, color: t.muted }}>
                loading episodes…
              </Typography>
            </Box>
          ) : episodes.length === 0 ? (
            <Typography sx={{ fontFamily: t.mono, fontSize: 12, color: t.muted, py: 1 }}>
              no episodes captured yet.
            </Typography>
          ) : (
            episodes.map((episode) => (
              <EpisodeRow
                badges={badges}
                episode={episode}
                expanded={selectedEpisodeId === episode.episodeId}
                key={episode.episodeId}
                t={t}
                timeline={timeline}
                timelineError={timelineError}
                timelineLoading={timelineLoading}
                onToggle={() => {
                  onSelectEpisode(
                    selectedEpisodeId === episode.episodeId ? null : episode.episodeId
                  );
                }}
              />
            ))
          )}
        </Box>
      ) : null}
    </Paper>
  );
}
