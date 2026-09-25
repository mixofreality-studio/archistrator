/**
 * A graph canvas's memory across remounts, and its level-of-detail rule. It
 * lives in `components/activity/` because BOTH canvases key their viewport with
 * it — the plan screen's build-order graph (PlanGraph.tsx) and, until Task 13
 * retires it, the construction console's layer-stack lens.
 *
 * WHY A MODULE STORE
 * ------------------
 * The console polls the project read every 1.5s while the construction pump
 * cascades, and a lens switch unmounts the canvas outright. xyflow's `fitView`
 * runs on every mount, so without a memory the operator's pan and zoom snap
 * back to fit every time they glance at the list and return — the same failure
 * NetworkView.tsx:106-128 already had to fix for its selection (spec §7.6 makes
 * that pattern mandatory here). Selection itself lives in the URL (Stage B);
 * this store holds only what the URL does not: the viewport.
 *
 * The store is keyed by a CONTENT SIGNATURE of the architecture and the activity
 * set — never by any status — so a cascade completing an activity keeps the
 * viewport, while a genuinely different project or plan starts from fit.
 *
 * Pure (no React): pinned by graphViewport.test.ts under node:test.
 */

/** An xyflow viewport: translate + zoom. */
export interface GraphViewport {
  x: number;
  y: number;
  zoom: number;
}

/**
 * The identity of what the canvas draws: the project, the architecture's
 * component ids and the plan's activity ids, each set sorted so arrival order
 * never matters. Statuses, attempts and provenance are deliberately NOT inputs.
 */
export function graphSignatureOf(
  projectId: string,
  componentIds: readonly string[],
  activityIds: readonly string[]
): string {
  // Code-unit order, the same as the model and layout (`localeCompare` is
  // locale-dependent — a signature must be the same string everywhere).
  const sorted = (ids: readonly string[]): string =>
    [...ids].sort((a, b) => (a < b ? -1 : a > b ? 1 : 0)).join(',');
  // The two sets are length-prefixed and kept in separate fields so an id can
  // never slide from one set into the other and collide.
  return [
    projectId,
    `c${String(componentIds.length)}:${sorted(componentIds)}`,
    `a${String(activityIds.length)}:${sorted(activityIds)}`,
  ].join('|');
}

/**
 * How many signatures the store remembers. A signature changes whenever the
 * architecture or the plan does, so an unbounded map would grow for the life of
 * the tab; the least recently saved is dropped first (a Map iterates in
 * insertion order, and a save re-inserts).
 */
export const VIEWPORT_STORE_LIMIT = 16;

const viewportStore = new Map<string, GraphViewport>();

function finite(vp: GraphViewport): boolean {
  return Number.isFinite(vp.x) && Number.isFinite(vp.y) && Number.isFinite(vp.zoom);
}

/** The viewport last left on this signature, or undefined (the canvas fits). */
export function loadGraphViewport(signature: string): GraphViewport | undefined {
  const stored = viewportStore.get(signature);
  return stored === undefined ? undefined : { ...stored };
}

/** Remember where the operator left the canvas. A NaN viewport is never kept. */
export function saveGraphViewport(signature: string, viewport: GraphViewport): void {
  if (!finite(viewport)) {
    viewportStore.delete(signature);
    return;
  }
  viewportStore.delete(signature);
  viewportStore.set(signature, { x: viewport.x, y: viewport.y, zoom: viewport.zoom });
  while (viewportStore.size > VIEWPORT_STORE_LIMIT) {
    const oldest = viewportStore.keys().next().value;
    if (oldest === undefined) break;
    viewportStore.delete(oldest);
  }
}

// ---------------------------------------------------------------------------
// The selection outline (designer P2)
// ---------------------------------------------------------------------------

export const SELECTION_OUTLINE_MIN_PX = 2;
export const SELECTION_OUTLINE_MAX_PX = 5;

/**
 * The selected lane's outline, in FLOW px: 2 / zoom, clamped to 2–5, so it
 * reads as a steady ~2 screen px while zoomed in and never vanishes at fit
 * (at zoom 0.4 a flat 2px outline drew as 0.8 screen px). A zoom that is not a
 * positive number falls back to the minimum.
 */
export function selectionOutlinePx(zoom: number): number {
  if (!Number.isFinite(zoom) || zoom <= 0) return SELECTION_OUTLINE_MIN_PX;
  return Math.min(SELECTION_OUTLINE_MAX_PX, Math.max(SELECTION_OUTLINE_MIN_PX, 2 / zoom));
}

/** The lane's gap between its left edge and its provenance rail, in FLOW px. */
export const RAIL_GAP_MIN_PX = 4;
export const RAIL_GAP_MAX_PX = 8;

