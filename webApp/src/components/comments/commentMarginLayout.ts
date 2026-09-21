/**
 * Margin-card placement: the Google-Docs stacking pass.
 *
 * Every comment thread wants to sit level with the content it anchors to. When
 * two anchors are close enough that their cards would overlap, the later card
 * slides down just far enough to clear the earlier one — so cards never overlap,
 * never reorder, and stay as close to their anchor as the neighbours allow.
 *
 * Pure and DOM-free so `node --test` can reach it; the renderer measures anchors
 * and hands the numbers here.
 */
export interface MarginCard {
  id: string;
  /** Anchor offset within the scroll container, in px. */
  desiredTop: number;
  /** Measured card height, in px. */
  height: number;
}

export interface PlacedCard {
  id: string;
  top: number;
}

export function stackCards(cards: readonly MarginCard[], gap: number): PlacedCard[] {
  const sorted = [...cards].sort((a, b) => a.desiredTop - b.desiredTop);
  const placed: PlacedCard[] = [];
  let floor = Number.NEGATIVE_INFINITY;
  for (const card of sorted) {
    const top = Math.max(card.desiredTop, floor);
    placed.push({ id: card.id, top });
    floor = top + card.height + gap;
  }
  return placed;
}
