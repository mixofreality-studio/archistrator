/**
 * The plan's LENS: which of the three lenses is showing (held in the URL) and the
 * shared toolbar state (held in a module-level store keyed by a content
 * signature).
 *
 * ── What this file used to be, and what stage 5 left ────────────────────────
 * It was the construction console's whole selection codec: the lens AND the
 * selected task attempt AND the artifact view, all in the URL —
 * `?lens=list&a=<activityId>&p=<phase>&k=<task>&n=<attempt>&av=&focus=&sc=` —
 * because the console polled every 1.5s while the pump cascaded and any remount
 * dropped component-held selection.
 *
 * Spec §7.4 deleted that console and its shared DetailPane. An activity is its
 * own full-screen route now (`/project/$id/activity/$activityId?task=&rev=`), so
 * every one of those params addresses nothing, and Task 13 deleted the codec that
 * read and wrote them (`parseLensSearch`, `serializeLensSearch`,
 * `artifactKeptFor`, `useLensSelection`, `LensState`, `ArtifactViewState`,
 * `ARTIFACT_VIEW_IDS`, `LegacyLensSearchParams`) together with its last consumer.
 *
 * Three things survive, and this is why each one is still here rather than moved:
 *
 *   - `validateLensSearch` — the plan route's `validateSearch`, and the
 *     now-redirecting construction route's. One line over
 *     `contracts/routePaths.planSearch`, so the two cannot disagree.
 *   - `LensSelection` — narrowed to the two members the TASKS lens actually
 *     reads. It is no longer a URL shape; it is the "which row is marked" value
 *     TasksLens and decisionFlow pass between them.
 *   - the TOOLBAR half (`SCOPE_IDS` … `useLensToolbar`) — the NetworkView
 *     signature-store pattern, which `list/activityScope.ts` and `PlanContainer`
 *     both need and which was never about the URL.
 */
import { useCallback, useState } from 'react';
// The explicit `.ts` is what lets `node --test` load this module directly
// (operationsGuard.ts does the same) — Node's resolver does not guess extensions.
import { LENS_IDS, planSearch } from '../../../contracts/routePaths.ts';

// ---------------------------------------------------------------------------
// Lens identity
// ---------------------------------------------------------------------------

/**
 * Re-exported, not redeclared: the plan route and the redirecting construction
 * route must agree on what a lens IS, and two copies of the array are how they
 * end up disagreeing. The one array lives in contracts/routePaths.ts, which
 * components may import and `routes/` is not.
 */
export { LENS_IDS };
export type LensId = (typeof LENS_IDS)[number];

/**
 * The one selectable thing. The URL stopped carrying it in stage 5 (the plan's
 * one search param is `lens`); it is now an in-memory value — "which row is
 * marked, and at which task" — that the TASKS lens passes down
 * (TasksLens.isSelected, decisionFlow).
 *
 * ── Task 13 tried to narrow this to `{ activityId, task }` and could not ────
 * The plan's teardown table says to keep only the two members `TasksLens.tsx`
 * and `decisionFlow.ts` ANNOTATE with. Measured: narrowing it puts tsc into 30
 * errors across FIVE kept modules, because the pure state helpers that take a
 * `LensSelection` read the other two —
 *
 *   detail/detailPaneState.ts   `.attempt` (:219, :298, :310, :341) and
 *                               `.lifecyclePhase` (:407, :461, :546, :548) —
 *                               and PlanList, PlanTile, planTiles, TasksLens,
 *                               activityRowPresentation and activityTree all
 *                               import from it;
 *   detail/bodies/taskBriefing.ts  `.lifecyclePhase` (:124, :169, :422);
 *   tasks/decisionFlow.ts          `.lifecyclePhase` (:348).
 *
 * So all four members STAY. Cutting the two would mean deleting live branches of
 * kept modules — a second, unbriefed deletion — which the plan forbids: a red
 * typecheck after a cut means the cut was wrong, not that something else should
 * go. Retiring `lifecyclePhase`/`attempt` is a real piece of work (those helpers
 * still address a task ATTEMPT, which is the DetailPane's shape) and belongs to
 * the stage-6 pass that retires `detailPaneState.ts` itself.
 */
export interface LensSelection {
  activityId?: string;
  lifecyclePhase?: string;
  task?: string;
  /** 1-based attempt index. Never `NaN` — a junk value is dropped, not coerced. */
  attempt?: number;
}

/**
 * The wire shape of the route's search params — `lens` and nothing else, as of
 * stage 5 (R8). `a`/`p`/`k`/`n` addressed a task attempt inside the DetailPane
 * and `av`/`focus`/`sc` a view of its artifact; that pane is gone, the plan's
 * only selection is its lens, and a param that addresses nothing has no business
 * in the address bar.
 */
export interface LensSearchParams {
  lens?: LensId;
}

