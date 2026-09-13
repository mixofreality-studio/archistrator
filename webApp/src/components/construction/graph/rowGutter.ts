/**
 * The GRAPH lens's row-label gutter (designer P1-6): row labels readable at
 * fit.
 *
 * The labels used to be canvas nodes, so they scaled with the viewport: at fit
 * (zoom ≈ 0.4) "RESOURCE ACCESS" was a 5px smudge. They are now a PINNED HTML
 * gutter down the canvas's left edge, synced to the viewport's y: each label
 * sits on its row's band as the canvas pans and zooms, at a FIXED on-screen
 * size that never scales. The canvas still reserves the gutter's room at fit
 * (invisible spacer nodes where the labels were), so cards clear it.
 *
 * Pure — no React — pinned by rowGutter.test.ts.
 */

/** The gutter's width on screen. */
export const GUTTER_PX = 76;
/** The label's on-screen size — the same at every zoom. */
export const GUTTER_LABEL_PX = 10;
/** The gutter's width once cards slide beneath it — a narrow rail. */
export const GUTTER_RAIL_PX = 14;
/**
 * Where the full gutter's SOLID ground ends — the rest of its width fades out.
 * A card under the fade reads through it; a card under the solid part does not.
 */
export const GUTTER_SOLID_PX = Math.round(GUTTER_PX * 0.75);

/**
 * The gutter's width for where the FIRST card column sits on screen
 * (`transform x + zoom × 0` — the layout's first column is at flow x 0).
 *
 * The full gutter while that column sits clear of its solid ground (it may sit
 * under the fade, as it does at fit); a narrow RAIL once a pan
 * or zoom slides cards beneath (designer re-check 1: zoomed in, the gutter's
 * 95% ground covered the first column and its labels overprinted card text).
 * The rail still names each row — its label runs vertically — and covers at most
 * GUTTER_RAIL_PX of any card.
 */
export function gutterWidthFor(firstColumnScreenX: number): number {
  return Number.isFinite(firstColumnScreenX) && firstColumnScreenX >= GUTTER_SOLID_PX
    ? GUTTER_PX
    : GUTTER_RAIL_PX;
}

/**
 * How far the zoom controls sit from the canvas's left edge: just clear of the
 * gutter. They stay bottom-LEFT — on the right, the narrow-screen detail drawer
 * (non-modal, below 1200px) covered them, and with it the only way to zoom
 * (found in live verification at 1100).
 */
export const CONTROLS_OFFSET_PX = GUTTER_PX + 8;

export interface GutterRowInput {
  row: string;
  /** The row's top and height in FLOW coordinates (activityGraphLayout). */
  y: number;
  height: number;
  label: string;
}

export interface GutterLabel {
  row: string;
  label: string;
  /** The row band's top and height ON SCREEN, relative to the canvas. */
  top: number;
  height: number;
  /** Any part of the band inside the canvas. */
  visible: boolean;
}

/**
 * The label size for a zoom — deliberately independent of it. The renderer
 * passes the live zoom so a test can prove, at every zoom, that it never
 * changes the answer.
 */
export function gutterLabelFontPx(zoom: number): number {
  void zoom;
  return GUTTER_LABEL_PX;
}

/**
 * Each row's screen band from xyflow's viewport transform `[x, y, zoom]`. The
 * gutter is pinned, so the x translation is never read: panning sideways
 * moves the cards under it, never the labels.
 */
export function rowGutterLabels(
  rows: readonly GutterRowInput[],
  transform: readonly [number, number, number],
  canvasHeight: number
): GutterLabel[] {
  const [, ty, zoom] = transform;
  return rows.map((r) => {
    const top = ty + r.y * zoom;
    const height = r.height * zoom;
    return {
      row: r.row,
      label: r.label,
      top,
      height,
      visible: top + height > 0 && top < canvasHeight,
    };
  });
}
