/**
 * Non-interactive decoration nodes for the layered architecture flows: the left
 * row-label gutter (`rowLabel`) and the Utilities side-bar frame (`utilityFrame`).
 * Positions + sizes are computed JSX-free in ./flowLayout (decorativeNodes); these
 * components only render. Both ignore pointer events so they never steal hover from
 * the real component nodes.
 */
import type { ReactNode } from 'react';
import { BaseEdge, getSmoothStepPath, type EdgeProps, type NodeProps } from '@xyflow/react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import { useTokens } from '../../theme/ThemeContext';
import type { Anchor } from '../comments/CommentContext';
import { GUTTER_W, NODE_H } from './flowLayout';

/**
 * A smooth-step edge whose horizontal bend sits just ABOVE the target row rather
 * than at the source→target midpoint. In the closed layered architecture a Manager
 * calls ResourceAccess directly (skipping the Engine row), so a midpoint bend lands
 * the horizontal run across the Engine band — visually confusing. Dropping the bend
 * near the target keeps the edge a clean straight drop that only turns just before
 * it arrives. Labels are never rendered on these edges (call text lives elsewhere).
 */
export function LayeredStepEdge({
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
  // Bend ~40px above the target top, but never above the source (guards the rare
  // same-row edge, e.g. a queued Manager→Manager call).
  const centerY = Math.max(targetY - 40, sourceY + 20);
  const [path] = getSmoothStepPath({
    sourceX,
    sourceY,
    sourcePosition,
    targetX,
    targetY,
    targetPosition,
    borderRadius: 8,
    centerY,
  });
  // A commentable edge (the static architecture graph) carries an anchor in its
  // data; it gets a wide invisible hit area so a click anywhere near the line arms
  // the anchor (via ArchitectureFlow's onEdgeClick — React Flow's own `selected`
  // state is inert here since the graph is controlled without a change handler).
  const commentable = (data as { comment?: Anchor } | undefined)?.comment !== undefined;
  return (
    <BaseEdge
      interactionWidth={commentable ? 26 : 0}
      path={path}
      {...(markerEnd !== undefined ? { markerEnd } : {})}
      {...(style !== undefined ? { style } : {})}
    />
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
