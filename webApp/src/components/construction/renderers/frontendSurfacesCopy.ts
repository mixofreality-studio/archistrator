/**
 * The SPA's no-surfaces copy (designer §5.5), naming the client (designer check
 * on renderers S1, polish 8). Its own module so the renderer file exports only
 * components, and so the sentence is tested without a renderer.
 */
export const NO_SURFACES_LABEL = 'NO SURFACES RECORDED';

/** Without a component id the renderer still says it, of "This client". */
export function noSurfacesSentence(componentId: string | undefined): string {
  const who = componentId !== undefined && componentId.length > 0 ? componentId : 'This client';
  return `${who}'s UI design records no surfaces, so there is nothing to preview. Surfaces are recorded with the UI design; the console does not guess routes.`;
}
