/**
 * Finding the console's scroller, and the dead space below its content.
 *
 * Three places in the Construction console walk up to the scrolling ancestor,
 * and two of them also need to know how much of that scroller's visible height
 * is NOT room for content — the detail pane caps its height against it
 * (lensGeometry.lensGeometryVars) and the graph canvas sizes itself against it
 * (graphViewport.canvasHeightPx). They were three hand-copied loops with a
 * comment in one of them saying it found the scroller "the way the lens shell
 * finds it", which is exactly how they drifted.
 *
 * WHY THE PADDING IS SUMMED, NOT READ OFF THE SCROLLER
 * ---------------------------------------------------
 * Both callers used to read `getComputedStyle(scroller).paddingBottom`, which
 * was right only while the scroller and the padded box were the same element —
 * the console's own `pb: 3` column WAS the scroller. Once ExperienceChrome took
 * ownership of one shared scroller for the whole page (Task 8b) that stopped
 * being true: the scroller has no padding of its own and the 24px sits five
 * levels down, on a box these walks pass straight through. Both callers silently
 * started reading 0, and the graph canvas grew by the difference — it reserved
 * the 16px it wanted below itself but not the 24px the wrapper already took, so
 * the scroller overflowed by 8px and the GRAPH lens stopped fitting above the
 * fold.
 *
 * What either caller actually wants is the TOTAL dead space between the bottom
 * of the content and the scroller's bottom edge. That is the sum of the bottom
 * padding of every box between the two, including the scroller's own: each is a
 * distinct band of space, stacked, so summing them counts each exactly once. It
 * is also stable under further layout changes — it does not care WHICH box
 * carries the padding, which is the property the old single read lacked.
 */

/** The nearest scrolling ancestor of `el`, or `null` if nothing above it scrolls. */
export function scrollerOf(el: HTMLElement): HTMLElement | null {
  let node: HTMLElement | null = el.parentElement;
  while (node !== null && !/(auto|scroll)/.test(getComputedStyle(node).overflowY)) {
    node = node.parentElement;
  }
  return node;
}

export interface ScrollerBox {
  /** The nearest scrolling ancestor, or `null`. */
  scroller: HTMLElement | null;
  /**
   * Total bottom padding between `el` and the scroller's bottom edge, in px —
   * every box in between plus the scroller itself. `0` when nothing scrolls
   * above `el`, since there is then no box to measure against.
   */
  padBottom: number;
}

/** {@link scrollerOf} plus the dead space below the content — see the file header. */
export function scrollerBoxOf(el: HTMLElement): ScrollerBox {
  let node: HTMLElement | null = el.parentElement;
  let padBottom = 0;
  while (node !== null) {
    const style = getComputedStyle(node);
    padBottom += Number.parseFloat(style.paddingBottom) || 0;
    if (/(auto|scroll)/.test(style.overflowY)) return { scroller: node, padBottom };
    node = node.parentElement;
  }
  return { scroller: null, padBottom: 0 };
}