/**
 * The room between a lane's left edge — the critical path's heavy edge — and
 * its provenance rail: 2 / zoom, clamped to 4–8 FLOW px, so the two marks keep
 * at least 2 SCREEN px apart from fit (≈0.4) down to 0.25 (designer re-check
 * 7: at fit the old flat 4px drew as 1.6px and the edge touched the rail). Never
 * under the old 4px zoomed in. A zoom that is not a positive number falls back
 * to the minimum.
 */
export function railGapPx(zoom: number): number {
  if (!Number.isFinite(zoom) || zoom <= 0) return RAIL_GAP_MIN_PX;
  return Math.min(RAIL_GAP_MAX_PX, Math.max(RAIL_GAP_MIN_PX, 2 / zoom));
}

/** Below this, the drawer is taken to cover too much to frame beside it. */
export const MIN_VISIBLE_CANVAS_PX = 240;

/**
 * The canvas width a framed card centres in: the canvas less whatever the
 * narrow-screen drawer covers of its right side (designer re-check 4 — at 1100 a
 * deep link centred its card under the drawer). No drawer: the whole canvas.
 * Never under MIN_VISIBLE_CANVAS_PX (and never over the canvas).
 */
export function visibleCanvasWidthPx(
  canvasLeft: number,
  canvasRight: number,
  drawerLeft: number | undefined
): number {
  const full = Math.max(0, canvasRight - canvasLeft);
  if (drawerLeft === undefined) return full;
  const covered = Math.max(0, canvasRight - Math.max(canvasLeft, drawerLeft));
  return Math.min(full, Math.max(MIN_VISIBLE_CANVAS_PX, full - covered));
}

// ---------------------------------------------------------------------------
// Level of detail (spec §7.6, Decision D7)
// ---------------------------------------------------------------------------

/** At or above this zoom a card shows phase labels and per-task ticks. */
export const LOD1_MIN_ZOOM = 0.8;

/**
 * LOD-0 — card + lane spines, geometry and fill only (the fit view).
 * LOD-1 — spine segments gain phase labels and task ticks (zoomed in, or hovered).
 * LOD-2 (the nested Figure A-1 sub-flow) is cut this wave (§10).
 */
export type Lod = 0 | 1;

export function lodFor(zoom: number, hovered: boolean): Lod {
  return hovered || zoom >= LOD1_MIN_ZOOM ? 1 : 0;
}

// ---------------------------------------------------------------------------
// What a canvas reads once when it mounts on a signature
// ---------------------------------------------------------------------------

export interface GraphMount {
  signature: string;
  /** The viewport left on this signature; undefined means "fit the view". */
  stored: GraphViewport | undefined;
  /**
   * The card to frame once — only for a deep-linked selection on a signature
   * with no remembered viewport. A remembered viewport always wins: it is
   * exactly where the operator left the canvas.
   */
  initialFocus: string | undefined;
}

export function graphMountFor(
  signature: string,
  selectedActivityId: string | undefined,
  cardOfActivity: Readonly<Record<string, string>>
): GraphMount {
  const stored = loadGraphViewport(signature);
  const initialFocus =
    stored === undefined && selectedActivityId !== undefined
      ? cardOfActivity[selectedActivityId]
      : undefined;
  return { signature, stored, initialFocus };
}

// ---------------------------------------------------------------------------
// The canvas's height — measured, never a guessed constant
// ---------------------------------------------------------------------------

/**
 * Below this the canvas stops shrinking and the page scrolls instead. About
 * 280 (designer P1-2): at 1366×768 the page header, the toolbar, the one-line
 * ribbon and the check row leave ~390px, and the old 420 floor pushed the
 * canvas below the fold.
 */
export const CANVAS_MIN_PX = 280;
/** Breathing room between the canvas and the bottom of the scroller. */
export const CANVAS_BOTTOM_PAD_PX = 16;

/**
 * The canvas fills the scroller from where it sits AT REST down to the
 * scroller's bottom edge. What sits above it — the page header, the toolbar,
 * the milestone ribbon, the key — wraps to a different height at every width
 * (at 1280 with the pane open the ribbon and key take four lines), so a fixed
 * `calc(100vh - N)` either clips the canvas below the fold or wastes the room.
 * Measured, the same lesson the detail pane's geometry already paid for
 * (lensGeometry.ts). `canvasTopAtRest` is the canvas's top with the scroller
 * scrolled to 0, so scrolling never changes the answer.
 */
export function canvasHeightPx(
  scrollerBottom: number,
  canvasTopAtRest: number,
  /** The scroller's own bottom padding — content ends above it, or the page scrolls. */
  scrollerPadBottom = 0
): number {
  const available = Math.round(
    scrollerBottom - scrollerPadBottom - canvasTopAtRest - CANVAS_BOTTOM_PAD_PX
  );
  return Number.isFinite(available) ? Math.max(CANVAS_MIN_PX, available) : CANVAS_MIN_PX;
}
