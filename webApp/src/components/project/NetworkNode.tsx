/**
 * A project-network node for the React-Flow graph — shared across the design wizard
 * AND the construction tracker. Two kinds:
 *  - activity: a card. Float-criticality (Löwy ch.8 §2, server-computed) drives the
 *    6px LEFT BORDER band; the float chip adopts the band; CRITICAL dominates (full
 *    bold border + filled header + ring). coding/noncoding is a small glyph; the
 *    numeric {float}d is ALWAYS visible (non-colour carrier, WCAG 1.4.1).
 *  - milestone: a DIAMOND glyph (public=filled / private=hollow; on-CP=critical
 *    colour, off-CP=muted) with the label below — no duration row.
 * Optional BUILD LENS: when `nodeStatus` is present the node shows a build-status
 * chip + status-tinted ring (the active one gets a strong ring) — a presentation
 * variant, not a fork. Selecting a node arms a comment anchor AND offers "Focus
 * subgraph" via the node toolbar.
 */
import type { ReactNode } from 'react';
import { Handle, Position, NodeToolbar, type NodeProps } from '@xyflow/react';
import Box from '@mui/material/Box';
import Button from '@mui/material/Button';
import Typography from '@mui/material/Typography';
import ChatBubbleOutlineIcon from '@mui/icons-material/ChatBubbleOutline';
import CenterFocusStrongIcon from '@mui/icons-material/CenterFocusStrong';
import { useTokens } from '../../theme/ThemeContext';
import type { Tokens } from '../../theme/themes';
import type { FloatBand, NetworkNodeKind } from '../../api/projectAdapters';
import type { BuildStatus } from '../../api/constructionAdapters';
import { useComments } from '../comments/CommentContext';
import { statusColor, StatusChip } from '../construction/status';
import { bandTokens, BAND_LABEL, type BandTokens } from './bandTokens';

export interface NetworkNodeData {
  activityId: string;
  kind: NetworkNodeKind;
  label: string;
  isPublic?: boolean;
  days: number;
  workerClass: string;
  float: number;
  onCriticalPath: boolean;
  coding: boolean;
  band: FloatBand;
  source: string;
  jsonPath: string;
  /** Fired when the operator chooses "Focus subgraph" from the node toolbar. */
  onFocus?: (id: string) => void;
  /** Construction build lens (optional) — colours/labels the node by build state. */
  nodeStatus?: BuildStatus;
  nodeActive?: boolean;
  [key: string]: unknown;
}

function NodeToolbarActions({ t, d }: { t: Tokens; d: NetworkNodeData }): ReactNode {
  const { setAnchor } = useComments();
  return (
    <NodeToolbar isVisible offset={6} position={Position.Top}>
      <Box sx={{ display: 'flex', gap: 0.5 }}>
        <Button
          size="small"
          startIcon={<ChatBubbleOutlineIcon sx={{ fontSize: 14 }} />}
          sx={{
            py: 0.25,
            color: t.accentText,
            bgcolor: t.accent,
            border: `1.5px solid ${t.line}`,
            '&:hover': { bgcolor: t.accent2 },
          }}
          onClick={() => {
            setAnchor({
              kind: 'node',
              label: d.activityId,
              source: d.source,
              jsonPath: d.jsonPath,
            });
          }}
        >
          Comment
        </Button>
        <Button
          size="small"
          startIcon={<CenterFocusStrongIcon sx={{ fontSize: 14 }} />}
          sx={{
            py: 0.25,
            color: t.ink,
            bgcolor: t.paper,
            border: `1.5px solid ${t.line}`,
            '&:hover': { bgcolor: t.paperAlt },
          }}
          onClick={() => {
            d.onFocus?.(d.activityId);
          }}
        >
          Focus subgraph
        </Button>
      </Box>
    </NodeToolbar>
  );
}

function FloatChipInline({
  t,
  data,
  band,
}: {
  t: Tokens;
  data: NetworkNodeData;
  band: BandTokens;
}): ReactNode {
  const crit = data.onCriticalPath;
  return (
    <Box
      sx={{
        fontFamily: t.mono,
        fontSize: 9,
        fontWeight: 700,
        color: crit ? t.accentText : band.fg,
        bgcolor: crit ? t.accent : 'transparent',
        border: `1px solid ${band.fg}`,
        borderRadius: 99,
        px: 0.55,
        py: 0.05,
        whiteSpace: 'nowrap',
        lineHeight: 1.5,
      }}
    >
      {crit ? 'CP · 0' : `${String(data.float)}d`}
    </Box>
  );
}

