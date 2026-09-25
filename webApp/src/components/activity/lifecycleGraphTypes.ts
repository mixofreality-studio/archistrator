/**
 * The props vocabulary of the activity lifecycle graph — the branching stepper
 * every activity's full-screen experience uses in place of SlimSpine. Types and
 * a few pure helpers only, so fixtures, containers and node:test can share them
 * without importing the component.
 *
 * An activity's internal tasks (Righting Software Appendix A, Figure A-1) come in
 * exactly two kinds: an AGENTIC DISPATCH produces an artifact, a REVIEW gates it.
 *
 * ── Revisions ───────────────────────────────────────────────────────────────
 * The unit a task repeats in is a REVISION of its artifact: revision N is the
 * dispatch episode that produced draft N plus the review round that judged it.
 * So a draft task and the review task that gates it number their revisions
 * TOGETHER — they share a {@link LifecycleNode.revisionGroup} — and moving between
 * the two keeps you on the revision you were reading ({@link revisionOnNavigate}).
 * The graph always opens the latest revision and offers the earlier ones,
 * read-only.
 */

export type LifecycleNodeKind = 'dispatch' | 'review';

export type LifecycleNodeState =
  | 'done'
  | 'running'
  | 'awaitingHuman'
  /** A review that sent the work back; the upstream dispatch is (or will be) re-running. */
  | 'sentBack'
  | 'failed'
  /** Not reachable yet — an upstream task has not finished. Not selectable. */
  | 'locked'
  | 'pending';

/** One revision of a task's artifact, as this task saw it. */
export interface LifecycleRevision {
  /** 1-based, ascending; the highest `n` is the latest. */
  n: number;
  /** "awaiting you" / "sent back" / "succeeded" … — shown verbatim. */
  outcome: string;
  /** Free detail after the outcome, e.g. "14m · 312k tok". */
  detail?: string | undefined;
  /** Short display date, e.g. "Sep 12". */
  at?: string | undefined;
  commentCount?: number | undefined;
}

export interface LifecycleNode {
  id: string;
  kind: LifecycleNodeKind;
  title: string;
  /** The {@link LifecyclePhase.id} this task belongs to. */
  phase: string;
  state: LifecycleNodeState;
  /** Ids of the tasks this one waits on. Authored order of the array of nodes
   *  decides the trunk — see lifecycleGraphLayout.ts. */
  dependsOn: readonly string[];
  revisions: readonly LifecycleRevision[];
  /**
   * Tasks that revise ONE artifact together (a draft and its review) carry the
   * same group. Omitted ⇒ the task numbers its revisions alone.
   */
  revisionGroup?: string | undefined;
  /**
   * Names the branch this task STARTS, drawn on its rail just ahead of it
   * (`DECOMPRESSED` / `SUBCRITICAL` / `COMPRESSED`). Set it on the first task of
   * each branch of a fork, the trunk's included. Leave it off a branch whose
   * phase is already named beneath the rails (Figure A-1's Test Plan) — that
   * phase label is the branch's name, and the geometry drops a lane label that
   * would repeat it.
   */
  laneLabel?: string | undefined;
}

export interface LifecyclePhase {
  id: string;
  label: string;
  /** Table A-1 earned-value weight, in percent. Omitted for unweighted phases. */
  weight?: number | undefined;
  /** The phase's exit gate has passed. */
  passed: boolean;
}

export interface LifecycleSelection {
  nodeId: string;
  revision: number;
}

/** A review → dispatch RETURN edge: the review has sent that task's work back. */
export interface LifecycleBackEdge {
  /** The review task. */
  from: string;
  /** The dispatch task it judges — the other member of its revision group. */
  to: string;
  /** Revisions the pair has been through (the `↻N` the arc carries); always ≥ 2. */
  revisions: number;
}

/**
 * The graph's only backward edges. One exists IFF a review has sent work back at
 * least once — which is exactly when its pair is past revision 1, or the review
 * is sitting in `sentBack` with the redraft not yet begun. A review that approved
 * first time, a review with no dispatch task in its group, a task that never ran:
 * no edge. The arc is history, not a possibility — it is never drawn "in case".
 */
export function backEdgesOf(nodes: readonly LifecycleNode[]): LifecycleBackEdge[] {
  return nodes.flatMap((review) => {
    if (review.kind !== 'review' || review.revisionGroup === undefined) return [];
    const dispatch = nodes.find(
      (n) => n.kind === 'dispatch' && n.revisionGroup === review.revisionGroup
    );
    if (dispatch === undefined) return [];
    const revisions = Math.max(dispatch.revisions.length, review.revisions.length);
    const sentBack = Math.max(revisions - 1, review.state === 'sentBack' ? 1 : 0);
    if (sentBack < 1) return [];
    return [{ from: review.id, to: dispatch.id, revisions: Math.max(revisions, 2) }];
  });
}

/** The latest revision number of a node; 0 when it has never run. */
export function latestRevision(node: Pick<LifecycleNode, 'revisions'>): number {
  return node.revisions.reduce((n, r) => Math.max(n, r.n), 0);
}

/**
 * One revision as a line of text — the graph's revision menu and the body's
 * revision select both say it this way, so the two can never disagree:
 * `Revision 2 · sent back · 4 comments · Sep 12`.
 */
export function revisionLine(r: LifecycleRevision, latest: boolean): string {
  const parts = [`Revision ${String(r.n)}`, latest ? `${r.outcome} (latest)` : r.outcome];
  if (r.detail !== undefined) parts.push(r.detail);
  if (r.commentCount !== undefined && r.commentCount > 0) {
    parts.push(`${String(r.commentCount)} comment${r.commentCount === 1 ? '' : 's'}`);
  }
  if (r.at !== undefined) parts.push(r.at);
  return parts.join(' · ');
}

/**
 * The revision to show when the reader CLICKS over to another task (as opposed
 * to picking a revision by name, which is always honoured as picked).
 *
 * Reading revision 2 of "Draft Architecture" and clicking "Review Architecture"
 * lands on revision 2 of the review — the round that judged the draft on screen,
 * and the comments made on it. That only holds inside one revision group, and
 * only if the target has that revision; anywhere else the click opens the
 * target's latest, as a click always has.
 */
export function revisionOnNavigate(
  nodes: readonly LifecycleNode[],
  from: LifecycleSelection,
  toNodeId: string
): number {
  const to = nodes.find((n) => n.id === toNodeId);
  if (to === undefined) return 0;
  const latest = latestRevision(to);
  const origin = nodes.find((n) => n.id === from.nodeId);
  if (origin === undefined || origin.id === to.id) return latest;
  const sameGroup = origin.revisionGroup !== undefined && origin.revisionGroup === to.revisionGroup;
  const reading = from.revision < latestRevision(origin);
  if (!sameGroup || !reading) return latest;
  return to.revisions.some((r) => r.n === from.revision) ? from.revision : latest;
}
