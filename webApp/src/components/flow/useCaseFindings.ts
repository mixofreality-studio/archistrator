/**
 * Pure (JSX-free) Design-Health finding joins for the per-use-case call-chain
 * views (Core Use Cases activity diagram + Architecture dynamic-view step-
 * through), kept in a leaf module so it is unit-testable under `node --test`
 * (the findingOverlays.ts pattern).
 *
 * The server's designhealth CC-* rule family (server/internal/utility/designhealth
 * /rules_callchain.go) mints Location.Section under this grammar, which this join
 * trusts VERBATIM (Task 7, "designhealth: mirror CC-* call-chain family over
 * step-keyed views"):
 *
 *   step-scoped       "dynamicView " + <view key> + " step " + <activityNodeId>
 *   view-scoped        "dynamicView " + <view key>            (CC-VIEW-USECASE)
 *   use-case-scoped    "useCase " + <useCaseId>
 *
 * `dvLabel` is always the view's KEY (not its display title) — the same label
 * the section grammar uses. A `dynamicView ${dvLabel}` PREFIX match is
 * deliberate: it catches both the view-scoped section (no " step …" suffix) and
 * every step-scoped section under that view in one pass — but the match must
 * stop at a word boundary (exact, or followed by " step "), not a bare
 * `startsWith`: view keys are free text, so "dv-order" is itself a STRING
 * prefix of a different, unrelated view "dv-order2", and a bare prefix match
 * would leak dv-order2's findings into dv-order's join.
 */
import type { Finding } from '../../contracts/types';

/**
 * Findings anchored to a use case: its use-case-scoped section, plus — when the
 * use case's linked dynamic view's key is supplied — every section for that
 * view (view-scoped and step-scoped alike; see module doc). Findings with no
 * location, or anchored elsewhere, are excluded.
 */
export function findingsForUseCase(
  findings: readonly Finding[],
  useCaseId: string,
  dvLabel?: string
): Finding[] {
  const useCaseSection = 'useCase ' + useCaseId;
  const dvPrefix = dvLabel !== undefined ? `dynamicView ${dvLabel}` : undefined;
  return findings.filter((f) => {
    const section = f.location?.section;
    if (section === undefined) return false;
    if (section === useCaseSection) return true;
    if (dvPrefix === undefined) return false;
    // Boundary-safe: an exact view-scoped match, or a step-scoped match under
    // THIS view specifically — not just any section string that happens to
    // start with these characters (a longer view key, e.g. "dv-order2", would
    // otherwise pass `startsWith("dynamicView dv-order")`).
    return section === dvPrefix || section.startsWith(dvPrefix + ' step ');
  });
}

/** Findings anchored to exactly one step (activity node) of one dynamic view. */
export function findingsForStep(
  findings: readonly Finding[],
  dvLabel: string,
  nodeId: string
): Finding[] {
  const section = `dynamicView ${dvLabel} step ${nodeId}`;
  return findings.filter((f) => f.location?.section === section);
}