function MilestoneNode({ d, selected }: { d: NetworkNodeData; selected: boolean }): ReactNode {
  const t = useTokens();
  const crit = d.onCriticalPath;
  const edge = crit ? t.accent : t.muted;
  // public = filled (a demo-to-management gate); private = hollow (internal hurdle).
  const isPublic = d.isPublic === true;
  const fillColor = isPublic ? edge : 'transparent';
  const ring = selected ? `0 0 0 3px ${t.accent}` : crit ? `0 0 0 1px ${t.accent}` : 'none';
  return (
    <Box
      aria-label={`Milestone ${d.label}, ${isPublic ? 'public' : 'private'}${crit ? ', on critical path' : ''}`}
      sx={{ display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 0.5, width: 96 }}
    >
      <Box
        sx={{
          width: 26,
          height: 26,
          transform: 'rotate(45deg)',
          bgcolor: fillColor,
          border: `${String(crit ? 3 : 2)}px solid ${edge}`,
          borderRadius: '4px',
          boxShadow: ring,
        }}
      />
      <Typography
        sx={{
          fontFamily: t.mono,
          fontWeight: 700,
          fontSize: 9.5,
          color: crit ? t.accent : t.ink,
          textAlign: 'center',
          lineHeight: 1.15,
        }}
      >
        {d.label}
      </Typography>
    </Box>
  );
}

export function NetworkNode({ data, selected }: NodeProps): ReactNode {
  const t = useTokens();
  const d = data as NetworkNodeData;
  const isSelected = selected;

  if (d.kind === 'milestone') {
    return (
      <>
        <Handle position={Position.Left} style={{ opacity: 0 }} type="target" />
        <Handle position={Position.Top} style={{ opacity: 0 }} type="target" />
        <NodeToolbarActions d={d} t={t} />
        <MilestoneNode d={d} selected={isSelected} />
        <Handle position={Position.Right} style={{ opacity: 0 }} type="source" />
        <Handle position={Position.Bottom} style={{ opacity: 0 }} type="source" />
      </>
    );
  }

  const crit = d.onCriticalPath;
  const band = bandTokens(t, d.band);
  const status = d.nodeStatus;
  const statusFg = status !== undefined ? statusColor(t, status) : undefined;

  // CRITICAL dominates with a full bold border + ring; other bands carry the band
  // colour on the LEFT BORDER only. Under the build lens, the ring/border adopt the
  // status colour (active = strong ring) so build state reads on top of the band.
  const borderColor = isSelected ? t.accent : (statusFg ?? (crit ? t.accent : t.line));
  const borderWidth = crit || statusFg !== undefined ? 2.5 : 1.5;
  const ring = isSelected
    ? `0 0 0 3px ${t.accent}`
    : d.nodeActive === true && statusFg !== undefined
      ? `0 0 0 3px ${statusFg}`
      : crit
        ? `0 0 0 1px ${t.accent}`
        : 'none';
  const fill = crit ? t.paperAlt : band.soft;

  return (
    <>
      <Handle position={Position.Left} style={{ opacity: 0 }} type="target" />
      <Handle position={Position.Top} style={{ opacity: 0 }} type="target" />
      {isSelected ? <NodeToolbarActions d={d} t={t} /> : null}

      <Box
        aria-label={
          crit
            ? `${d.activityId}, ${String(d.days)} days, float 0 days, on critical path`
            : `${d.activityId}, ${String(d.days)} days, float ${String(d.float)} days, ${BAND_LABEL[d.band]}`
        }
        sx={{
          width: 196,
          bgcolor: fill,
          border: `${String(borderWidth)}px solid ${borderColor}`,
          borderLeft: `6px solid ${band.fg}`,
          borderRadius: t.radius / 8 + 0.75,
          boxShadow: ring,
          overflow: 'hidden',
        }}
      >
        <Box
          sx={{
            display: 'flex',
            alignItems: 'center',
            gap: 0.5,
            px: 1,
            py: 0.4,
            bgcolor: crit ? t.accent : t.paper,
            borderBottom: `1px solid ${crit ? t.accent : t.line}`,
          }}
        >
          <Typography
            sx={{
              fontFamily: t.mono,
              fontWeight: 700,
              fontSize: 11,
              color: crit ? t.accentText : t.ink,
            }}
          >
            {d.activityId}
          </Typography>
          <Box sx={{ flexGrow: 1 }} />
          <FloatChipInline band={band} data={d} t={t} />
        </Box>
        <Box sx={{ px: 1, py: 0.7 }}>
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75 }}>
            {/* coding/noncoding demoted from the left border to a small glyph here */}
            <Box
              aria-hidden
              sx={{
                width: 7,
                height: 7,
                flexShrink: 0,
                borderRadius: d.coding ? '2px' : '50%',
                bgcolor: d.coding ? t.accent2 : 'transparent',
                border: `1.5px solid ${d.coding ? t.accent2 : t.muted}`,
              }}
            />
            <Typography sx={{ fontFamily: t.mono, fontSize: 9.5, fontWeight: 700, color: t.ink }}>
              {String(d.days)}d
            </Typography>
            <Typography
              sx={{
                fontFamily: t.mono,
                fontSize: 9,
                color: t.muted,
                overflow: 'hidden',
                textOverflow: 'ellipsis',
                whiteSpace: 'nowrap',
              }}
            >
              {d.workerClass.length > 0 ? d.workerClass : 'unassigned'}
            </Typography>
          </Box>
          {/* Build lens: a status chip below the duration row when present. */}
          {status !== undefined && (
            <Box sx={{ mt: 0.5 }}>
              <StatusChip size="xs" status={status} t={t} />
            </Box>
          )}
        </Box>
      </Box>

      <Handle position={Position.Right} style={{ opacity: 0 }} type="source" />
      <Handle position={Position.Bottom} style={{ opacity: 0 }} type="source" />
    </>
  );
}
