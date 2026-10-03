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

/** A query's state as the plan view takes it (artifactRenderers AsyncArtifactData). */
export interface ScenarioPlanRead {
  data: readonly unknown[] | undefined;
  error: Error | null;
  isLoading: boolean;
}

export type ScenarioPlanState = 'loading' | 'error' | 'unread' | 'empty' | 'ready';

/**
 * WHAT the plan view may say about the scenarios read. The one distinction that
 * is easy to lose: a DISABLED read (the activity names no component, so the
 * container never fired it) has react-query's `{isLoading:false, data:undefined}`
 * shape, which is NOT an empty projection. "No use-case path reaches this
 * component" is the server's answer — `data: []` — and only that; a read that
 * never ran is `unread`, and the view says so instead of inventing a finding.
 */
export function scenarioPlanState(q: ScenarioPlanRead): ScenarioPlanState {
  if (q.isLoading) return 'loading';
  if (q.error !== null) return 'error';
  if (q.data === undefined) return 'unread';
  return q.data.length === 0 ? 'empty' : 'ready';
}
