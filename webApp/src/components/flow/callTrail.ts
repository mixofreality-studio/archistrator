/**
 * Pure derivation of the VISITED TRAIL: which calls of a realized chain the
 * reader has ALREADY walked past, given the route the walkthrough took
 * (founder QA round 4 — the architect's strongest recommendation).
 *
 * Before this, the Dynamic lens' fragment mode lit exactly one step's calls and
 * muted the other 21 strands identically, so the chain never accumulated: every
 * step looked like the first step, and a step that authors no calls blanked the
 * whole diagram. With a trail the chain BUILDS as you walk — walked calls hold a
 * mid tint, the current step burns bright, and the never-walked remainder stays
 * a ghost.
 *
 * The trail is derived, never stored: it is a function of the walkthrough's
 * breadcrumb path, so Back and Restart shrink it for free (no extra state to
 * keep in sync, no way for the two surfaces to disagree).
 *
 * Kept JSX-free and dependency-free so it is unit-testable under `node --test`
 * (the callStatus.ts / fragmentCaption.ts pattern).
 */

/** The minimum a sequenced call must expose to be placed on the trail. */
export interface TrailCall {
  /** Global 1-based position across the whole linearization. */
  seq: number;
  /** The activity node whose CallStep authored this call. */
  stepNodeId: string;
}

/**
 * The seqs of every call authored by a path node the reader has already LEFT —
 * i.e. every element of `path` except the last, which is where the reader stands
 * now (that step's calls are the CURRENT fragment and are lit differently).
 *
 * A path that loops back over a node it already visited (an activity diagram's
 * regenerate/escalate arc) puts that node's calls on the trail as well as in the
 * current fragment; the renderer resolves the tie in favour of "current", since
 * the brighter tier always wins.
 *
 * An empty path (the multi-root entry chooser) — and a one-element path (the
 * reader is standing on the very first step, having walked past nothing) — both
 * yield an empty trail. That emptiness is what the caption calls "No calls yet".
 */
export function visitedSeqsForPath(
  calls: readonly TrailCall[],
  path: readonly string[]
): Set<number> {
  const left = new Set(path.slice(0, -1));
  if (left.size === 0) return new Set();
  const seqs = new Set<number>();
  for (const c of calls) if (left.has(c.stepNodeId)) seqs.add(c.seq);
  return seqs;
}
