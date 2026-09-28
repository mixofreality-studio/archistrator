/**
 * The mutation key every REVIEW DECISION of ONE project shares — the pure half of
 * `useSubmitReviewDecision`, with no React or api import, so node:test pins it
 * (reviewDecisionKey.test.ts).
 *
 * IT KEYS A TASK DECISION, and that is why it is no longer called
 * `phaseDecisionKey`: the op behind it was `SubmitPhaseDecision` on the retired
 * construction child, addressed by (activity, PHASE) out of a five-member phase
 * vocabulary. Stage 4b1 deleted that door — the decision is signalled to the generic
 * child by (activity, TASK) through `SubmitTaskDecision`, and one op now carries every
 * approve, send-back, comment-status flip and phase advance in the product. The key's
 * SHAPE is unchanged (one key per project) and so is every guarantee below; only the
 * name was still describing a workflow that no longer exists.
 *
 * The console reads what is on the wire, and what each decision answered, from the
 * QueryClient's mutation cache by this key (tasks-lens review C1). The project id is
 * load-bearing: the cache is shared by every project the app has open, and without
 * it a decision in flight on project A would hold project B's gate of the same
 * activity id busy (tasks round 2 minor).
 */
export function reviewDecisionMutationKey(projectId: string): readonly unknown[] {
  return ['submitReviewDecision', projectId];
}

/**
 * The mutation-cache filter for ONE project's review decisions — the only filter
 * the route passes, to its cache read (useMutationState) and to its one-click
 * guard (isMutating) alike (tasks round-2 review, minor: each call site built its
 * own, and a key without the project survived every test). Exact, so no other
 * project's key can match, whatever id it has.
 */
export function reviewDecisionFilters(projectId: string): {
  mutationKey: readonly unknown[];
  exact: true;
} {
  return { mutationKey: reviewDecisionMutationKey(projectId), exact: true };
}
