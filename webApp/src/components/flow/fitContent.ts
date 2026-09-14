/**
 * FIT A DRAWING TO ITS WIDTH, AND THE CANVAS TO THE DRAWING — pure, so the rule
 * is tested without a renderer (designer check on renderers S3).
 *
 * xyflow's `fitView` fits BOTH axes into a canvas of a height the caller guessed.
 * Two failures followed:
 *   - the relationships canvas was 358px (640 in focus) for a two-node fact, and
 *     fit it at 1.12–1.23 — larger than the drawing's own size, in a box mostly
 *     empty;
 *   - the code canvas, before an op was expanded, centred a lone «interface»
 *     node in a canvas sized for its expansion, so the node floated.
 *
 * So the ZOOM is decided by the width alone, capped at `maxZoom` (1.0: never
 * larger than drawn) and floored at `minZoom` (past it the reader pans); the
 * canvas HEIGHT is then the drawing's height at that zoom, plus the gutters and
 * the frame, within [minHeight, maxHeight]; and the drawing is centred across
 * and TOP-aligned, so nothing floats in the space below it.
 */

export interface ContentBounds {
  x: number;
  y: number;
  width: number;
  height: number;
}

export interface ContentFit {
  /** The viewport zoom. */
  zoom: number;
  /** The viewport translation: centred across, the drawing's top at the gutter. */
  x: number;
  y: number;
  /** The canvas frame's height (border-box: the drawing, the gutters, the frame). */
  height: number;
}

export function fitToWidth(args: {
  /** The pane's inner width — the canvas frame minus its borders. */
  paneWidth: number;
  /** The drawing's bounds, in canvas pixels. */
  bounds: ContentBounds;
  minZoom: number;
  maxZoom: number;
  /** Clear space around the drawing, in screen pixels. */
  gutter: number;
  /** The frame's two borders, in screen pixels (added to the height). */
  frame: number;
  minHeight: number;
  /** Past this the canvas stops growing and the reader pans; absent, it grows. */
  maxHeight?: number | undefined;
}): ContentFit {
  const { paneWidth, bounds, gutter, frame } = args;
  const room = Math.max(0, paneWidth - 2 * gutter);
  const byWidth = bounds.width > 0 ? room / bounds.width : args.maxZoom;
  const zoom = Math.min(args.maxZoom, Math.max(args.minZoom, byWidth));
  const drawn = Math.ceil(bounds.height * zoom) + 2 * gutter + frame;
  const height = Math.max(args.minHeight, Math.min(args.maxHeight ?? Infinity, drawn));
  return {
    zoom,
    x: (paneWidth - bounds.width * zoom) / 2 - bounds.x * zoom,
    y: gutter - bounds.y * zoom,
    height,
  };
}
