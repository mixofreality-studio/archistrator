/**
 * PROVENANCE — an orthogonal axis, not a state.
 *
 * Every row on the construction surface already answers "what happened here?"
 * through the state channel (chip + glyph). This module answers a DIFFERENT
 * question that the state channel cannot: "how do we know?".
 *
 * The two must never be collapsed. On 2026-09-09 the founder ruled "assume any
 * component that is fully implemented is done and reviewed and integrated", and
 * a backfill wrote 214 task attempts onto 23 activities from that ruling, and
 * all 23 now render 100% with every lifecycle phase complete. On the 19
 * ten-task activities among them, six of the tasks (srs, srsReview, stp,
 * stpReview, integration, testing) have, bar a handful, no artifact behind them
 * at all; their entire evidence is the ruling, recorded in the attempt's
 * `provenance.basis`. Rendered on the state channel
 * alone they are indistinguishable from work someone watched happen. That is
 * laundering, and this module is what stands between the two.
 *
 * THE THREE GRADES AND THEIR CHANNELS
 * -----------------------------------
 *   recorded       nothing — the default is the ABSENCE of a mark
 *   reconstructed  a 3px hatched left rail (the house `scanlines` texture)
 *   unknown        a dashed hairline, no rail
 *
 * COLOUR IS NEVER THE CHANNEL. Colour on this surface is already committed to
 * status (chip fills) and float (band rails); swapping a colour for provenance
 * would read as a status change to anyone who has learnt the surface. TEXTURE
 * is the channel, which is why `ProvenanceRail` structurally forbids a colour:
 * `color?: undefined` is a compile-time promise, and the brief's test pins it.
 *
 * TWO DENSITY RULES (paid for by two rejected prototype rounds)
 * ------------------------------------------------------------
 *  1. STAMP THE GROUP, NOT THE ROW. The `≈ RECONSTRUCTED` badge belongs on the
 *     tier-1 and tier-2 headers only; task rows inherit the rail alone. A screen
 *     of 300 badges reads as damage; one consistent hatch on a rail reads as a
 *     MATERIAL — which is exactly what it is.
 *  2. SUB-GRADE IN THE TOOLTIP, NOT ON SCREEN. `backfilled` and `synthesized`
 *     (inferred) share one hatch. The tooltip names which, and quotes the BASIS
 *     string, so a reader sees `founderRuling[2026-09-09]=…` without leaving the
 *     row.
 *
 * CONTAGION
 * ---------
 * A node's provenance is the WORST among its descendants. A collapsed activity
 * must not be able to hide a fake: if any one attempt beneath it is
 * reconstructed, the activity header carries the badge.
 *
 * `unknown` IS NOT `observed`
 * ---------------------------
 * The single most important line in this file is the empty-ledger case. The
 * server's roll-up seeds an empty ledger to `observed` (defensible as an
 * aggregate — nothing was derived from anything unknown), so `mapConstructionRow`
 * DROPS `worstOrigin` when the ledger is empty rather than let every empty-ledger
 * row arrive stamped "observed" and render as "recorded". A row about
 * which nothing whatsoever is recorded must never claim to be trustworthy.
 * `worstOriginOf` preserves that: no attempts at all is `unknown`, never
 * `observed`.
 *
 * It is a plain `.ts` sibling of provenance.tsx for the reason every tested
 * module here is: Node's native type-stripping test runner cannot load a `.tsx`
 * module at all. Relative VALUE imports therefore carry an explicit extension.
 */
import type { RecordOriginRow } from '../../contracts/types';
import { scanlines } from '../../utilities/theme/textures.ts';

// ---------------------------------------------------------------------------
// The axis
// ---------------------------------------------------------------------------

/**
 * A resolved provenance origin — the three wire origins plus `unknown`, which
 * is NOT on the wire and cannot be: it is what "no record exists at all" reads
 * as, and the wire has no field for the absence of every field.
 */
export type ProvenanceOrigin = RecordOriginRow | 'unknown';

/** The three GRADES the surface draws. Two origins share `reconstructed`. */
export type ProvenanceGrade = 'recorded' | 'reconstructed' | 'unknown';

/**
 * Worst-first ordering over the origins that can actually appear in a ledger.
 * `unknown` is deliberately absent: it is not a rank, it is the answer when
 * there is nothing to rank. Ranking it would make every activity with one
 * un-attempted task read as `unknown` and bury the 23 reconstructed ones.
 */
