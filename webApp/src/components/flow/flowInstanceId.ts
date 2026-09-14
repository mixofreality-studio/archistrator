/**
 * A React Flow instance's own `id`, from React's `useId()`.
 *
 * xyflow builds DOM ids from its instance id — the background `pattern-<id>`,
 * `react-flow__node-desc-<id>`, `react-flow__edge-desc-<id>`,
 * `react-flow__aria-live-<id>` and the edge markers — and every instance
 * defaults to `1`. Two canvases on a page (the detail pane and the focus view
 * over it, or two diagrams side by side) therefore duplicated all of them, and
 * `url(#pattern-1)` could paint one canvas with another's pattern (designer
 * check on renderers S1, polish 3). `useId()` is unique per mounted component
 * but carries punctuation (`:r1:`, `«r1»`, `_r_1_` by React version) that is
 * awkward inside `url(#…)` and a CSS selector, so it is reduced to [A-Za-z0-9_-].
 */
export function flowInstanceId(reactId: string): string {
  const safe = reactId.replace(/[^A-Za-z0-9_-]/g, '');
  return `rf-${safe.length > 0 ? safe : '0'}`;
}
