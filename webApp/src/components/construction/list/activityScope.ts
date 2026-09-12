/**
 * The LIST lens's NAVIGABILITY rules (Stage B Task 11): scope chips, the kind
 * and layer filters, search-and-reveal, tier-1 sort, and "expand to current
 * phase". (The "Observed only" evidence toggle is NOT a filter — it rewrites the
 * rows' evidence before the tree is built; see observedOnly.ts.)
 *
 * There is no DataGrid backing this tree (`@mui/x-tree-view` is the free
 * community package — see Task 2's brief), so none of sort/filter/virtualize
 * comes for free. Every rule below is therefore explicit, and every one of
 * them is PURE: no React, no DOM, no apiRef. ActivityTreeView.tsx calls this
 * module and owns only the imperative apiRef choreography (expansion,
 * scrolling) that a pure function cannot do. It is a plain `.ts` sibling for
 * the same reason activityTree.ts and activityRowPresentation.ts already are:
 * Node's native type-stripping test runner cannot load a `.tsx` module at all.
 *
 * WHY SCOPE/KIND/LAYER FILTER AT TIER 1 ONLY
 * ------------------------------------------
 * An activity either shows with its full, untouched phase/task subtree, or it
 * does not show at all. None of these filters reaches INTO a subtree and
 * prunes individual phases or tasks — the tree's derivation (activityTree.ts)
 * is the single source for what an activity's subtree contains, and a filter
 * that trimmed it would be a second, competing opinion about the same data.
 *
 * WHY SEARCH IS DIFFERENT
 * ------------------------
 * Search is the one axis that legitimately reaches tier 3: an operator who
 * knows a task key or a generated task label (`codeReview` / "Code Review")
 * is looking for ONE row inside a specific activity's subtree, not for the
 * activity itself. So an activity that matches only through a descendant task
 * still passes the search filter (`activityPassesSearch`), and the caller
 * (ActivityTreeView) is handed back exactly which task ids matched
 * (`matchingTaskIds`) so it can reveal and highlight them without touching any
 * other activity's subtree. The brief only names three fields for the
 * ACTIVITY itself (id / title / componentId); tasks have no componentId, so
 * their two analogous fields (the task key and its generated label) are what
 * "a tier-3 match" is read against here — a judgment call filling a real gap
 * in the brief's three-field list, recorded here rather than left implicit.
 *
 * WHY THERE IS NO "HIDE SYNTHESIZED" FILTER ANY MORE
 * ----------------------------------------------------
 * It used to REMOVE every activity whose worst provenance was reconstructed —
 * 23 of 29 rows. But the committed activity list decides what exists (spec R6);
 * evidence only decides what is known. Its replacement, "Observed only", keeps
 * every row and strips the untrusted evidence instead (observedOnly.ts), so a
 * backfilled activity reads not started rather than disappearing (designer P1-11).
 */
import type { ActivityBuildStatusRow } from '../../../contracts/types';
import { provenanceGradeOf, worstOriginOf, type ProvenanceOrigin } from '../provenanceAxis.ts';
import type { ScopeId, SortId, ToolbarState } from '../lens/useLensSelection.ts';
import { activityRowState, currentStageMarker } from './activityRowPresentation.ts';
import type { ActivityNode, TaskNode } from './activityTree.ts';

// ---------------------------------------------------------------------------
// Scope chips
// ---------------------------------------------------------------------------

/**
 * One scope chip's predicate over a tier-1 activity. `critical`/`near` reuse
 * the existing tracker filter bar's own vocabulary (NearCriticalFloat.tsx:
 * on the critical path, or float <= 5 and NOT on it) rather than inventing a
 * second near-critical threshold for the same project.
 */
