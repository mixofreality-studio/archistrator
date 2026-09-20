/* eslint-disable react-refresh/only-export-components -- provider + hooks colocated */
/**
 * Maps a comment anchor's typed-model JSONPath to the DOM element that renders it,
 * so a margin card can find the row or node it belongs beside — and so clicking a
 * card can scroll that content into view. Registration is a ref callback, so a row
 * enrols itself by rendering and un-enrols by unmounting; nothing has to be
 * declared centrally.
 */
import { createContext, useCallback, useContext, useMemo, useRef, type ReactNode } from 'react';

interface Registry {
  register: (jsonPath: string, el: HTMLElement | null) => void;
  lookup: (jsonPath: string) => HTMLElement | null;
}

const Ctx = createContext<Registry | null>(null);

// DEV-only. Each of the three hooks below is a silent no-op when no
// AnchorRegistryProvider is mounted above it — the worst failure shape for this
// feature: every card lands in the unplaced bucket, the margin just looks wrong,
// and nothing anywhere says why (see CommentContext.tsx's useComments() for the
// same pattern on a missing CommentProvider).
//
// `useRegisterAnchor` runs once PER ROW, and `useAnchorOffsets` re-runs on every
// scroll/resize-driven `tick` bump — an un-deduped warn would flood the console
// on a 60-row list or while scrolling. Warn once per HOOK NAME (not per call),
// so the signal fires but never spams.
const warnedHooks = new Set<string>();
function warnIfNoProvider(hookName: string, consequence: string): void {
  if (!import.meta.env.DEV || warnedHooks.has(hookName)) return;
  warnedHooks.add(hookName);
  console.warn(
    `${hookName}: no AnchorRegistryProvider above this component — ${consequence} ` +
      'Mount AnchorRegistryProvider around the surface that renders CommentableList / the margin.'
  );
}

export function AnchorRegistryProvider({ children }: { children: ReactNode }): ReactNode {
  const map = useRef(new Map<string, HTMLElement>());
  const value = useMemo<Registry>(
    () => ({
      register: (jsonPath, el): void => {
        if (el === null) map.current.delete(jsonPath);
        else map.current.set(jsonPath, el);
      },
      lookup: (jsonPath): HTMLElement | null => map.current.get(jsonPath) ?? null,
    }),
    []
  );
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

/** Ref callback that enrols this element under `jsonPath`. No-op with no provider. */
export function useRegisterAnchor(jsonPath: string): (el: HTMLElement | null) => void {
  const reg = useContext(Ctx);
  if (reg === null) {
    warnIfNoProvider(
      'useRegisterAnchor',
      'this item will never be found by the margin — its card falls into the unplaced bucket and clicking it cannot scroll here.'
    );
  }
  return useCallback(
    (el: HTMLElement | null) => {
      reg?.register(jsonPath, el);
    },
    [reg, jsonPath]
  );
}

/**
 * Offset of each anchor within `scrollRoot`, in px. Missing anchors are omitted.
 *
 * `tick` is not read directly — it exists so a caller that re-measures on scroll
 * or resize (by bumping a state counter into this param) forces a fresh pass over
 * the DOM without having to invent its own memoization scheme. Bump it, don't read
 * it.
 */
export function useAnchorOffsets(
  jsonPaths: readonly string[],
  scrollRoot: HTMLElement | null,
  tick: number
): Map<string, number> {
  const reg = useContext(Ctx);
  if (reg === null) {
    warnIfNoProvider(
      'useAnchorOffsets',
      'every anchor offset comes back empty — the margin cannot place any card.'
    );
  }
  return useMemo(() => {
    // `tick` carries no value worth reading — bumping it is the caller's signal
    // to re-measure on scroll/resize (RULING P3a). This `void` is a deliberate
    // reference, not dead code: it satisfies exhaustive-deps honestly (the dep
    // really is used, just not for its value) instead of suppressing the rule.
    // Do NOT "clean up" this line by removing `tick` from the deps array.
    void tick;
    const out = new Map<string, number>();
    if (reg === null || scrollRoot === null) return out;
    const rootTop = scrollRoot.getBoundingClientRect().top - scrollRoot.scrollTop;
    for (const p of jsonPaths) {
      const el = reg.lookup(p);
      if (el !== null) out.set(p, el.getBoundingClientRect().top - rootTop);
    }
    return out;
    // Re-measure whenever the anchor set changes, or the caller bumps `tick` on
    // scroll/resize — the registry's map is a ref and mutates silently, so this
    // memo has no other way to learn that a registered element's position moved.
  }, [reg, jsonPaths, scrollRoot, tick]);
}

/**
 * Returns a function that scrolls a given anchor's registered element into view
 * (centered, smooth). Does nothing when the path is not currently registered —
 * e.g. the anchored item scrolled out of a virtualization window or the artifact
 * kind changed underneath the margin.
 */
export function useScrollAnchorIntoView(): (jsonPath: string) => void {
  const reg = useContext(Ctx);
  if (reg === null) {
    warnIfNoProvider('useScrollAnchorIntoView', 'clicking a margin card will silently do nothing.');
  }
  return useCallback(
    (jsonPath: string) => {
      const el = reg?.lookup(jsonPath) ?? null;
      el?.scrollIntoView({ block: 'center', behavior: 'smooth' });
    },
    [reg]
  );
}
