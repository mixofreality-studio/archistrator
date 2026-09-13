/**
 * How a CARD is framed — its border, its layer edge, its opacity. Pure, pinned
 * by graphCardPresentation.test.ts and laneSchedule.test.ts; GraphNodes.tsx
 * only maps this onto tokens.
 *
 * A card is the architecture's component, not an activity: it takes its layer
 * and its coverage (hollow), and hover-focus may mute it. It NEVER takes a
 * schedule channel — float and the critical path belong to the LANE (architect
 * Q2 ruling) — so no activity field is read here beyond "is there a lane".
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
}

export interface CardFrameState {
  /** Hover-focus: this card is outside the lit neighbourhood. */
  outsideFocus: boolean;
}

export function cardFrameFor(
  card: Pick<GraphCard, 'hollow' | 'row'>,
  state: CardFrameState
): CardFrame {
  return {
    hollow: card.hollow,
    borderStyle: card.hollow ? 'dashed' : 'solid',
    layerEdge: !card.hollow,
    // Utilities are shared infrastructure — hover-focus never mutes the bar.
    muted: state.outsideFocus && card.row !== 'utility',
  };
}
