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

/** One neighbour of the focal component, with the operations the edges carry. */
export interface NeighbourRow {
  id: string;
  name: string;
  layer: string;
  /** The relationship labels (op names) between the two, in the architecture's order. */
  operations: string[];
}

/**
 * The pane's Component tab as TEXT (designer recheck on S2): who calls this
 * component and whom it calls, one row per neighbour, utilities left out (they
 * are the one line). Drawn in the pane, the same relationships fit to 0.31
 * scale; the diagram draws in the focus view only.
 */
export function neighbourRowsFor(
  view: C4View,
  focusId: string
): { callers: NeighbourRow[]; callees: NeighbourRow[] } {
  const v = withoutUtilities(view, focusId);
  const rows = (pick: (from: string, to: string) => string | undefined): NeighbourRow[] => {
    const ops = new Map<string, string[]>();
    for (const r of v.relationships) {
      const other = pick(r.from, r.to);
      if (other === undefined || other === focusId) continue;
      const list = ops.get(other) ?? [];
      if (r.label.length > 0 && !list.includes(r.label)) list.push(r.label);
      ops.set(other, list);
    }
    return v.components
      .filter((c) => ops.has(c.id))
      .map((c) => ({ id: c.id, name: c.name, layer: c.layer, operations: ops.get(c.id) ?? [] }));
  };
  return {
    callers: rows((from, to) => (to === focusId ? from : undefined)),
    callees: rows((from, to) => (from === focusId ? to : undefined)),
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
