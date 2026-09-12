/**
 * Lens state for the construction console: WHICH lens is showing and WHAT is
 * selected — both held in the URL's search params, plus the shared toolbar
 * state, held in a module-level store keyed by a content signature.
 *
 * Why the URL and not component state
 * -----------------------------------
 * The console polls the project read every 1.5s while the construction pump is
 * cascading. Component-held selection dies to that poll: any remount (a tab
 * switch, a suspense boundary, an envelope-identity change that re-keys a
 * subtree) drops it, and the operator loses their place mid-glance. NetworkView
 * already had to work around exactly this with a module-level selection store
 * (see NetworkView.tsx signatureOf/selectionStore) — this module keeps that
 * pattern for the toolbar and goes one better for SELECTION, which lives in the
 * URL instead:
 *
 *   ?lens=list&a=<activityId>&p=<lifecyclePhase>&k=<task>&n=<attempt>
 *
 *   - the shared detail pane never OWNS selection, so it cannot lose it;
 *   - a remount cannot wipe it — the URL is outside React's tree entirely;
 *   - a link addresses exactly ONE task attempt, which is how a review
 *     notification will deep-link an operator straight to the thing it wants.
 *
 * The codec below is total: every malformed value degrades to something
 * renderable rather than propagating. An unknown lens falls back to `list`
 * (a blank surface is worse than the default one) and a non-numeric attempt is
 * DROPPED rather than handed downstream as `NaN`.
 */
import { useCallback, useMemo, useState } from 'react';
import { getRouteApi } from '@tanstack/react-router';

// ---------------------------------------------------------------------------
// Lens identity
// ---------------------------------------------------------------------------

export const LENS_IDS = ['list', 'graph', 'tasks'] as const;
export type LensId = (typeof LENS_IDS)[number];

/** The one selectable thing, at whatever depth the operator has reached. */
export interface LensSelection {
  activityId?: string;
  lifecyclePhase?: string;
  task?: string;
  /** 1-based attempt index. Never `NaN` — a junk value is dropped, not coerced. */
  attempt?: number;
}

export interface LensState {
  lens: LensId;
  selection: LensSelection;
}

/**
 * The wire shape of the route's search params. `parseLensSearch` takes the looser
 * `Record<string, unknown>` (the URL hands over whatever it likes), so callers
 * holding one of these SPREAD it in — `parseLensSearch({ ...search })`.
 */
export interface LensSearchParams {
  lens?: LensId;
  a?: string;
  p?: string;
  k?: string;
  n?: number;
}

function isLensId(value: unknown): value is LensId {
  return typeof value === 'string' && (LENS_IDS as readonly string[]).includes(value);
}

/** A non-empty string, or nothing. An empty param is an absent param. */
function nonEmpty(value: unknown): string | undefined {
  return typeof value === 'string' && value.length > 0 ? value : undefined;
}

/**
 * A 1-based attempt index, or nothing. `'x'`, `''`, `'1.5'`, `'0'` and `'-3'`
 * all yield undefined — passing `NaN` downstream would render as "attempt NaN"
 * and silently poison every comparison that touches it.
 */
function attemptOf(value: unknown): number | undefined {
  if (typeof value !== 'string' && typeof value !== 'number') return undefined;
  if (typeof value === 'string' && value.trim().length === 0) return undefined;
  const n = Number(value);
  return Number.isInteger(n) && n >= 1 ? n : undefined;
}

/** Decode the URL's search params into the lens + selection the console renders. */
export function parseLensSearch(search: Record<string, unknown>): LensState {
  const rawLens = search['lens'];
  const activityId = nonEmpty(search['a']);
  const lifecyclePhase = nonEmpty(search['p']);
  const task = nonEmpty(search['k']);
  const attempt = attemptOf(search['n']);
  return {
    lens: isLensId(rawLens) ? rawLens : 'list',
    selection: {
      ...(activityId !== undefined ? { activityId } : {}),
      ...(lifecyclePhase !== undefined ? { lifecyclePhase } : {}),
      ...(task !== undefined ? { task } : {}),
      ...(attempt !== undefined ? { attempt } : {}),
    },
  };
}

