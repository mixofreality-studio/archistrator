/**
 * The "Observed only" evidence view (fix round B, designer P1-11).
 *
 * The old "Hide synthesized" toggle REMOVED every activity whose worst provenance
 * was reconstructed — 23 of 29 rows vanished, leaving 6. But the committed activity
 * list decides what EXISTS (spec R6: "slot 9 is authoritative for what exists");
 * the evidence only decides what is known about it. So the toggle now keeps every
 * row and strips the evidence it will not trust: with only observed events
 * counting, an activity whose record was reconstructed reads NOT STARTED.
 *
 * WHAT IS STRIPPED, AND WHY THAT MUCH
 * -----------------------------------
 * Every attempt whose origin is not `observed` is dropped. The server's resolved
 * phase completions, coarse status and current phase were derived from the
 * ledger it held — including what is now dropped — and this surface never
 * re-derives completion itself (activityTree.ts rule 2: the server is the single
 * authority). So on a row that lost ANY attempt, those derived fields are dropped
 * with it: honesty by omission. A row that lost nothing is returned as-is.
 * Omitting over-reports nothing; the cost is that a row mixing observed and
 * reconstructed attempts shows its observed task attempts but no phase progress
 * or status (none exists in the corpus today). `recorded` is untouched: a stored
 * row still exists, it just carries no trusted evidence.
 *
 * Pure — no React, no DOM — so node:test pins it (observedOnly.test.ts).
 */
import type { ConstructionRow, ConstructionRows, TaskAttemptRow } from '../../../contracts/types';

/** The row as it reads when only observed attempts count. */
export function observedOnlyRow(row: ConstructionRow): ConstructionRow {
  const kept = row.attempts.filter((a) => a.provenance.origin === 'observed');
  if (kept.length === row.attempts.length) return row;
  // Drop the fields the server derived from the ledger it held, by omission.
  const next: ConstructionRow = {
    ...row,
    attempts: kept,
    phases: [],
    hasBuildEvidence: kept.length > 0,
  };
  delete next.status;
  delete next.currentLifecyclePhase;
  delete next.worstOrigin;
  if (kept.length > 0) next.worstOrigin = 'observed';
  return next;
}

/** Every row, as the evidence view reads it. The row SET never changes. */
export function rowsForEvidenceView(
  rows: ConstructionRows | undefined,
  observedOnly: boolean
): ConstructionRows | undefined {
  return evidenceViewFor(rows, observedOnly).rows;
}

/**
 * The evidence view AND what it set aside (designer re-check B1).
 *
 * A stripped row is not an unrecorded one: its record exists, the toggle just
 * declines to count it. Without the attempts it hid, the pane could only say what
 * the stripped row says — no attempts, so "unrecorded", "no record, has not run" —
 * which is false about a row the server holds 10 attempts for. So the view carries
 * the hidden attempts per activity, and the pane states how many of them fall in
 * whatever is selected (hiddenInScope).
 *
 * `hidden` holds an entry only for an activity that lost at least one attempt, and
 * is empty whenever the toggle is off.
 */
export interface EvidenceView {
  rows: ConstructionRows | undefined;
  hidden: Readonly<Record<string, readonly TaskAttemptRow[]>>;
}

export function evidenceViewFor(
  rows: ConstructionRows | undefined,
  observedOnly: boolean
): EvidenceView {
  if (rows === undefined || !observedOnly) return { rows, hidden: {} };
  const out: ConstructionRows = {};
  const hidden: Record<string, readonly TaskAttemptRow[]> = {};
  for (const [id, row] of Object.entries(rows)) {
    out[id] = observedOnlyRow(row);
    const set = row.attempts.filter((a) => a.provenance.origin !== 'observed');
    if (set.length > 0) hidden[id] = set;
  }
  return { rows: out, hidden };
}

/**
 * The TASKS lens badge: how many activities sit at the human code-review gate
 * (`in-review`), counted from the EVIDENCE VIEW so the badge says what the list
 * says. With "Observed only" on, a row whose status came from reconstructed
 * evidence has lost that status (observedOnlyRow), so it no longer counts as
 * owed.
 *
 * It takes the view rather than bare rows on purpose: the caller cannot hand it
 * the raw rows by accident and have the badge ignore the toggle again.
 */
export function tasksOwedIn(view: EvidenceView): number {
  return Object.values(view.rows ?? {}).filter((r) => r.status === 'in-review').length;
}

/** How many of an activity's hidden attempts fall in the selection: the selected
 *  task's, else the selected phase's, else the whole activity's. */
export function hiddenInScope(
  hidden: readonly TaskAttemptRow[] | undefined,
  selection: { lifecyclePhase?: string | undefined; task?: string | undefined }
): number {
  if (hidden === undefined) return 0;
  if (selection.task !== undefined) return hidden.filter((a) => a.task === selection.task).length;
  if (selection.lifecyclePhase !== undefined) {
    return hidden.filter((a) => a.phase === selection.lifecyclePhase).length;
  }
  return hidden.length;
}
