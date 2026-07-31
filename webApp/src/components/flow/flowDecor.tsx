/**
 * Non-interactive decoration nodes for the layered architecture flows: the left
 * row-label gutter (`rowLabel`) and the Utilities side-bar frame (`utilityFrame`).
 * Positions + sizes are computed JSX-free in ./flowLayout (decorativeNodes); these
 * components only render. Both ignore pointer events so they never steal hover from
 * the real component nodes.
 */
import type { ReactNode } from 'react';
import {
  BaseEdge,
  EdgeLabelRenderer,
  Position,
  getSmoothStepPath,
  type EdgeProps,
  type NodeProps,
} from '@xyflow/react';
import Box from '@mui/material/Box';
import Tooltip from '@mui/material/Tooltip';
import Typography from '@mui/material/Typography';
import ErrorOutlineRoundedIcon from '@mui/icons-material/ErrorOutlineRounded';
import WarningAmberRoundedIcon from '@mui/icons-material/WarningAmberRounded';
import { useTokens } from '../../utilities/theme/ThemeContext';
import type { Anchor } from '../comments/CommentContext';
import type { Finding } from '../../contracts/types';
import { UI_IDENTIFIERS } from '../../utilities/constants/UIIdentifiers';
import { GUTTER_W, NODE_H, severityColor } from './flowLayout';
import { findingLines, maxSeverity } from './findingOverlays';
import { parallelLane, type ParallelSlot } from './parallelEdges';

/** Lateral pixels between two adjacent parallel strands. Sized so seven stacked
 *  strands (the drive-system-design chain's SystemDesignManager→AgenticJobAccess
 *  run) fan across ±54px — comfortably inside the 188px node width, so every
 *  strand still leaves and arrives within its own box. */
const PARALLEL_SPREAD = 18;
/** How much each strand also staggers its horizontal bend, so the runs do not
 *  land on one line either. Small: the bend must stay between the two rows. */
const PARALLEL_BEND = 5;
/** Where a SIDE route's sequence chip sits: this far back along the wire from
 *  the target's left edge. A side route (into the Utilities bar) doglegs across
 *  the whole diagram, so its geometric midpoint lands on whatever node happens
 *  to be in the way — the chip belongs at the ARRIVAL, next to the arrowhead,
 *  where the reader is already looking to see what was called. */
const CHIP_ARRIVAL_GAP = 22;

/**
 * A smooth-step edge whose horizontal bend sits just ABOVE the target row rather
 * than at the source→target midpoint. In the closed layered architecture a Manager
 * calls ResourceAccess directly (skipping the Engine row), so a midpoint bend lands
 * the horizontal run across the Engine band — visually confusing. Dropping the bend
 * near the target keeps the edge a clean straight drop that only turns just before
 * it arrives. Call TEXT is never rendered on these edges (it lives in the caption
 * rail); the only thing a line ever carries is a small index — a finding badge, or
 * the current call's sequence chip (founder QA round 4) that ties it to the
 * matching caption line.
 *
 * PARALLEL STRANDS: edges carrying a `parallel` slot in their data are fanned
 * laterally so several calls between the same pair read as several lines instead
 * of one (see parallelEdges.ts).
 */
export function LayeredStepEdge({
  source,
  target,
  sourceX,
  sourceY,
  targetX,
  targetY,
  sourcePosition,
  targetPosition,
  style,
  markerEnd,
  data,
}: EdgeProps): ReactNode {
  // A commentable edge (the static architecture graph) carries an anchor in its
  // data; it gets a wide invisible hit area so a click anywhere near the line arms
  // the anchor (via ArchitectureFlow's onEdgeClick — React Flow's own `selected`
  // state is inert here since the graph is controlled without a change handler).
  const d = data as
    | { comment?: Anchor; findings?: Finding[]; seqChip?: number; parallel?: ParallelSlot }
    | undefined;
  const commentable = d?.comment !== undefined;
  const findings = d?.findings ?? [];
  // Parallel-strand separation (founder QA round 4): several calls between the
  // SAME pair leave and arrive at the same handles, so without a lateral offset
  // React Flow stacks them into one visible line — and "Next" between two of
  // them looks like it did nothing. The lane is 0 for every non-parallel edge,
  // which leaves every other view's geometry untouched.
  const lane = parallelLane(d?.parallel);
  // A side route (into the Utilities bar: source Right → target Left) runs
  // horizontally, so its strands must fan VERTICALLY instead.
  const side = sourcePosition === Position.Right || targetPosition === Position.Left;
  const dx = side ? 0 : lane * PARALLEL_SPREAD;
  const dy = side ? lane * PARALLEL_SPREAD : 0;
  // Bend ~40px above the target top, but never above the source (guards the rare
  // same-row edge, e.g. a queued Manager→Manager call).
  const centerY = Math.max(targetY - 40 + lane * PARALLEL_BEND, sourceY + dy + 20);
  const [path, labelX, labelY] = getSmoothStepPath({
    sourceX: sourceX + dx,
    sourceY: sourceY + dy,
    sourcePosition,
    targetX: targetX + dx,
    targetY: targetY + dy,
    targetPosition,
    borderRadius: 8,
    ...(side ? {} : { centerY }),
  });
  return (
    <>
      <BaseEdge
        interactionWidth={commentable ? 26 : 0}
        path={path}
        {...(markerEnd !== undefined ? { markerEnd } : {})}
        {...(style !== undefined ? { style } : {})}
      />
      {findings.length > 0 ? (
        <EdgeLabelRenderer>
          <EdgeFindingBadge findings={findings} from={source} to={target} x={labelX} y={labelY} />
        </EdgeLabelRenderer>
      ) : null}
      {d?.seqChip !== undefined ? (
        <EdgeLabelRenderer>
          <EdgeSeqChip
            nudged={findings.length > 0}
            seq={d.seqChip}
            x={side ? targetX - CHIP_ARRIVAL_GAP : labelX}
            y={side ? targetY + dy : labelY}
          />
        </EdgeLabelRenderer>
      ) : null}
    </>
  );
}

