/**
 * The contract's Component view without its utilities — pure (designer check on
 * renderers S1, polish 2). A utility is reachable from every layer, so as nodes
 * it says nothing about the focal component and leads nowhere on click; the view
 * names the ones this component reaches in one line instead.
 */
import type { C4View } from '../../contracts/adapters';

function isUtility(layer: string): boolean {
  return layer === 'utility';
}

/** The view minus every utility (unless the focus is itself one) and its edges. */
export function withoutUtilities(view: C4View, focusId: string): C4View {
  const drop = new Set(
    view.components.filter((c) => isUtility(c.layer) && c.id !== focusId).map((c) => c.id)
  );
  if (drop.size === 0) return view;
  return {
    ...view,
    components: view.components.filter((c) => !drop.has(c.id)),
    relationships: view.relationships.filter((r) => !drop.has(r.from) && !drop.has(r.to)),
  };
}

/** The utilities the focal component's relationships reach, by name, in the architecture's order. */
export function utilityNeighbours(view: C4View, focusId: string): string[] {
  const touched = new Set<string>();
  for (const r of view.relationships) {
    if (r.from === focusId) touched.add(r.to);
    if (r.to === focusId) touched.add(r.from);
  }
  return view.components
    .filter((c) => c.id !== focusId && isUtility(c.layer) && touched.has(c.id))
    .map((c) => c.name);
}
