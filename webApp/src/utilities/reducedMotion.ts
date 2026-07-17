/**
 * OS-level "reduce motion" preference. Read at animation time (not cached) so a
 * live settings change takes effect immediately. Callers gate their animation
 * durations: instant jump (0) when reduction is requested.
 */
export function prefersReducedMotion(): boolean {
  return window.matchMedia('(prefers-reduced-motion: reduce)').matches;
}