/**
 * Encode lens + selection back into search params.
 *
 * `lens` is ALWAYS emitted, even for the default `list`. The route's
 * validateSearch output IS the URL's search — anything it omits is stripped from
 * the address bar — so dropping `lens=list` would quietly rewrite a shared deep
 * link, which is the exact failure this whole shape exists to prevent.
 */
export function serializeLensSearch({ lens, selection }: LensState): LensSearchParams {
  return {
    lens,
    ...(selection.activityId !== undefined ? { a: selection.activityId } : {}),
    ...(selection.lifecyclePhase !== undefined ? { p: selection.lifecyclePhase } : {}),
    ...(selection.task !== undefined ? { k: selection.task } : {}),
    ...(selection.attempt !== undefined ? { n: selection.attempt } : {}),
  };
}

/**
 * The route's `validateSearch`: normalize whatever arrived in the URL into the
 * typed params. A deep link validates instead of throwing; junk keys and junk
 * values are dropped rather than rendered.
 */
export function validateLensSearch(search: Record<string, unknown>): LensSearchParams {
  return serializeLensSearch(parseLensSearch(search));
}

// ---------------------------------------------------------------------------
// The hook — TanStack Router's search params as the single source of selection
// ---------------------------------------------------------------------------

const routeApi = getRouteApi('/project/$projectId/construction');

export interface LensSelectionApi extends LensState {
  setLens: (lens: LensId) => void;
  /** Replace the selection wholesale (a shallower click clears what is below it). */
  select: (selection: LensSelection) => void;
  /** Drop the selection, keeping the lens. */
  clear: () => void;
}

export function useLensSelection(): LensSelectionApi {
  const search = routeApi.useSearch();
  const navigate = routeApi.useNavigate();

  const state = useMemo(() => parseLensSearch({ ...search }), [search]);

  // `replace: true` — lens/selection changes are a glance, not a navigation. A
  // history entry per click would make Back useless for leaving the console.
  const push = useCallback(
    (next: LensState): void => {
      void navigate({ search: serializeLensSearch(next), replace: true });
    },
    [navigate]
  );

  const setLens = useCallback(
    (lens: LensId): void => {
      push({ lens, selection: state.selection });
    },
    [push, state.selection]
  );

  const select = useCallback(
    (selection: LensSelection): void => {
      push({ lens: state.lens, selection });
    },
    [push, state.lens]
  );

  const clear = useCallback((): void => {
    push({ lens: state.lens, selection: {} });
  }, [push, state.lens]);

  return { lens: state.lens, selection: state.selection, setLens, select, clear };
}

// ---------------------------------------------------------------------------
// Shared toolbar state — persisted by content signature (the NetworkView pattern)
// ---------------------------------------------------------------------------

/**
 * Scope chips, shared by every lens. `critical`/`near` carry the existing tracker
 * filter vocabulary forward unchanged; the rest are the rewrite's additions.
 * Only `all` is wired to anything this stage — the predicates land in Task 11.
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
   * The Task 11 audit toggle. ON treats any node whose worst provenance grade
   * is `reconstructed` (backfilled or synthesized) as absent from the LIST —
   * not merely undecorated. Default OFF: the surface's normal reading already
   * marks reconstructed rows (the hatched rail + group badge); this toggle
   * exists to let a reader ask "what is left if I do not trust a single ruling
   * or inference?" and see the answer render without crashing.
   */
  hideSynthesized: boolean;
}

export const DEFAULT_TOOLBAR: ToolbarState = {
  search: '',
  scope: 'all',
  kind: 'all',
  layer: 'all',
  sort: 'network',
  hideSynthesized: false,
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
