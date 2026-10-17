/**
 * The SPA's no-surfaces copy (designer §5.5), naming the client (designer check
 * on renderers S1, polish 8). Its own module so the renderer file exports only
 * components, and so the sentence is tested without a renderer.
 */
export const NO_SURFACES_LABEL = 'NO SCREENS TO PREVIEW YET';

/** Without a component id the renderer still says it, of "This client". */
export function noSurfacesSentence(componentId: string | undefined): string {
  const who = componentId !== undefined && componentId.length > 0 ? componentId : 'This client';
  return `${who}'s UI design lists no screens (surfaces), so there is nothing to preview here. Once the UI design names its screens, each one opens here as a live preview of the built app.`;
}
