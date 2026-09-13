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

// ---------------------------------------------------------------------------
// Re-measuring after a web font swaps in
// ---------------------------------------------------------------------------

/**
 * The slice of `document.fonts` (a `FontFaceSet`) this module needs — narrowed
 * so the wiring below can be pinned by node:test against a plain fake, with no
 * DOM at all.
 */
export interface FontFaceSetLike {
  readonly ready: Promise<unknown>;
  addEventListener(type: 'loadingdone', listener: () => void): void;
  removeEventListener(type: 'loadingdone', listener: () => void): void;
}

/**
 * Re-run `onChange` when a web font finishing load can have invalidated an
 * already-taken measurement (round 3, designer re-check): a code measured
 * against the fallback font can fit, then clip once the web font swaps in and
 * draws wider glyphs. A `ResizeObserver` never fires for this — the segment's
 * own box does not resize, only the glyphs inside it do — so two OTHER signals
 * cover it:
 *
 *  - `fonts.ready` — the swap that finishes after the first measurement was
 *    already taken (fires once, or immediately if it had already resolved);
 *  - `loadingdone` — every later swap, for as long as the caller stays
 *    subscribed (a font loaded on demand, a second face, a retry).
 *
 * Returns the unsubscribe function a `useEffect` cleanup runs on unmount; safe
 * to call more than once.
 */
export function subscribeToFontMetricChanges(
  fonts: FontFaceSetLike,
  onChange: () => void
): () => void {
  let live = true;
  void fonts.ready
    .then(() => {
      if (live) onChange();
    })
    .catch(() => {
      // The spec promises `ready` never rejects; if a fake or a future runtime
      // ever does, skip the re-measure rather than throw — `loadingdone` (and
      // any future ResizeObserver firing) remain as fallbacks.
    });
  const onLoadingDone = (): void => {
    onChange();
  };
  fonts.addEventListener('loadingdone', onLoadingDone);
  return (): void => {
    live = false;
    fonts.removeEventListener('loadingdone', onLoadingDone);
  };
}