const ORIGIN_RANK: Record<RecordOriginRow, number> = {
  observed: 0,
  backfilled: 1,
  synthesized: 2,
};

const GRADE_OF: Record<ProvenanceOrigin, ProvenanceGrade> = {
  observed: 'recorded',
  backfilled: 'reconstructed',
  synthesized: 'reconstructed',
  unknown: 'unknown',
};

/**
 * The SUB-grade names. `synthesized` reads as "inferred" to a human — the wire
 * word is a generator's word — and the two reconstructed sub-grades are told
 * apart HERE, in prose, never by a second visual channel.
 *
 * The ONE set of words for an origin on this surface: the tooltip below and the
 * list's expanded attempt ledger both read it, so a ledger line can never name
 * an attempt differently from the tooltip on the same row.
 */
const SUB_GRADE_LABEL: Record<ProvenanceOrigin, string> = {
  observed: 'observed',
  backfilled: 'backfilled',
  synthesized: 'inferred',
  unknown: 'unrecorded',
};

/** The sub-grade word for one origin, lower-case — as the tooltip states it. */
export function provenanceSubGradeLabel(origin: ProvenanceOrigin): string {
  return SUB_GRADE_LABEL[origin];
}

/** The one word the badge shows. Grade, never sub-grade — see density rule 2. */
export const GRADE_LABEL: Record<ProvenanceGrade, string> = {
  recorded: 'RECORDED',
  reconstructed: 'RECONSTRUCTED',
  unknown: 'UNRECORDED',
};

export function provenanceGradeOf(origin: ProvenanceOrigin): ProvenanceGrade {
  return GRADE_OF[origin];
}

// ---------------------------------------------------------------------------
// Contagion
// ---------------------------------------------------------------------------

/** One attempt, read for its origin alone. */
interface AttemptProvenanceLike {
  provenance: { origin: RecordOriginRow; basis?: string };
}

/**
 * Anything on the construction tree that can carry provenance.
 *
 * Structural rather than a union of the three node types so the SAME function
 * serves all three tiers (and a hand-built fixture) without a tier tag that
 * could drift: an ActivityNode has `phases` and the server's `worstOrigin`, a
 * PhaseNode has `tasks`, a TaskNode has `attempts`. Every field is optional
 * because each tier supplies exactly one of them.
 */
export interface ProvenanceBearing {
  /**
   * The SERVER's roll-up over the row's whole attempt ledger, when it sent one.
   *
   * Folded in alongside the walk rather than trusted instead of it: the tree
   * only carries attempts whose task key appears in the activity's Figure A-1
   * profile, so an off-profile reconstructed attempt exists on the row and
   * appears in no descendant. Taking the worst of both means a fake cannot hide
   * in the gap between the ledger and the profile.
   */
  worstOrigin?: RecordOriginRow;
  attempts?: readonly AttemptProvenanceLike[];
  tasks?: readonly ProvenanceBearing[];
  phases?: readonly ProvenanceBearing[];
}

/** Every origin recorded at or beneath `node`, in walk order. */
function originsUnder(node: ProvenanceBearing): RecordOriginRow[] {
  const found: RecordOriginRow[] = [];
  if (node.worstOrigin !== undefined) found.push(node.worstOrigin);
  for (const a of node.attempts ?? []) found.push(a.provenance.origin);
  for (const child of node.phases ?? []) found.push(...originsUnder(child));
  for (const child of node.tasks ?? []) found.push(...originsUnder(child));
  return found;
}

/**
 * The WORST provenance at or beneath `node` — the contagion rule.
 *
 * `unknown` when nothing at all is recorded beneath it. That is the whole point
 * of this function and the reason it does not simply read `row.worstOrigin`.
 */
export function worstOriginOf(node: ProvenanceBearing): ProvenanceOrigin {
  const origins = originsUnder(node);
  if (origins.length === 0) return 'unknown';
  return origins.reduce((worst, o) => (ORIGIN_RANK[o] > ORIGIN_RANK[worst] ? o : worst));
}

/**
 * The distinct `basis` strings behind the RECONSTRUCTED attempts at or beneath
 * `node`, in first-seen order.
 *
 * Only the reconstructed ones: an observed attempt's basis, if it ever carries
 * one, is corroboration rather than the thing standing in for evidence, and the
 * tooltip exists to show what a synthetic record was written FROM.
 */
export function provenanceBasesOf(node: ProvenanceBearing): string[] {
  const seen = new Set<string>();
  collectBases(node, seen);
  return [...seen];
}

