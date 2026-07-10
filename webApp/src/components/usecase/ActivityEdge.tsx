/**
 * Custom React-Flow edge for the activity diagrams. Draws an orthogonal
 * (smooth-step) control/guarded-flow path and — critically — renders any guard
 * label anchored to *this* edge, just above its own TARGET node, on its own
 * solid background chip. That is what keeps a decision's fan-out readable: each
 * branch carries its guard over its own drop line instead of every guard
 * collapsing onto one shared horizontal rail (where the dashed line would strike
 * through the text). Control-flow edges carry no label and render as a plain
 * routed line. Styling (stroke, dash, width, dim) arrives via `style`; the label
 * text + placement arrive via `data`.
 */
import type { ReactNode } from 'react';
import { BaseEdge, EdgeLabelRenderer, getSmoothStepPath, type EdgeProps } from '@xyflow/react';
import { useTokens } from '../../utilities/theme/ThemeContext';

export interface ActivityEdgeData {
  /** Guard text to paint on this edge (empty → no label). */
  label: string;
  /** True when the edge is dimmed (walkthrough off-path). */
  dim: boolean;
  /** True when the edge is on the walkthrough path (emphasized label). */
  onPath: boolean;
  [key: string]: unknown;
}

export function ActivityEdge(props: EdgeProps): ReactNode {
  const t = useTokens();
  const {
    id,
    sourceX,
    sourceY,
    targetX,
    targetY,
    sourcePosition,
    targetPosition,
    markerEnd,
    style,
  } = props;

  const [path] = getSmoothStepPath({
    sourceX,
    sourceY,
    sourcePosition,
    targetX,
    targetY,
    targetPosition,
    borderRadius: 12,
    offset: 22,
  });

  const d = (props.data ?? { label: '', dim: false, onPath: false }) as ActivityEdgeData;
  const hasLabel = typeof d.label === 'string' && d.label.length > 0;

  // Anchor the label to the target end of the edge — on the short vertical drop
  // just above the target box, centered on the target. Because a decision's
  // branches each terminate at a *distinct* target, per-target anchoring means
  // guard chips never collide and never sit on the shared distribution rail.
  const labelX = targetX;
  const labelY = targetY - 22;

  return (
    <>
      <BaseEdge
        id={id}
        path={path}
        {...(markerEnd !== undefined ? { markerEnd } : {})}
        {...(style !== undefined ? { style } : {})}
      />
      {hasLabel ? (
        <EdgeLabelRenderer>
          <div
            className="nodrag nopan"
            style={{
              position: 'absolute',
              transform: `translate(-50%, -50%) translate(${String(labelX)}px, ${String(labelY)}px)`,
              maxWidth: 172,
              padding: '2px 6px',
              borderRadius: 4,
              background: t.paper,
              border: `1px solid ${d.onPath ? t.accent : t.line}`,
              boxShadow: `0 1px 2px ${t.bg}`,
              color: t.ink,
              fontFamily: t.mono,
              fontSize: 10,
              fontWeight: 700,
              lineHeight: 1.2,
              textAlign: 'center',
              whiteSpace: 'normal',
              opacity: d.dim ? 0.25 : 1,
              pointerEvents: 'none',
              zIndex: 4,
            }}
          >
            {d.label}
          </div>
        </EdgeLabelRenderer>
      ) : null}
    </>
  );
}
