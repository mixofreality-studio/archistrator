/**
 * WHICH system test plan scenario the browser shows — pure, tested without a
 * renderer.
 *
 * The deep link (`sc`, useLensSelection) is the scenario browser's STORE: a
 * component's "reached through" coverage row opens N-STP at `sc=STP-UC3`, and
 * the browser reads it rather than opening on its first scenario — which is how
 * every row used to land on STP-UC1 whatever it named (designer check on
 * renderers S1, B2). A local pick comes next; the first scenario last. A
 * requested id the plan does not hold is ignored rather than rendering nothing.
 */
export function activeScenarioId(
  scenarios: readonly { id: string }[],
  candidates: readonly (string | undefined)[]
): string {
  for (const c of candidates) {
    if (c !== undefined && c.length > 0 && scenarios.some((s) => s.id === c)) return c;
  }
  return scenarios[0]?.id ?? '';
}

/**
 * More than this many cases go into a dropdown in a narrow browser (the 480–520px
 * pane): as chips they wrapped to four lines (designer check, polish 5).
 */
export const CASE_CHIP_LIMIT = 3;

/** Below this width the browser is in the pane, not a full page. */
export const NARROW_BROWSER_WIDTH = 640;

export function casesAsDropdown(caseCount: number, width: number): boolean {
  return caseCount > CASE_CHIP_LIMIT && width < NARROW_BROWSER_WIDTH;
}
