/**
 * Deterministic swim-lane → color mapping for activity diagrams. Lanes are keyed
 * by first-seen order (from UseCaseView.lanes) so a given use case always paints
 * the same way across renders. `laneColors` returns the strong accent per lane
 * (used for headers, borders, terminal fills); `laneBand` derives the faint
 * background tint painted behind a lane column.
 */
import type { Tokens } from '../../theme/themes';

export function laneColors(t: Tokens, lanes: string[]): Record<string, string> {
  const palette = [t.accent, t.accent2, t.committedDot, t.awaitingFg, t.muted];
  const map: Record<string, string> = {};
  lanes.forEach((l, i) => {
    map[l] = palette[i % palette.length] ?? t.muted;
  });
  return map;
}

/** A faint, theme-aware background band tint for alternating lane columns. */
export function laneBand(t: Tokens, laneIndex: number): string {
  // Alternate between paper and paperAlt so columns read as distinct bands in
  // every theme (both use theme tokens, so light + dark stay legible).
  return laneIndex % 2 === 0 ? t.paperAlt : t.paper;
}