export function scopePredicate(scope: ScopeId, node: ActivityNode): boolean {
  switch (scope) {
    case 'all':
      return true;
    case 'critical':
      return node.onCriticalPath === true;
    case 'near':
      return node.onCriticalPath !== true && node.float !== undefined && node.float <= 5;
    case 'awaitingMe':
      return activityRowState(node.row) === 'awaitingHuman';
    case 'inFlight':
      return activityRowState(node.row) === 'running';
    case 'hasRetries':
      return node.retryCount > 0;
    case 'reconstructed':
      return provenanceGradeOf(worstOriginOf(node)) === 'reconstructed';
    case 'unknown':
      return provenanceGradeOf(worstOriginOf(node)) === 'unknown';
  }
}

// ---------------------------------------------------------------------------
// Kind / layer
// ---------------------------------------------------------------------------

/** `'all'` passes everything; a live facet value matches `node.kind` exactly. */
export function matchesKind(node: ActivityNode, kind: string): boolean {
  return kind === 'all' || node.kind === kind;
}

/** `'all'` passes everything; a live facet value matches `node.layer` exactly.
 *  `layerBand` is not filtered on here — Stage D's layer-stack projection is
 *  the surface that reads it — but it travels on the same node for that use. */
export function matchesLayer(node: ActivityNode, layer: string): boolean {
  return layer === 'all' || node.layer === layer;
}

// ---------------------------------------------------------------------------
// Search
// ---------------------------------------------------------------------------

/** Case-insensitive, whitespace-trimmed — the same normalization on both the
 *  needle and every haystack field, computed once per call. */
function normalizeSearch(query: string): string {
  return query.trim().toLowerCase();
}

function includesQuery(field: string, query: string): boolean {
  return field.toLowerCase().includes(query);
}

/** The activity's OWN three searchable fields — exactly the brief's list. */
function activityOwnFieldsMatch(node: ActivityNode, query: string): boolean {
  return (
    includesQuery(node.activityId, query) ||
    includesQuery(node.label, query) ||
    (node.componentId !== undefined && includesQuery(node.componentId, query))
  );
}

/** A task's analogous fields: its generated key, its profile label, and the
 *  book's name for it — so "code review" still finds a test plan's gate after
 *  the profile renamed it "Scenario Review". */
function taskFieldsMatch(task: TaskNode, query: string): boolean {
  return (
    includesQuery(task.task, query) ||
    includesQuery(task.label, query) ||
    includesQuery(task.bookLabel, query)
  );
}

/**
 * Every tier-3 task nodeId under `node` whose own fields match `query`.
 *
 * Empty for an empty query — search reveals nothing when there is nothing to
 * search for, rather than "matching" every task in the tree.
 */
export function matchingTaskIds(node: ActivityNode, query: string): string[] {
  const q = normalizeSearch(query);
  if (q === '') return [];
  const ids: string[] = [];
  for (const phase of node.phases) {
    for (const task of phase.tasks) {
      if (taskFieldsMatch(task, q)) ids.push(task.nodeId);
    }
  }
  return ids;
}

/**
 * Whether `node` should remain in the LIST under `query` — by its own
 * identity, or because at least one of its tasks matched (in which case the
 * caller reveals that task via `matchingTaskIds`, never by filtering the
 * activity's subtree down to just the match).
 */
export function activityPassesSearch(node: ActivityNode, query: string): boolean {
  const q = normalizeSearch(query);
  if (q === '') return true;
  return activityOwnFieldsMatch(node, q) || matchingTaskIds(node, query).length > 0;
}

// ---------------------------------------------------------------------------
// The search-reveal provenance guarantee (carried forward from Task 7)
// ---------------------------------------------------------------------------

/**
 * Whether a search-revealed task row must carry its OWN inline provenance
 * mark, independent of whether its ancestors happen to still be on screen.
 *
 * The tier-1/tier-2 group badge (`ProvenanceGroupStamp`) is the surface's
 * normal defence against a reconstructed row reading as fact — but it is an
 * argument from CONTEXT (the header is always visible above the task), and
 * search is the one feature that can reveal and focus a tier-3 row on its own
 * initiative. So the SAME rule the group badge already applies —
 * `grade === 'reconstructed'`, never `unknown` too, which is already
 * self-evidently quiet and asserts nothing a reader could mistake for fact —
 * is applied here to the matched row itself, pinned by a test so a future
 * edit cannot silently relax it back to "the ancestors are probably visible".
 */