function collectBases(node: ProvenanceBearing, into: Set<string>): void {
  for (const a of node.attempts ?? []) {
    const { origin, basis } = a.provenance;
    if (GRADE_OF[origin] === 'reconstructed' && basis !== undefined && basis.length > 0) {
      into.add(basis);
    }
  }
  for (const child of node.phases ?? []) collectBases(child, into);
  for (const child of node.tasks ?? []) collectBases(child, into);
}

// ---------------------------------------------------------------------------
// The rail
// ---------------------------------------------------------------------------

/**
 * How one grade is drawn. A DESCRIPTION, not CSS: the ink is supplied by the
 * row's theme token at render time (`currentColor`), identically for every
 * grade, so no colour can ever encode provenance.
 */
export interface ProvenanceRail {
  grade: ProvenanceGrade;
  /** A CSS background-image. Absent for `recorded` — no mark is the mark. */
  texture?: string;
  /** The `unknown` hairline. Absent for the other two grades. */
  outline?: 'dashed';
  /** The rail's width in px. `0` where there is no rail to draw. */
  widthPx: number;
  /**
   * ALWAYS absent, and typed `undefined` so it can never become present.
   *
   * Provenance is expressed through TEXTURE, never colour — colour on this
   * surface belongs to status and float, and a colour swap here would read as a
   * status change. The brief's test asserts this is undefined; the type makes
   * the assertion redundant, which is the point.
   */
  color?: undefined;
}

/** 3px, matching the float rail and the criticality edge — one rail width. */
const RAIL_WIDTH_PX = 3;

/**
 * The hatch, drawn in `currentColor`.
 *
 * The same `scanlines` geometry the five palettes' `texture` token uses, so the
 * rail reads as the surface's own material rather than a new mark. Taking the
 * ink from `currentColor` is what keeps `ProvenanceRail.color` genuinely absent:
 * the renderer sets one theme token on the element for every grade alike.
 */
const HATCH = scanlines('currentColor');

export function provenanceRailFor(origin: ProvenanceOrigin): ProvenanceRail {
  switch (provenanceGradeOf(origin)) {
    case 'recorded':
      // Nothing. An observed record earns no decoration; the absence of a mark
      // IS "we watched this happen", and it is the only grade that gets to be
      // quiet.
      return { grade: 'recorded', widthPx: 0 };
    case 'reconstructed':
      return { grade: 'reconstructed', texture: HATCH, widthPx: RAIL_WIDTH_PX };
    case 'unknown':
      // "A dashed outline, and NO rail." The dashed outline is not a new mark:
      // the surface already draws exactly that for an unknown — `StateGlyph`'s
      // 1px dashed square on a task row, the stage rule's 1px dashed weight
      // track on an unreported phase. Provenance composes with it instead of
      // adding ink of its own, because every activity nothing has been attempted
      // on has an empty ledger and FloatRail already MEASURED what a hairline on that
      // many rows does: it becomes ruled-paper texture and drowns the rows that
      // carry data. Hence `widthPx: 0` — the column reserves its space and the
      // hatch stays the only thing ever drawn in it.
      return { grade: 'unknown', outline: 'dashed', widthPx: 0 };
  }
}

// ---------------------------------------------------------------------------
// The words
// ---------------------------------------------------------------------------

/** How many characters of a basis string the tooltip quotes before eliding. */
const BASIS_BUDGET = 260;

/**
 * The tooltip for one node's provenance — the ONLY place the sub-grade and the
 * basis are stated.
 *
 * Plain text rather than a node so the same string serves the rail's tooltip,
 * the badge's tooltip and any aria-label a caller needs, and so it can be
 * asserted in a test without a renderer.
 */
export function provenanceTooltipFor(origin: ProvenanceOrigin, bases: readonly string[]): string {
  const sub = SUB_GRADE_LABEL[origin];
  switch (provenanceGradeOf(origin)) {
    case 'recorded':
      return 'Recorded — observed as it happened.';
    case 'unknown':
      // Deliberately not "not started". Nothing here says the work did not
      // happen; it says nothing was written down either way.
      return 'Unrecorded — no attempt exists for this. Its state is unknown, not observed.';
    case 'reconstructed':
      return [
        `Reconstructed (${sub}) — this record was WRITTEN FROM the basis below, not observed.`,
        ...bases.map((b) => `Basis: ${elide(b)}`),
      ].join('\n');
  }
}

function elide(s: string): string {
  return s.length <= BASIS_BUDGET ? s : `${s.slice(0, BASIS_BUDGET - 1)}…`;
}
