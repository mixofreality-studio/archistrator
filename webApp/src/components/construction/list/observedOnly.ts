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
import type { ConstructionRow, ConstructionRows } from '../../../contracts/types';

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
  if (rows === undefined || !observedOnly) return rows;
  const out: ConstructionRows = {};
  for (const [id, row] of Object.entries(rows)) out[id] = observedOnlyRow(row);
  return out;
}
