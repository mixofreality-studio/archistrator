/**
 * The localStorage slot a reader's UNSENT comments persist under (R10).
 *
 * The design rails key their drafts by `<projectId>:<kind>`, because a Phase-1 /
 * Phase-2 step IS one artifact kind and there is exactly one live draft of it.
 * The Activity Experience has neither property: one activity carries several
 * review TASKS (an SRS gate, a detailed-design gate, a code gate), and each task
 * carries several REVISIONS. Notes staged against revision 2 of the code review
 * are not notes about revision 1 of the SRS review, so they cannot share a slot —
 * the reader would come back to a composer holding somebody else's sentences.
 *
 * Hence five segments: the `activity:` discriminator, the project, the activity,
 * the task and the revision. The discriminator is what makes a collision with the
 * design rails' two-segment shape impossible, which matters while both shapes are
 * live (stage 6 is where the design rails move onto this one).
 *
 * ── Why changing the shape is safe, and not lossy ───────────────────────────
 * `pendingCommentsStore` stamps every persisted envelope with the project
 * head-state `projectVersion` and refuses one written by a later incarnation
 * (`loadPending`, pendingCommentsStore.ts:73-92). A key that has never been
 * written simply loads EMPTY — which is the correct reading of "nothing was
 * staged here", not a loss. Unsent drafts are ephemeral by construction; the
 * failure this shape prevents (one task's notes surfacing under another's gate)
 * is the one that would actually mislead a reviewer.
 *
 * Pure and React-free: `node --test` loads it directly.
 */

/**
 * `activity:<projectId>:<activityId>:<taskId>:<rev>` — the draft slot a reader's
 * unsent comments persist under on the Activity Experience.
 */
export function activityCommentKey(input: {
  projectId: string;
  activityId: string;
  taskId: string;
  /** The selected revision; `0` on a task that has never run (activitySelection.ts). */
  revision: number;
}): string {
  return `activity:${input.projectId}:${input.activityId}:${input.taskId}:${String(input.revision)}`;
}

/**
 * The design rails' existing key shape, unchanged. It is written down here so the
 * two shapes are stated side by side and the activity one can be PROVEN unable to
 * collide with it (see the tests). The System Design and Project Design rails keep
 * using it until stage 6 moves them onto {@link activityCommentKey}.
 */
export function designCommentKey(projectId: string, kind: string): string {
  return `${projectId}:${kind}`;
}
