/**
 * INTEGRATION-PENDING rows (architect (D), D.3) — the words every lens and the pane
 * use for them, from the server's `pendingResume` and nothing else.
 *
 * Such a row is one no construction pump wrote whose ledger holds some lifecycle
 * phases complete and others not: the backfill built C-billing-manager and
 * C-system-design-manager against their dependencies' contracts, and their
 * Integration waits until every dependency is Done. The server's coarse status
 * says `in-review` for them (EV reads it), which the surface used to render as
 * RUNNING — in flight, holding Begin on "Construction running…". Nothing runs
 * them. The fix is the server's own field, read here; the TS predecessor mirror
 * (computeActivityStatuses' done set) is not extended to decide it.
 *
 * The copy (routed to the PM, per D.3):
 *   - chip:           "Integration pending"  ("<fromPhase label> pending")
 *   - pane sentence:  "Integration pending — waits on C-a (not built), C-b (not built)"
 *                     "Integration pending — next in line"
 *   - phase line:     "waits on C-a, C-b" / "next in line" — on the fromPhase's own
 *                     row in the list, its segment in the graph, and its line in the
 *                     hover card (designer final pass, item 2).
 *
 * Pure — node:test pins it (pendingResume.test.ts).
 */
import type {
  ConstructionRow,
  PendingDependencyReason,
  PendingResumeRow,
} from '../../../contracts/types';

/** How one unsatisfied dependency is qualified in the pane's sentence. */
export const WAITS_ON_REASON_LABEL: Readonly<Record<PendingDependencyReason, string>> = {
  notBuilt: 'not built',
  builtNotIntegrated: 'built but not integrated',
  milestoneNotReached: 'not reached',
  unresolved: 'unresolved',
};

/** The fromPhase's display label, from the row's own phases (the server's profile
 *  label), or the wire name when the row does not carry it. */
export function pendingPhaseLabel(row: ConstructionRow, pr: PendingResumeRow): string {
  return row.phases.find((p) => p.phase === pr.fromPhase)?.label ?? pr.fromPhase;
}

/** "Integration pending", or undefined for a row that is not pending. */
export function pendingChipLabel(row: ConstructionRow): string | undefined {
  const pr = row.pendingResume;
  return pr === undefined ? undefined : `${pendingPhaseLabel(row, pr)} pending`;
}

/** "waits on C-a, C-b" (tagged: "waits on C-a (not built), …"), or "next in line". */
export function waitsOnText(pr: PendingResumeRow, tagged: boolean): string {
  if (pr.waitsOn.length === 0) return 'next in line';
  const ids = pr.waitsOn.map((d) =>
    tagged ? `${d.id} (${WAITS_ON_REASON_LABEL[d.reason]})` : d.id
  );
  return `waits on ${ids.join(', ')}`;
}

/** The pane's sentence: "Integration pending — waits on C-a (not built), …". */
export function pendingSentence(row: ConstructionRow): string | undefined {
  const pr = row.pendingResume;
  if (pr === undefined) return undefined;
  return `${pendingPhaseLabel(row, pr)} pending — ${waitsOnText(pr, true)}`;
}

/** The compact line the fromPhase's own row carries ("waits on C-a, C-b"), and
 *  nothing for any other phase or any row that is not pending. */
export function pendingPhaseLine(row: ConstructionRow, phase: string): string | undefined {
  const pr = row.pendingResume;
  if (pr?.fromPhase !== phase) return undefined;
  return waitsOnText(pr, false);
}

/**
 * The activities waiting on a dependency: pending rows whose waitsOn is non-empty.
 * A pending row that is next in line waits on nothing; it counts as eligible.
 */
export function waitingActivityIds(
  rows: Readonly<Record<string, ConstructionRow>> | undefined
): ReadonlySet<string> {
  return new Set(
    Object.values(rows ?? {})
      .filter((r) => (r.pendingResume?.waitsOn.length ?? 0) > 0)
      .map((r) => r.activityId)
  );
}
