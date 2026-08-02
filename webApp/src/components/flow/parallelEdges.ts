/**
 * Pure per-pair ordinal for PARALLEL edges — several distinct calls drawn
 * between the SAME (source, target) participants (founder QA round 4).
 *
 * A realized call chain routinely stacks strands: SystemDesignManager calls
 * AgenticJobAccess seven separate times in the drive-system-design use case.
 * Every one of those edges leaves the same source handle and arrives at the same
 * target handle, so React Flow draws seven identical paths on top of each other
 * — the reader sees ONE line, "Next does nothing" between two steps that in fact
 * moved from strand 3 to strand 4, and the numbered chips (change 4a) pile up in
 * the same pixel.
 *
 * This leaf answers only "which strand of how many is this edge?"; the renderer
 * (LayeredStepEdge) turns the ordinal into a lateral offset. Kept JSX-free and
 * dependency-free so it is unit-testable under `node --test` (the callStatus.ts /
 * fragmentCaption.ts pattern).
 *
 * DIRECTION MATTERS: a→b and b→a are different buckets. They already leave/enter
 * different handles (bottom→top vs. the reverse), so they do not overlap and must
 * not be spread against each other.
 */

/** One edge's place among the strands sharing its (source, target) pair. */
export interface ParallelSlot {
  /** 0-based ordinal within the pair, in the order the edges were supplied. */
  index: number;
  /** How many edges share the pair (1 = not parallel at all). */
  count: number;
}

/** The minimum an edge must expose to be bucketed. */
export interface ParallelEdgeRef {
  id: string;
  from: string;
  to: string;
}

/**
 * Buckets `edges` by their directed (from, to) pair and returns each edge's slot,
 * keyed by edge id. TOTAL: every supplied edge gets an entry, including the
 * singletons (`{ index: 0, count: 1 }`) — the caller decides that count 1 means
 * "draw it exactly where it always was". Order within a pair follows the input
 * order, which for a call chain is the global sequence, so strand n is drawn in
 * the same slot on every render.
 *
 * A repeated id (should not happen — call seqs are unique) keeps the LAST slot,
 * matching Map semantics; ids are never invented.
 */
export function parallelIndex(edges: readonly ParallelEdgeRef[]): Map<string, ParallelSlot> {
  const buckets = new Map<string, ParallelEdgeRef[]>();
  for (const e of edges) {
    // NUL cannot occur in a component id, so it is an unambiguous pair separator
    // (a plain '-' would collide: 'a-b' + 'c' against 'a' + 'b-c' in kebab ids).
    const key = `${e.from}\u0000${e.to}`;
    const bucket = buckets.get(key);
    if (bucket === undefined) buckets.set(key, [e]);
    else bucket.push(e);
  }

  const slots = new Map<string, ParallelSlot>();
  for (const bucket of buckets.values()) {
    bucket.forEach((e, index) => {
      slots.set(e.id, { index, count: bucket.length });
    });
  }
  return slots;
}

/**
 * The signed lane a slot sits in, centred on 0: with 3 strands the lanes are
 * -1, 0, +1; with 4 they are -1.5, -0.5, +0.5, +1.5. Multiply by a pixel spread
 * to offset the drawn path. A lone edge (count 1) is lane 0 — it is drawn
 * exactly where it would have been drawn before any of this existed, which is
 * what keeps every other flow view pixel-identical.
 */
export function parallelLane(slot: ParallelSlot | undefined): number {
  if (slot === undefined || slot.count <= 1) return 0;
  return slot.index - (slot.count - 1) / 2;
}
