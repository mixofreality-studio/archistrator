/**
 * Which toolbar controls mean something on which lens (designer P1-1). Pure, so
 * node:test pins it; ConstructionShell renders from it.
 *
 * The TASKS lens has its own order — risk floor first, then blast radius, then id
 * (owedRanking.ts) — so a Sort menu there would offer an ordering it ignores; it
 * is replaced by a static label that names the order in force. "Expand to current
 * phase" opens tree rows and "Observed only" rewrites a row's evidence: neither has
 * anything to act on in a table of decisions, so both are disabled there and say
 * so, rather than looking live and doing nothing.
 */
import type { LensId } from './useLensSelection.ts';

export const RANKED_LABEL = 'Ranked: risk floor · blast radius · id';
export const LIST_LENS_ONLY = 'List lens only';

export interface LensToolbarControls {
  /** A Sort menu, or the static label naming the lens's own order. */
  sort: 'menu' | 'ranked';
  /** Whether "Expand to current phase" and "Observed only" act on this lens. */
  listControls: boolean;
}

export function toolbarForLens(lens: LensId): LensToolbarControls {
  return lens === 'tasks'
    ? { sort: 'ranked', listControls: false }
    : { sort: 'menu', listControls: true };
}