/**
 * The small numbered chip at a CURRENT call's bend (founder QA round 4): the
 * global sequence number, matching the number the fragment caption prints beside
 * the same call. Without it a caption reading "6. dispatch the draft job" and a
 * lit wire on the canvas were two facts the reader had to correlate by guesswork
 * — the architect's "no canvas↔caption correspondence".
 *
 * Deliberately aria-hidden and pointer-transparent: the caption already states
 * the number, the label and both endpoints as text, so announcing it again on
 * the canvas would only duplicate. It is a visual index, not content.
 */
function EdgeSeqChip({
  seq,
  x,
  y,
  nudged,
}: {
  seq: number;
  x: number;
  y: number;
  /** Shift clear of the finding badge that already occupies this midpoint. */
  nudged: boolean;
}): ReactNode {
  const t = useTokens();
  return (
    <Box
      aria-hidden="true"
      sx={{
        position: 'absolute',
        transform: `translate(-50%, -50%) translate(${String(x + (nudged ? 22 : 0))}px, ${String(y)}px)`,
        pointerEvents: 'none',
        minWidth: 19,
        height: 19,
        px: 0.45,
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        borderRadius: '999px',
        bgcolor: t.ink,
        color: t.paper,
        border: `1.5px solid ${t.paper}`,
        fontFamily: t.mono,
        fontSize: 11,
        fontWeight: 700,
        lineHeight: 1,
      }}
    >
      {seq}
    </Box>
  );
}

/**
 * The quiet severity badge at a finding edge's bend: a small glyph in the
 * severity colour whose tooltip lists each "ruleId — message" line. Focusable
 * (tabIndex 0) so keyboard users reach the tooltip; the same lines ride its
 * aria-label for AT parity (the C4Node no-volatility-badge idiom).
 */
function EdgeFindingBadge({
  findings,
  from,
  to,
  x,
  y,
}: {
  findings: Finding[];
  from: string;
  to: string;
  x: number;
  y: number;
}): ReactNode {
  const t = useTokens();
  const severity = maxSeverity(findings);
  const color = severityColor(t, severity);
  const lines = findingLines(findings);
  const Icon = severity === 'error' ? ErrorOutlineRoundedIcon : WarningAmberRoundedIcon;
  return (
    <Tooltip
      arrow
      placement="top"
      title={
        <Box>
          {lines.map((line) => (
            <Typography key={line} sx={{ fontFamily: t.mono, fontSize: 10.5, lineHeight: 1.45 }}>
              {line}
            </Typography>
          ))}
        </Box>
      }
    >
      <Box
        aria-label={`Structure findings on ${from} → ${to}: ${lines.join('. ')}`}
        data-testid={UI_IDENTIFIERS.Architecture.findingEdge(from, to)}
        role="img"
        sx={{
          position: 'absolute',
          transform: `translate(-50%, -50%) translate(${String(x)}px, ${String(y)}px)`,
          // EdgeLabelRenderer content is pointer-inert by default; the badge needs
          // hover (tooltip) + keyboard focus.
          pointerEvents: 'all',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          width: 16,
          height: 16,
          borderRadius: '50%',
          bgcolor: t.paper,
          border: `1.5px solid ${color}`,
          outline: 'none',
          '&:focus-visible': { outline: `2px solid ${t.accent}`, outlineOffset: 1 },
        }}
        tabIndex={0}
      >
        <Icon sx={{ fontSize: 11, color }} />
      </Box>
    </Tooltip>
  );
}

interface RowLabelData {
  text: string;
  [key: string]: unknown;
}

export function RowLabelNode({ data }: NodeProps): ReactNode {
  const t = useTokens();
  const d = data as RowLabelData;
  return (
    <Box
      sx={{
        width: GUTTER_W - 22,
        height: NODE_H,
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'flex-end',
        pr: 1,
        pointerEvents: 'none',
      }}
    >
      <Typography
        sx={{
          fontFamily: t.mono,
          fontWeight: 700,
          fontSize: 12,
          letterSpacing: '0.06em',
          textTransform: 'uppercase',
          color: t.muted,
          textAlign: 'right',
          whiteSpace: 'pre-line',
          lineHeight: 1.15,
        }}
      >
        {d.text}
      </Typography>
    </Box>
  );
}

interface UtilityFrameData {
  width: number;
  height: number;
  [key: string]: unknown;
}

export function UtilityFrameNode({ data }: NodeProps): ReactNode {
  const t = useTokens();
  const d = data as UtilityFrameData;
  return (
    <Box
      sx={{
        width: d.width,
        height: d.height,
        border: `1.5px dashed ${t.line}`,
        borderRadius: 1.5,
        bgcolor: 'transparent',
        pointerEvents: 'none',
        position: 'relative',
      }}
    >
      <Typography
        sx={{
          position: 'absolute',
          top: 8,
          left: 0,
          right: 0,
          textAlign: 'center',
          fontFamily: t.mono,
          fontWeight: 700,
          fontSize: 12,
          letterSpacing: '0.1em',
          textTransform: 'uppercase',
          color: t.muted,
        }}
      >
        Utilities
      </Typography>
    </Box>
  );
}
