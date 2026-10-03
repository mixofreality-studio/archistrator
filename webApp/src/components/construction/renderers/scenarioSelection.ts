/**
 * WHICH scenario the browser shows — pure, tested without a renderer.
 *
 * The deep link (`sc`, useLensSelection) is the scenario browser's STORE: a
 * component's "reached through" coverage row opens the plan at `sc=UC3-P2`, and
 * the browser reads it rather than opening on its first scenario — which is how
 * every row used to land on the first scenario whatever it named (designer check
 * on renderers S1, B2). A local pick comes next; the first scenario last. A
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
