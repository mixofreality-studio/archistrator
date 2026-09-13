/**
 * The mutation key every phase decision of ONE project shares — the pure half of
 * useSubmitPhaseDecision, with no React or api import, so node:test pins it
 * (phaseDecisionKey.test.ts).
 *
 * The console reads what is on the wire, and what each decision answered, from the
 * QueryClient's mutation cache by this key (tasks-lens review C1). The project id is
 * load-bearing: the cache is shared by every project the app has open, and without
 * it a decision in flight on project A would hold project B's gate of the same
 * activity id busy (tasks round 2 minor).
 */
export function phaseDecisionMutationKey(projectId: string): readonly unknown[] {
  return ['submitPhaseDecision', projectId];
}

/**
 * The mutation-cache filter for ONE project's phase decisions — the only filter
 * the route passes, to its cache read (useMutationState) and to its one-click
 * guard (isMutating) alike (tasks round-2 review, minor: each call site built its
 * own, and a key without the project survived every test). Exact, so no other
 * project's key can match, whatever id it has.
 */
export function phaseDecisionFilters(projectId: string): {
  mutationKey: readonly unknown[];
  exact: true;
} {
  return { mutationKey: phaseDecisionMutationKey(projectId), exact: true };
}
