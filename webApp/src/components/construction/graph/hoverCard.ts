/**
 * What the GRAPH lens's hover card marks — the provenance rule on the one
 * surface that is legible at every zoom (designer P0-1, code review).
 *
 * At fit zoom nothing on the canvas is readable, so the hover card IS the
 * reading surface. It used to list every phase of a backfilled lane as "Gate
 * passed" with no mark at all — laundering reconstructed evidence into fact on
 * exactly the surface the reader trusts. So it follows the list's own group
 * rule (provenance.tsx, ProvenanceGroupStamp):
 *
 *  - the HEADER carries the card-level `≈ RECONSTRUCTED` stamp when any lane
 *    on it is reconstructed (the same reading the card's own head shows);
 *  - each RECONSTRUCTED lane line carries its own stamp PLUS the lane's state
 *    chip (the one the lane shows on the canvas), so "passed" never appears
 *    without the word that qualifies it;
 *  - an UNKNOWN lane stays unmarked — `unknown` asserts nothing a reader could
 *    mistake for fact, the same reason the group stamp skips it.
 *
 * Pure — no React — pinned by hoverCard.test.ts.
 */
import { provenanceGradeOf, worstOriginOf, type ProvenanceBearing } from '../provenanceAxis.ts';
import { activityRowState, chipFor, type RowChip } from '../list/activityRowPresentation.ts';
import type { ConstructionRow } from '../../../contracts/types.ts';

export interface HoverLaneMarks {
  /** The lane's own `≈ RECONSTRUCTED` stamp. */
  stamp: boolean;
  /** The lane's state chip — shown on a stamped line, so the stamp qualifies it. */
  chip?: RowChip;
}

export function hoverLaneMarksFor(
  lane: ProvenanceBearing & { row: ConstructionRow }
): HoverLaneMarks {
  const stamp = provenanceGradeOf(worstOriginOf(lane)) === 'reconstructed';
  if (!stamp) return { stamp: false };
  const chip = chipFor(activityRowState(lane.row));
  return { stamp: true, ...(chip !== undefined ? { chip } : {}) };
}

/** The header's card-level stamp: any reconstructed lane on the card. */
export function hoverCardStampFor(lanes: readonly ProvenanceBearing[]): boolean {
  return provenanceGradeOf(worstOriginOf({ phases: lanes })) === 'reconstructed';
}
