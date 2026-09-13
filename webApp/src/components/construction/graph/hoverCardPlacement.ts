/**
 * Where the GRAPH lens's hover card opens (designer P1-1).
 *
 * It used to be xyflow's NodeToolbar, positioned above a card or — by a
 * `topRow` guess — below it, inside the canvas's own clipping box: Manager
 * cards lost their top 44–64px and the utility bar its right edge at every
 * width. It is now an MUI Popper in a PORTAL, anchored to the card, placed to
 * the RIGHT with the LEFT as its one fallback, and kept inside the CANVAS by
 * popper.js's `flip` and `preventOverflow` (with `altAxis`, and untethered so
 * it may slide clear of its card rather than hang off the canvas edge).
 *
 * Pure — the modifier list is data — pinned by hoverCardPlacement.test.ts; the
 * clipping itself is pinned in Playwright at 1280, 1366 and 1600.
 */

export const HOVER_CARD_PLACEMENT = 'right';
export const HOVER_CARD_FALLBACKS: readonly string[] = ['left'];
/** Clearance from the canvas edge, and between the card and its hover card. */
export const HOVER_CARD_GAP_PX = 8;

export interface PopperModifierSpec {
  name: string;
  options: Record<string, unknown>;
}

/**
 * The popper.js modifiers. `boundary` is the canvas element; without one (the
 * canvas is not yet in the DOM) popper falls back to its clipping parents.
 */
export function hoverCardModifiers(boundary: Element | null | undefined): PopperModifierSpec[] {
  const bounded = boundary !== null && boundary !== undefined ? { boundary } : {};
  return [
    { name: 'offset', options: { offset: [0, HOVER_CARD_GAP_PX] } },
    {
      name: 'flip',
      options: {
        fallbackPlacements: [...HOVER_CARD_FALLBACKS],
        padding: HOVER_CARD_GAP_PX,
        ...bounded,
      },
    },
    {
      name: 'preventOverflow',
      options: { altAxis: true, tether: false, padding: HOVER_CARD_GAP_PX, ...bounded },
    },
  ];
}
