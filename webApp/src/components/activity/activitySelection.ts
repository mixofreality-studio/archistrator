/**
 * The URL ⇄ SELECTION rule of the Activity Experience: what `?task=&rev=` asks
 * for, corrected against what the activity actually has (spec §7.2).
 *
 * The selection lives in the URL and nowhere else. `useActivityView` polls every
 * 2 s while an agent works, so any selection held in React state beside the
 * query would be a second source of truth for the same fact — and the one that
 * loses a race. Keeping it in the address bar also means a link addresses ONE
 * revision of ONE task, which is the whole point of `?rev`.
 *
 * Correction, not rejection: a URL that names a task this activity does not have
 * (a stale deep link, a renamed lifecycle task) opens the activity's default task
 * rather than an empty body, and a `rev` the task never reached opens its latest.
 * The URL is a request; the activity is the authority.
 *
 * Pure and React-free: `node --test` loads it directly.
 */
import { selectedTaskId } from './defaultTask.ts';
import { latestRevision, type LifecycleNode } from './lifecycleGraphTypes.ts';

export interface ActivitySelection {
  taskId: string;
  revision: number;
}

/**
 * The selection the URL asks for, corrected against what the activity actually
 * has. `undefined` only when the activity has NO tasks at all — there is nothing
 * to select, and a fabricated selection would render a body about nothing.
 *
 * A task that has never run selects revision `0` — the graph's own "no revision"
 * value (`latestRevision` of an empty list, lifecycleGraphTypes.ts:119), which
 * `RevisionSelect` renders as `NO REVISION YET`. It is deliberately not 1: there
 * is no revision 1 to read, and claiming one would send the body looking for an
 * episode that does not exist.
 */
export function selectionFor(
  nodes: readonly LifecycleNode[],
  task: string | undefined,
  rev: number | undefined
): ActivitySelection | undefined {
  const taskId = selectedTaskId(nodes, task);
  if (taskId === undefined) return undefined;
  const node = nodes.find((n) => n.id === taskId);
  if (node === undefined) return undefined;
  const asked = rev !== undefined && node.revisions.some((r) => r.n === rev);
  return { taskId, revision: asked ? rev : latestRevision(node) };
}

/**
 * The `?rev` a navigation should WRITE for a (task, revision) pair — `undefined`
 * where the URL must carry none.
 *
 * ── The rule: no `rev` means LATEST ─────────────────────────────────────────
 * A URL without `rev` is a request to follow the head; only a deliberately
 * picked NON-latest revision writes one. Emitting `rev` unconditionally (as the
 * container first did) pins the reader to revision N — so when the agent
 * finishes N+1 under the 2 s poll, the screen the reader is already looking at
 * silently becomes a read-only history of N, without them asking for history and
 * without the new work ever appearing. Omitting it is what makes "open this
 * task" and "open THIS revision of this task" two different, honest addresses.
 *
 * Three cases collapse to `undefined`: the latest (and anything above it — there
 * is no such revision to address), a task that has never run (`0` is the graph's
 * own "no revision", and `?rev=0` addresses nothing), and a task this activity
 * does not carry (nothing can be checked against, so nothing is claimed).
 */
export function revisionParam(
  nodes: readonly LifecycleNode[],
  taskId: string,
  revision: number
): number | undefined {
  const node = nodes.find((n) => n.id === taskId);
  if (node === undefined) return undefined;
  if (revision < 1) return undefined;
  return revision < latestRevision(node) ? revision : undefined;
}

/**
 * True when the reader is looking at HISTORY rather than the head — which is
 * what puts the body in read-only mode (R1).
 *
 * `revision > 0` is load-bearing: a task that has never run selects 0, and 0 is
 * below every latest, so without it every not-yet-started task would open under
 * a read-only history banner for a revision nobody has ever seen.
 */
export function isHistorical(nodes: readonly LifecycleNode[], sel: ActivitySelection): boolean {
  const node = nodes.find((n) => n.id === sel.taskId);
  if (node === undefined) return false;
  return sel.revision > 0 && sel.revision < latestRevision(node);
}
