/**
 * The preview's navigation guard: a preview never opens a second browsing
 * context. Nested previews are off inside a preview (design-renderer-data.md
 * §2′.5 item 5), and a frame is only one way to nest one:
 *
 *   - a frame is refused by the preview document's CSP (`frame-src 'none'`,
 *     preview.html);
 *   - a NEW TAB or window is refused here. The construction pane's "Open in a
 *     new tab" link for a built surface (FrontendArtifactView) would boot a
 *     second copy of the app from the preview's origin, and any other
 *     `target="_blank"` link (a CI run link, say) would leave the fixture world.
 *     `window.open` is refused by the network guard (networkGuard.ts).
 *
 * In-app links are untouched: the router's own links carry no target and are
 * handled on the memory history. Only an activation that would open another
 * context is refused: a non-self target, or a modifier-click or middle-click
 * that the browser would turn into a new tab. Each refusal is reported, so it
 * shows on the preview's alarm.
 *
 * The decision is a pure function (opensNewContext), unit-tested under node;
 * installNavigationGuard is the thin capture-phase listener around it.
 */

export interface LinkActivation {
  /** The anchor's `target` attribute ('' when absent). */
  readonly target: string;
  /** MouseEvent.button: 0 primary, 1 middle. */
  readonly button: number;
  readonly ctrlKey: boolean;
  readonly metaKey: boolean;
  readonly shiftKey: boolean;
}

/** True when activating the link would open another tab or window. */
export function opensNewContext(a: LinkActivation): boolean {
  const target = a.target.trim().toLowerCase();
  if (target !== '' && target !== '_self') return true;
  if (a.button === 1) return true;
  return a.ctrlKey || a.metaKey || a.shiftKey;
}

/**
 * Refuses, in the capture phase (before any app handler), every link activation
 * that would open another browsing context. `onBlocked` hears the link's href.
 */
export function installNavigationGuard(doc: Document, onBlocked: (href: string) => void): void {
  const guard = (event: MouseEvent): void => {
    const origin = event.target instanceof Element ? event.target : null;
    const anchor = origin?.closest('a[href]');
    if (!(anchor instanceof HTMLAnchorElement)) return;
    // Read each field off the event: its button and modifier flags are prototype
    // getters, which a spread would silently drop.
    const activation: LinkActivation = {
      target: anchor.target,
      button: event.button,
      ctrlKey: event.ctrlKey,
      metaKey: event.metaKey,
      shiftKey: event.shiftKey,
    };
    if (!opensNewContext(activation)) return;
    event.preventDefault();
    event.stopImmediatePropagation();
    onBlocked(anchor.href);
  };
  doc.addEventListener('click', guard, { capture: true });
  doc.addEventListener('auxclick', guard, { capture: true });
}
