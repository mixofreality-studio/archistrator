/**
 * A read-only thumbnail of an activity's lifecycle graph, for list rows (the
 * project plan). Same layout and geometry as LifecycleGraph at a fraction of the
 * size: no pill, no labels, no arrowheads, no icons, no interaction — just the shape of the
 * activity and how far along it is. One SVG, named as an image.
 */
import { useMemo, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import { useTokens } from '../../utilities/theme/ThemeContext';
import type { Tokens } from '../../utilities/theme/themes';
import { UI_IDENTIFIERS } from '../../utilities/constants/UIIdentifiers';
import { assertNever } from '../../contracts/exhaustive';
import { layoutLifecycleGraph } from './lifecycleGraphLayout.ts';
import { lifecycleGeometry, type LifecycleMetrics } from './lifecycleGraphGeometry.ts';
import type { LifecycleNode, LifecycleNodeState } from './lifecycleGraphTypes.ts';

const MINI: LifecycleMetrics = {
  pip: 9,
  gap: 7,
  laneHeight: 13,
  bend: 16,
  bendPerLane: 4,
  labelRow: 0,
  labelGap: 0,
  labelPad: 0,
  labelHeight: 0,
  // No arrowheads, arcs or icons down here: at 9px they are mush. Direction is
  // left→right by position, and both task kinds are the same plain dot — the
  // thumbnail answers "what shape is this activity and how far along", nothing finer.
  arrowLen: 0,
  arrowHalf: 0,
  arrowGap: 0,
  arcStart: 0,
  arcLand: 0,
  arcBow: 0,
  arcBadge: { w: 0, h: 0 },
};

function miniPaint(t: Tokens, state: LifecycleNodeState, surface: string): [string, string] {
  switch (state) {
    case 'done':
      return [t.committedDot, t.committedDot];
    case 'running':
      return [surface, t.accent];
    case 'awaitingHuman':
      return [t.accent, t.accent];
    case 'sentBack':
      return [surface, t.awaitingFg];
    case 'failed':
      return [t.dangerFg, t.dangerFg];
    case 'locked':
    case 'pending':
      return [surface, t.line];
    default:
      return assertNever(state);
  }
}

export function LifecycleGraphMini({
  nodes,
  label,
  surface,
  maxWidth,
}: {
  nodes: readonly LifecycleNode[];
  /** Accessible name, e.g. "Lifecycle: 4 of 11 tasks done". */
  label: string;
  surface?: string | undefined;
  /** Scale the drawing down (never up) to fit this width — a fixed-size tile
   *  holds a 3-task and an 11-task activity alike. */
  maxWidth?: number | undefined;
}): ReactNode {
  const t = useTokens();
  const bg = surface ?? t.paper;
  const layout = useMemo(() => layoutLifecycleGraph(nodes), [nodes]);
  const g = useMemo(() => lifecycleGeometry(layout, nodes, [], undefined, MINI), [layout, nodes]);
  const stateOf = new Map(nodes.map((n) => [n.id, n.state]));
  const r = MINI.pip / 2;
  const drawn = g.width + 2;
  const scale = maxWidth !== undefined && drawn > maxWidth ? maxWidth / drawn : 1;
  return (
    <Box
      aria-label={label}
      component="svg"
      data-testid={UI_IDENTIFIERS.ActivityLifecycle.MINI}
      height={g.height * scale}
      role="img"
      sx={{ display: 'block', flexShrink: 0, overflow: 'visible' }}
      viewBox={`-1 0 ${String(drawn)} ${String(g.height)}`}
      width={drawn * scale}
    >
      {g.rails.map((rail) => {
        const to = stateOf.get(rail.to) ?? 'pending';
        const on = stateOf.get(rail.from) === 'done' && to !== 'pending' && to !== 'locked';
        return (
          <path
            d={rail.d}
            fill="none"
            key={`${rail.from}>${rail.to}`}
            stroke={on ? t.accent : t.line}
            strokeWidth={1.5}
          />
        );
      })}
      {nodes.map((n) => {
        const pos = layout.positions.get(n.id);
        const cx = g.x.get(n.id);
        if (pos === undefined || cx === undefined) return null;
        const cy = g.laneY(pos.lane);
        const [fill, stroke] = miniPaint(t, n.state, bg);
        return (
          <rect
            fill={fill}
            height={MINI.pip}
            key={n.id}
            rx={r}
            stroke={stroke}
            strokeWidth={1.5}
            width={MINI.pip}
            x={cx - r}
            y={cy - r}
          />
        );
      })}
    </Box>
  );
}
