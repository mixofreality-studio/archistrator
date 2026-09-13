/**
 * How a CARD is framed — its border, its layer edge, its opacity. Pure, pinned
 * by graphCardPresentation.test.ts and laneSchedule.test.ts; GraphNodes.tsx
 * only maps this onto tokens.
 *
 * A card is the architecture's component, not an activity: it takes its layer
 * and its coverage (hollow), and hover-focus or a filter may mute it. It NEVER
 * takes a schedule channel — float and the critical path belong to the LANE
 * (architect Q2 ruling) — so no activity field is read here beyond "is there a
 * lane, and does any lane match".
 *
 * FILTERS (designer P1-4): while a filter is active a card dims as a WHOLE when
 * none of its lanes match — which includes every hollow card and every utility,
 * since neither carries a lane. A card with even one matching lane stays lit
 * (its unmatched lanes dim on their own). Filters dim; they never move anything.
 */
import type { GraphCard } from './activityGraphModel.ts';

export interface CardFrame {
  /** A component no activity builds: transparent, dashed. */
  hollow: boolean;
  borderStyle: 'solid' | 'dashed';
  /** The 3px top edge in the layer colour — only on a card that carries lanes. */
  layerEdge: boolean;
  /** Hover-focus mute. */
  muted: boolean;
  /** Filter dim: a filter is active and nothing on this card matches it. */
  filterDimmed: boolean;
}

export interface CardFrameState {
  /** Hover-focus: this card is outside the lit neighbourhood. */
  outsideFocus: boolean;
  /** Any toolbar filter is active (graphFilter.filtersActive). */
  filterActive?: boolean;
  /** Activities the filters do NOT match. */
  unmatched?: ReadonlySet<string>;
}

export function cardFrameFor(
  card: Pick<GraphCard, 'hollow' | 'row' | 'lanes'>,
  state: CardFrameState
): CardFrame {
  const unmatched = state.unmatched;
  const anyLaneMatches = card.lanes.some((l) => unmatched?.has(l.activityId) !== true);
  return {
    hollow: card.hollow,
    borderStyle: card.hollow ? 'dashed' : 'solid',
    layerEdge: !card.hollow,
    // Utilities are shared infrastructure — hover-focus never mutes the bar.
    muted: state.outsideFocus && card.row !== 'utility',
    filterDimmed: state.filterActive === true && !anyLaneMatches,
  };
}

/** One opacity rule for both mutes. */
export function cardDimmed(frame: CardFrame): boolean {
  return frame.muted || frame.filterDimmed;
}