/**
 * The route's `validateSearch`, for BOTH the plan route and the legacy
 * construction route that redirects into it (R8 — the redirect's
 * `planSearchFromLegacy` reads the `lens` this emits). A deep link validates instead
 * of throwing; an unknown lens falls back to `list`, and `lens` is ALWAYS
 * emitted, even for the default — validateSearch's output IS the address bar.
 *
 * The rule itself is `contracts/routePaths.planSearch`, not a copy: the two
 * routes must agree, and the containers construct the same object when they
 * navigate.
 */
export function validateLensSearch(search: Record<string, unknown>): LensSearchParams {
  return planSearch(search['lens']);
}

// ---------------------------------------------------------------------------
// Shared toolbar state — persisted by content signature (the NetworkView pattern)
// ---------------------------------------------------------------------------

/**
 * Scope chips, shared by every lens. `critical`/`near` carry the existing tracker
 * filter vocabulary forward unchanged; the rest are the rewrite's additions.
 * Each one's predicate lives in list/activityScope.ts (scopePredicate).
 */
export const SCOPE_IDS = [
  'all',
  'critical',
  'near',
  'awaitingMe',
  'inFlight',
  'hasRetries',
  'reconstructed',
  'unknown',
] as const;
export type ScopeId = (typeof SCOPE_IDS)[number];

/**
 * Sort applies to TIER 1 ONLY. Tiers 2 and 3 are a sequence (Figure A-1 order) —
 * sorting them is nonsense and is deliberately not offered.
 */
export const SORT_IDS = ['network', 'floatAsc'] as const;
export type SortId = (typeof SORT_IDS)[number];

/** `kind` and `layer` hold `'all'` or one live facet value from the dataset. */
export interface ToolbarState {
  search: string;
  scope: ScopeId;
  kind: string;
  layer: string;
  sort: SortId;
  /**
   * The "Observed only" evidence toggle (fix round B, designer P1-11). ON keeps
   * EVERY activity — the committed list decides what exists (spec R6) — and sets
   * aside every attempt not observed by the running system, so an activity known
   * only from reconstructed evidence reads as not started (list/observedOnly.ts).
   * It replaced "Hide synthesized", which removed 23 of 29 rows outright.
   */
  observedOnly: boolean;
}

export const DEFAULT_TOOLBAR: ToolbarState = {
  search: '',
  scope: 'all',
  kind: 'all',
  layer: 'all',
  sort: 'network',
  observedOnly: false,
};

/**
 * A content signature for the console's dataset: stable across a fresh-but-
 * identical poll envelope, changing only when the actual project or activity SET
 * changes. Deliberately keyed on the activity IDENTITIES and not on any per-row
 * status — a cascade completing an activity must not reset the operator's
 * filters, which is precisely the churn the 1.5s poll produces.
 *
 * (Directly after NetworkView.tsx's signatureOf, which this console already
 * needed once.)
 */
export function toolbarSignatureOf(projectId: string, activityIds: readonly string[]): string {
  const ids = [...activityIds].sort((a, b) => a.localeCompare(b));
  return `${projectId}:${String(ids.length)}:${ids.join(',')}`;
}

const toolbarStore = new Map<string, ToolbarState>();

export function loadToolbar(signature: string): ToolbarState {
  return toolbarStore.get(signature) ?? DEFAULT_TOOLBAR;
}

interface HeldToolbar {
  /** The content signature this toolbar state belongs to (see toolbarSignatureOf). */
  signature: string;
  state: ToolbarState;
}

export interface LensToolbarApi {
  toolbar: ToolbarState;
  setToolbar: (patch: Partial<ToolbarState>) => void;
}

/**
 * The shared toolbar's state, held across lens switches AND across a remount.
 * This is the whole point of a lens control over tabs: the search you typed and
 * the scope you chose describe the DATASET, not the lens, so they must not die
 * when you look at the same rows a different way.
 */
export function useLensToolbar(signature: string): LensToolbarApi {
  const [held, setHeld] = useState<HeldToolbar>(() => ({
    signature,
    state: loadToolbar(signature),
  }));

  // Adjust state during render when the dataset identity changes — the
  // React-sanctioned alternative to a setState-in-effect (the previous signature
  // is tracked in state itself, not a ref). A fresh-but-identical envelope from
  // the 1.5s poll keeps the SAME signature, so the toolbar survives untouched.
  if (held.signature !== signature) {
    setHeld({ signature, state: loadToolbar(signature) });
  }

  const setToolbar = useCallback(
    (patch: Partial<ToolbarState>): void => {
      // Read the current value from the store at call time, not from a closure:
      // two controls can fire in one tick (typing into search while a select
      // commits) and a stale closure would clobber the other's change.
      const next: ToolbarState = { ...loadToolbar(signature), ...patch };
      toolbarStore.set(signature, next);
      setHeld({ signature, state: next });
    },
    [signature]
  );

  const toolbar = held.signature === signature ? held.state : loadToolbar(signature);
  return { toolbar, setToolbar };
}
