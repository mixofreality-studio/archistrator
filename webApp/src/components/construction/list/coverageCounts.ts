/**
 * The 40-vs-69 seam, made countable (Stage B Task 12).
 *
 * The committed, drift-gated activity list DERIVES 40 activities from the
 * architecture. The construction head-state (`.activityConstruction`) carries
 * 69 records — a superseded hand-authored inventory that outlived part of its
 * plan. ConstructionConsole.tsx's own comment already names the seam: "Only 9
 * of the 69 construction rows join today: the committed network and activity
 * list carry the DERIVED 40 ... while the construction head-state is still
 * keyed by the legacy ids." Nothing upstream of this module decides that;
 * every number here is COMPUTED from the two real row sets, never hand-typed,
 * because the split will keep moving as the reconciliation proceeds and a
 * hardcoded count that drifts out from under the data is worse than no count
 * at all.
 *
 * THE SIX NUMBERS
 * ---------------
 *   derived       activityListModel.activities.length — what the architecture
 *                 currently derives.
 *   mapped        of the derived set, the activities carrying a componentId —
 *                 one coding activity per component (the-method-activity-list).
 *   cross-cutting of the derived set, the activities with NO componentId — the
 *                 project-wide Method activities (integration, testing,
 *                 documentation, …) that are not any one component's.
 *   legacy        the construction head-state's own row count.
 *   reconcile     legacy rows whose activityId IS one of the derived names —
 *                 these are not orphaned, they are a currently-valid activity
 *                 that also happens to carry head-state; they stay in the
 *                 ordinary tree, unflagged.
 *   orphaned      legacy rows whose activityId names NOTHING the architecture
 *                 currently derives — superseded, not fabricated. Some carry
 *                 real recorded evidence; provenance stays true per row.
 *
 * derived = mapped + cross-cutting, and legacy = reconcile + orphaned, by
 * construction — never asserted twice, just how the arithmetic falls out.
 *
 * Plain `.ts`, no React: Node's native type-stripping test runner cannot load a
 * `.tsx` module at all, the same reason activityTree.ts and activityScope.ts
 * are siblings of their `.tsx` renderers rather than living inside them.
 */

/** The two derived-activity-list fields this module needs — a minimal
 *  structural slice of `ActivityItem`, not the whole contract, so this module
 *  stays decoupled from the wire shape. `ActivityItem[]` satisfies it as-is. */
export interface DerivedActivityRef {
  readonly name: string;
  readonly componentId?: string;
}

export interface CoverageCounts {
  readonly derivedTotal: number;
  readonly mapped: number;
  readonly crossCutting: number;
  readonly legacyTotal: number;
  readonly reconcile: number;
  readonly orphaned: number;
}

/** `''` reads as "no component" — the same non-emptiness rule
 *  ConstructionConsole.tsx already applies when it joins `componentId` onto
 *  ActivityMeta (`a.componentId !== undefined && a.componentId.length > 0`). */
function hasComponentId(a: DerivedActivityRef): boolean {
  return a.componentId !== undefined && a.componentId.length > 0;
}

/** The derived activities' own names, as a lookup set for the legacy side. */
export function derivedNameSet(derived: readonly DerivedActivityRef[]): ReadonlySet<string> {
  return new Set(derived.map((a) => a.name));
}

/** All six numbers, from the two real row sets and nothing else. */
export function computeCoverageCounts(
  derived: readonly DerivedActivityRef[],
  legacyActivityIds: readonly string[]
): CoverageCounts {
  const derivedTotal = derived.length;
  const mapped = derived.filter(hasComponentId).length;
  const crossCutting = derivedTotal - mapped;

  const names = derivedNameSet(derived);
  const legacyTotal = legacyActivityIds.length;
  const reconcile = legacyActivityIds.filter((id) => names.has(id)).length;
  const orphaned = legacyTotal - reconcile;

  return { derivedTotal, mapped, crossCutting, legacyTotal, reconcile, orphaned };
}

/**
 * Whether one legacy activityId is ORPHANED — it names nothing the derived
 * activity list currently carries — as opposed to RECONCILE, which names a
 * still-current derived activity and stays an ordinary row in the tree.
 *
 * This is also the read-only/non-selectable predicate for the LIST lens's
 * bottom group: an orphaned row is drillable for reading but never an action
 * target (see partitionLegacy and ActivityTreeView's `legacy` flag).
 */
export function isOrphanedLegacyActivity(
  activityId: string,
  derivedNames: ReadonlySet<string>
): boolean {
  return !derivedNames.has(activityId);
}

/**
 * Split any row set carrying an `activityId` into the ordinary population
 * (`current` — includes the 9 reconcile rows, which stay normal) and the
 * orphaned legacy population (`legacy` — the bottom group's contents).
 *
 * Generic over `T` rather than `ActivityNode` specifically so this pure
 * partition is exercised directly, with terse fixtures, by a test that never
 * has to build a full three-tier tree.
 */
export function partitionLegacy<T extends { activityId: string }>(
  rows: readonly T[],
  derivedNames: ReadonlySet<string>
): { current: T[]; legacy: T[] } {
  const current: T[] = [];
  const legacy: T[] = [];
  for (const row of rows) {
    (isOrphanedLegacyActivity(row.activityId, derivedNames) ? legacy : current).push(row);
  }
  return { current, legacy };
}

/** The bottom tree group's own label — one constant so the group's testid,
 *  its rendered text and any test asserting on it can never drift apart. */
export const LEGACY_GROUP_LABEL = 'LEGACY RECORDS · UNRECONCILED';

/**
 * Founder ruling on the orphaned 60 (2026-09-09 seam audit, D8): they are kept
 * as CONTEXT ONLY while the derived activities are brought into good shape,
 * and DELETED once that is done. This is a staging state with a planned end,
 * not a permanent bucket — and the deletion itself is a separate future
 * commit, never this one. Exported as one string so the group's rendered copy
 * and its tooltip can never say two different things.
 */
export const LEGACY_GROUP_COPY =
  'Superseded by the derived activity list — not fabricated, superseded; some of these carry real recorded evidence. ' +
  'Kept as context only while the derived activities are brought into good shape, and deleted once that is done.';

/** The persistent strip's exact text, e.g.
 *  `COVERAGE  40 derived · 31 mapped · 9 cross-cutting ‖ 69 legacy · 9 reconcile · 60 orphaned ⚠`.
 *  A pure formatter so the rendered strip and a test can assert the identical
 *  string without either hand-copying the other. */
export function coverageStripText(counts: CoverageCounts): string {
  return (
    `COVERAGE  ${String(counts.derivedTotal)} derived · ${String(counts.mapped)} mapped · ` +
    `${String(counts.crossCutting)} cross-cutting ‖ ${String(counts.legacyTotal)} legacy · ` +
    `${String(counts.reconcile)} reconcile · ${String(counts.orphaned)} orphaned ⚠`
  );
}
