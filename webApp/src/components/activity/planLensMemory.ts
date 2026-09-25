/**
 * MODULE MEMORY of the plan lens the reader last had open (spec §7.3: "✕ returns
 * with viewport and selection preserved").
 *
 * The problem it solves: the activity route's search is `?task=&rev=` and
 * nothing else, so an activity screen cannot know which lens the reader came
 * from, and its ✕ used to land everybody on `?lens=list` — including the reader
 * who had just clicked a tile in the GRAPH. Widening the activity search with a
 * `lens` the activity screen never reads would put a second, stale copy of the
 * plan's own state in every activity URL, where a stale link would then argue
 * with the plan about which lens is current.
 *
 * So the lens stays the PLAN's, and the way back is remembered here rather than
 * carried. This is the same posture `graphViewport.ts` already takes for the
 * graph's pan/zoom: page-lifetime memory, keyed by nothing, deliberately not
 * durable — a reload legitimately starts from the default, and the URL remains
 * the only thing that can be shared.
 *
 * Pure and React-free: `node --test` loads it directly.
 */
import type { PlanLensId } from '../../contracts/routePaths.ts';

/** `'list'` is the plan's own default (`planSearch`), so it is this one's too. */
let last: PlanLensId = 'list';

/** The plan records the lens it is rendering; the last one wins. */
export function rememberPlanLens(lens: PlanLensId): void {
  last = lens;
}

/** The lens ✕ should return to: the last one the plan rendered, else `'list'`. */
export function lastPlanLens(): PlanLensId {
  return last;
}
