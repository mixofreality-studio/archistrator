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
  return useMemo(() => {
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
    // eslint-disable-next-line react-hooks/exhaustive-deps -- tick is a deliberate re-measure trigger (RULING P3a), not a value read in this body
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
  return useCallback(
    (jsonPath: string) => {
      const el = reg?.lookup(jsonPath) ?? null;
      el?.scrollIntoView({ block: 'center', behavior: 'smooth' });
    },
    [reg]
  );
}
