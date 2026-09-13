/**
 * The GRAPH lens's memory across remounts, and its level-of-detail rule.
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
  const sorted = (ids: readonly string[]): string =>
    [...ids].sort((a, b) => a.localeCompare(b)).join(',');
  // The two sets are length-prefixed and kept in separate fields so an id can
  // never slide from one set into the other and collide.
  return [
    projectId,
    `c${String(componentIds.length)}:${sorted(componentIds)}`,
    `a${String(activityIds.length)}:${sorted(activityIds)}`,
  ].join('|');
}

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
  viewportStore.set(signature, { x: viewport.x, y: viewport.y, zoom: viewport.zoom });
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
