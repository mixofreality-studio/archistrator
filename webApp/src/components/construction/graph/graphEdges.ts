/**
 * The GRAPH lens's edge presentation and focus rules — which edges a hover (or
 * a hovered milestone) lights, which it hides, and how each edge is drawn.
 *
 * Pure, pinned by graphEdges.test.ts. It used to live inside
 * ActivityGraphLens.tsx, where R5's one hard rule for edges — an ALARM edge is
 * never hidden — was unreachable by any test (code review, graph lens). The
 * renderer now only maps what this returns onto tokens.
 *
 * WHAT AN EDGE MAY CARRY
 * ----------------------
 * Its mode (queued calls are dashed, as in every architecture view), its App C
 * direction (the alarm channel) and hover-focus. NEVER a schedule channel: float
 * and the critical path belong to the LANE, never to a card or an edge (the
 * architect's Q2 ruling). The inputs below are the architecture's edge and the
 * focus — there is no activity on them to read a float from, by construction.
 */
import type { EdgeDirection, GraphEdge } from './activityGraphModel.ts';

/** What a hover or a hovered milestone lights. */
export interface GraphFocus {
  /** Cards lit; every other non-utility card mutes. */
  cards: ReadonlySet<string>;
  /** Edges lit; every other NON-ALARM edge hides. */
  incident: (e: GraphEdge) => boolean;
}

/** Hovering a card lights it, its neighbours, and the edges touching it. */
export function hoverFocusFor(hoveredId: string, edges: readonly GraphEdge[]): GraphFocus {
  const cards = new Set<string>([hoveredId]);
  for (const e of edges) {
    if (e.from === hoveredId) cards.add(e.to);
    if (e.to === hoveredId) cards.add(e.from);
  }
  return { cards, incident: (e) => e.from === hoveredId || e.to === hoveredId };
}

/** A hovered milestone lights its cards and the edges running between them. */
export function cardSetFocusFor(cards: ReadonlySet<string>): GraphFocus {
  return { cards, incident: (e) => cards.has(e.from) && cards.has(e.to) };
}

export interface EdgePresentation {
  id: string;
  from: string;
  to: string;
  /** A queued (or pub/sub) call — anything that is not a synchronous call. */
  dashed: boolean;
  /** The alarm channel: an upward, or unsanctioned sideways, call. */
  alarm: boolean;
  /** Hidden by hover-focus. NEVER true for an alarm edge (R5). */
  hidden: boolean;
  variant: 'focus' | 'normal';
  /** `graph-edge graph-edge-<direction>` — how specs find the alarm edges. */
  className: string;
}

export function edgeClassName(direction: EdgeDirection): string {
  return `graph-edge graph-edge-${direction}`;
}

export function edgePresentationFor(e: GraphEdge, focus: GraphFocus | null): EdgePresentation {
  const incident = focus?.incident(e) === true;
  return {
    id: e.id,
    from: e.from,
    to: e.to,
    dashed: e.mode !== 'sync',
    alarm: e.alarm,
    // An alarm edge is never hidden — R5: "never hidden, routed around, or
    // dimmed by hover-focus or filters".
    hidden: focus !== null && !incident && !e.alarm,
    variant: incident ? 'focus' : 'normal',
    className: edgeClassName(e.direction),
  };
}
