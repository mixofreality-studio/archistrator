/**
 * The lens shell's MEASURED geometry — the one place the sticky toolbar's height,
 * the scroller's visible height and the content row's offset are named, so the
 * detail pane pins below the toolbar as it actually is rather than where a
 * constant guessed it would be, and is never taller than the room it has.
 *
 * Why measured (designer P0-1): the toolbar wraps. At 1600px it is ~51px tall; at
 * 1280/1366 its controls wrap onto a second line and it is ~86px. A pane pinned at
 * a constant 76px slid under the toolbar at exactly the widths people use, and a
 * height cap built on 100vh ignored the app chrome above the scroller, so the
 * action bar ("Run this task") ran off the bottom of the screen.
 *
 * WHY THREE MEASUREMENTS, NOT TWO
 * -------------------------------
 * The pane is `position: sticky`. Once the page has scrolled it pins at
 * `toolbar + 8px`; AT REST it sits in flow, where the content row starts — below
 * the page header and the toolbar. A cap computed only for the pinned position
 * leaves the pane taller than the room it has at rest, so its action bar sits
 * below the fold until the operator scrolls. The pane's real top is therefore the
 * LARGER of the row's offset and the pinned offset, and the cap is the scroller's
 * visible height less that and the bottom gap. CSS `max()` does that arithmetic
 * with no JS in the layout path; the shell only keeps the three inputs current.
 *
 * ConstructionShell measures them (ResizeObserver + scroll) and writes them as CSS
 * custom properties on the DETAIL SLOT's own wrapper — never on the shell root. The
 * properties inherit, so a write on the root invalidated the style of every element
 * under it, the whole ~4k-element tree, on every scroll event (~310ms over 60 wheel
 * steps against ~5ms; fix-A review I4). On the pane's wrapper only the pane
 * recalculates. The pane (detailPaneState.WIDE_PANE_SX) reads them through the
 * cascade. No React state is involved, so neither a resize nor a scroll re-renders
 * the tree, and a value that did not change is not written at all (varsToWrite).
 *
 * Pure (no DOM, no React) so node:test can pin the arithmetic.
 */

export const LENS_TOOLBAR_H_VAR = '--lens-toolbar-h';
export const LENS_SCROLL_H_VAR = '--lens-scroll-h';
/** The content row's top edge, measured from the scroller's visible top edge —
 *  where the pane sits in flow. Goes negative once the row scrolls past. */
export const LENS_ROW_TOP_VAR = '--lens-row-top';

/** Clearance between the stuck toolbar's bottom edge and the pinned pane. */
export const PANE_TOOLBAR_GAP_PX = 8;
/** Bottom breathing room so the pane never touches the scroller's edge. */
export const PANE_BOTTOM_GAP_PX = 16;

/**
 * The custom properties the shell publishes. The toolbar height and the row
 * offset round UP (a fractional pixel must never let the pane overlap the toolbar
 * or claim room it does not have) and the scroller height rounds DOWN (a
 * fractional pixel must never let the action bar spill past it).
 *
 * The row offset is clamped to the pinned offset (toolbar + gap): once the row
 * has scrolled past, the pane is pinned and the cap reads the pinned offset
 * anyway, so an unclamped value only changed on every scroll step for nothing —
 * and a changed value is a write. Clamped, it settles and stops being written.
 */
export function lensGeometryVars(
  toolbarHeightPx: number,
  scrollHeightPx: number,
  rowTopPx: number,
  /**
   * The scroller's own bottom padding. Content ends ABOVE it — the canvas
   * subtracts it too (graphViewport.canvasHeightPx) — so a pane capped against
   * the full client height overflowed the scroller by (padding − the 16px gap):
   * 7px at 1280 and 1366 (designer re-check 2). Absent: no padding.
   */
  scrollPadBottomPx = 0
): Record<string, string> {
  const toolbarPx = Math.ceil(toolbarHeightPx);
  return {
    [LENS_TOOLBAR_H_VAR]: `${String(toolbarPx)}px`,
    [LENS_SCROLL_H_VAR]: `${String(Math.floor(scrollHeightPx - scrollPadBottomPx))}px`,
    [LENS_ROW_TOP_VAR]: `${String(Math.max(Math.ceil(rowTopPx), toolbarPx + PANE_TOOLBAR_GAP_PX))}px`,
  };
}

/** The entries of `next` whose value differs from what was last written. */
export function varsToWrite(
  last: Readonly<Record<string, string>>,
  next: Readonly<Record<string, string>>
): [string, string][] {
  return Object.entries(next).filter(([name, value]) => last[name] !== value);
}

// The fallbacks (a zero toolbar and row offset, the full viewport) only ever apply
// before the shell's first measurement, which it takes in a layout effect —
// before first paint — so they are a default nobody sees.
const PINNED_OFFSET = `var(${LENS_TOOLBAR_H_VAR}, 0px) + ${String(PANE_TOOLBAR_GAP_PX)}px`;

/** Where the pane pins once the page has scrolled: just below the measured toolbar. */
export const PANE_STICKY_TOP = `calc(${PINNED_OFFSET})`;

/** The room the pane actually has, at rest and pinned alike (see the file comment). */
export const PANE_MAX_HEIGHT = `calc(var(${LENS_SCROLL_H_VAR}, 100vh) - max(var(${LENS_ROW_TOP_VAR}, 0px), ${PINNED_OFFSET}) - ${String(PANE_BOTTOM_GAP_PX)}px)`;

/**
 * The toolbar is STUCK — and takes its light shadow — once the scroller has moved
 * and the toolbar sits at the scroller's top edge. At rest it sits below the page
 * header, so it is not stuck even though sticky is armed.
 */
export function isToolbarStuck(input: {
  toolbarTop: number;
  scrollerTop: number;
  scrollTop: number;
}): boolean {
  return input.scrollTop > 0 && input.toolbarTop - input.scrollerTop <= 1;
}

/**
 * Where to scroll so a deep-linked row sits at the CENTRE of the visible band —
 * below the stuck toolbar, above the window's bottom edge (designer final N1).
 *
 * Measured, it used to land at 62-81% of the band at 1280/1366: the scroller was
 * already at its maximum, because the list ENDS a little below the linked row and
 * there was nothing left to scroll. So when the centring scrollTop is past the
 * current maximum, the difference is returned as a RUNWAY — blank space the list
 * adds below itself — rather than silently clamping the row to the bottom.
 *
 * All inputs in px. `rowTop` is in the scroller's CONTENT coordinates; `bandTop`
 * and `bandBottom` in its VIEWPORT coordinates; `contentHeight` is the scroll
 * extent with no runway. Pure, so node:test pins it.
 */
export function centeredScrollFor(input: {
  rowTop: number;
  rowHeight: number;
  bandTop: number;
  bandBottom: number;
  contentHeight: number;
  clientHeight: number;
}): { scrollTop: number; runwayPx: number } {
  const rowMid = input.rowTop + input.rowHeight / 2;
  const bandMid = (input.bandTop + input.bandBottom) / 2;
  const scrollTop = Math.max(0, Math.round(rowMid - bandMid));
  const max = Math.max(0, input.contentHeight - input.clientHeight);
  return { scrollTop, runwayPx: Math.max(0, scrollTop - max) };
}