export function needsInlineProvenanceMark(
  isSearchMatch: boolean,
  origin: ProvenanceOrigin
): boolean {
  return isSearchMatch && provenanceGradeOf(origin) === 'reconstructed';
}

// ---------------------------------------------------------------------------
// Sort — TIER 1 ONLY
// ---------------------------------------------------------------------------

/**
 * `network` preserves input order (the committed project network's own
 * order, which is what the caller feeds in) — a stable copy, never a re-sort
 * back to "as given" that could silently reorder on a tie. `floatAsc` sorts by
 * float ascending; an unknown float (a row the committed network does not
 * carry) sorts LAST, never as if it were `0`,
 * because a fabricated zero float would read as "no slack at all" — the
 * loudest possible lie this surface can tell about an activity it knows
 * nothing about. Ties keep their relative (network) order — `Array.sort` is
 * stable per the ECMAScript spec, so no explicit tie-break index is needed.
 */
export function sortActivities(nodes: readonly ActivityNode[], sort: SortId): ActivityNode[] {
  if (sort === 'network') return [...nodes];
  return [...nodes].sort((a, b) => {
    const fa = a.float ?? Number.POSITIVE_INFINITY;
    const fb = b.float ?? Number.POSITIVE_INFINITY;
    return fa - fb;
  });
}

// ---------------------------------------------------------------------------
// The combined pipeline
// ---------------------------------------------------------------------------

export type ToolbarFilters = Pick<ToolbarState, 'scope' | 'kind' | 'layer' | 'search' | 'sort'>;

/**
 * Filter (scope AND kind AND layer AND search), then
 * sort. One entry point so the caller (ConstructionConsole.tsx) never has to
 * remember the combination order, and so a test can pin the whole pipeline's
 * behaviour rather than each rule only in isolation.
 */
export function applyToolbarToActivities(
  nodes: readonly ActivityNode[],
  toolbar: ToolbarFilters
): ActivityNode[] {
  const filtered = nodes.filter(
    (n) =>
      scopePredicate(toolbar.scope, n) &&
      matchesKind(n, toolbar.kind) &&
      matchesLayer(n, toolbar.layer) &&
      activityPassesSearch(n, toolbar.search)
  );
  return sortActivities(filtered, toolbar.sort);
}

// ---------------------------------------------------------------------------
// "Expand to current phase"
// ---------------------------------------------------------------------------

/** In-construction (actively running) or in-review (blocked on a human gate
 *  decision) — both are "the current phase is happening right now", which is
 *  the whole point of the button. An activity with no evidence, or one that
 *  already integrated, has no "current phase" left to jump to. */
export function isActivelyInFlight(status: ActivityBuildStatusRow | undefined): boolean {
  return status === 'in-construction' || status === 'in-review';
}

/**
 * The tree-item ids "Expand to current phase" opens — deliberately just the
 * in-flight ACTIVITIES' own ids (revealing their phase rows), never every
 * activity: that is the "expand all" trap the whole feature exists to avoid.
 * Today's live project has 1-3 activities in flight at once, so clicking the
 * button opens 1-3 rows' worth of phase children, not 528.
 */
export function currentPhaseExpansionIds(nodes: readonly ActivityNode[]): string[] {
  return nodes.filter((n) => isActivelyInFlight(n.row.status)).map((n) => n.nodeId);
}

/** Re-exported so a caller that already has the node (not just its row) can
 *  reuse the SAME "what is this activity's current phase" reading the tree
 *  view's own AbsentStage marker uses, rather than a second implementation. */
export { currentStageMarker };
