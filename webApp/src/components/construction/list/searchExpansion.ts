/**
 * Which tree rows a SEARCH opened, kept apart from the rows the OPERATOR opened
 * (designer P1-10).
 *
 * The search reveal expands every matched activity and its matched phase so the
 * match can be focused. Before this, those rows stayed open after the query was
 * cleared — 220 rows where the operator had left 29 — because the tree kept one
 * undifferentiated list of expanded ids and could not tell "I opened this" from
 * "the search opened this for me".
 *
 * The rule:
 *   - a row the reveal opens that was not already open is SEARCH-owned;
 *   - a new query first closes what the previous query opened, then opens what
 *     it needs; clearing the query closes them all;
 *   - anything the operator touches becomes theirs: a row they close stays
 *     closed, a row they open (or open via "Expand to current phase") survives
 *     the clear, and a row that was already open before the search is never
 *     closed by it.
 *
 * Pure (no React) so node:test can pin it; ActivityTreeView owns the state.
 */

export interface TreeExpansion {
  /** Every expanded item id — what the tree renders. Every function here returns
   *  fresh arrays and never mutates its input. */
  expanded: string[];
  /** The subset a search reveal opened and the operator has not touched since. */
  searchOpened: string[];
}

export const NO_EXPANSION: TreeExpansion = { expanded: [], searchOpened: [] };

/**
 * Apply a search's reveal: close the previous query's reveal, then open `reveal`
 * (empty for a cleared query). Ids already open by the operator stay theirs.
 */
export function revealForQuery(state: TreeExpansion, reveal: readonly string[]): TreeExpansion {
  const stale = new Set(state.searchOpened);
  const kept = state.expanded.filter((id) => !stale.has(id));
  const keptSet = new Set(kept);
  const opened = [...new Set(reveal)].filter((id) => !keptSet.has(id));
  return { expanded: [...kept, ...opened], searchOpened: opened };
}

/**
 * The operator changed expansion by hand (chevron or keyboard): `next` is the
 * tree's whole new expanded set. A search-opened row they closed leaves the
 * search's list for good; anything they opened was never in it.
 */
export function applyOperatorExpansion(
  state: TreeExpansion,
  next: readonly string[]
): TreeExpansion {
  const nextSet = new Set(next);
  return { expanded: [...next], searchOpened: state.searchOpened.filter((id) => nextSet.has(id)) };
}

/**
 * What a DEEP LINK must open, and which row it must bring into view, on load
 * (designer re-check N1). A link naming a phase (`p`) or a task (`k`) used to
 * select a row inside a collapsed activity: the pane opened, and the row it
 * described sat hidden under a closed chevron. The link's ancestors open — the
 * activity for a phase, the activity and its phase for a task — and the selected
 * row is the scroll target. An activity-only link needs nothing opened.
 */
export function deepLinkReveal(selection: {
  activityId?: string | undefined;
  lifecyclePhase?: string | undefined;
  task?: string | undefined;
}): { expand: string[]; target: string | null } {
  const { activityId, lifecyclePhase, task } = selection;
  if (activityId === undefined || lifecyclePhase === undefined) return { expand: [], target: null };
  const phaseId = `${activityId}::${lifecyclePhase}`;
  if (task === undefined) return { expand: [activityId], target: phaseId };
  return { expand: [activityId, phaseId], target: `${phaseId}::${task}` };
}

/** One URL selection as a key: a deep link is identified by its a/p/k alone. */
export function deepLinkKey(selection: {
  activityId?: string | undefined;
  lifecyclePhase?: string | undefined;
  task?: string | undefined;
}): string {
  return [selection.activityId ?? '', selection.lifecyclePhase ?? '', selection.task ?? ''].join(
    '|'
  );
}

/**
 * The last URL selection a mounted LIST tree has already shown (fix-C review N1).
 *
 * A lens switch UNMOUNTS the tree, so its own state cannot remember that a link
 * was revealed: coming back re-opened the link's ancestors every time, undoing
 * whatever the operator had collapsed since. The reveal now runs only for a
 * selection no tree has shown yet — i.e. only when the URL selection changed.
 * Module memory, the same pattern the design-experience diagram views use to
 * survive their remounts. Written from an effect once the reveal is decided, so
 * StrictMode's double render and double effect are both idempotent.
 */
const shownLink: { key: string | undefined } = { key: undefined };

export function linkAlreadyShown(key: string): boolean {
  return shownLink.key === key;
}

export function rememberShownLink(key: string): void {
  shownLink.key = key;
}

/** Tests only: a fresh console. */
export function forgetShownLink(): void {
  shownLink.key = undefined;
}

/** An explicit operator action opened `ids` ("Expand to current phase"): they are
 *  the operator's now, including any a search had opened. */
export function openByOperator(state: TreeExpansion, ids: readonly string[]): TreeExpansion {
  const claimed = new Set(ids);
  return {
    expanded: [...new Set([...state.expanded, ...ids])],
    searchOpened: state.searchOpened.filter((id) => !claimed.has(id)),
  };
}
