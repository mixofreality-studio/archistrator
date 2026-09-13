/**
 * Whether a lifecycle segment's short code FITS the segment it labels
 * (designer re-check, the P1 blocker: 17 of 29 lanes showed "R…", "T.CON", "I…").
 *
 * MEASURED, never counted: the renderer measures the code's rendered text width
 * and the segment's width, and a code that does not fit renders NO text — never
 * an ellipsis, never a clipped fragment. Its row keeps its height either way, so
 * a lane never jumps as codes come and go. Letter-spacing is 0, so an engine
 * lane's five codes fit where they can.
 *
 * Pure — pinned by segmentCode.test.ts.
 */

/** The code's letter-spacing — 0, so the measured width is the text's own. */
export const SEGMENT_CODE_LETTER_SPACING = 0;

export function segmentCodeFits(textWidthPx: number, segmentWidthPx: number): boolean {
  return (
    Number.isFinite(textWidthPx) &&
    Number.isFinite(segmentWidthPx) &&
    textWidthPx > 0 &&
    textWidthPx <= segmentWidthPx
  );
}
