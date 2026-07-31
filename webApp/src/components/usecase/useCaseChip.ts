/**
 * Pure per-use-case call-chain realization summary — the carousel's
 * REALIZATION_CHIP ("N/M steps realized", next to the CORE/NON-CORE chip) and
 * the walkthrough focus card's per-step badge share the SAME eligibility rule:
 * only action / timeEvent / acceptEvent activity nodes are ever required to
 * carry a realized step (mirrors the server's designhealth ccMustHaveStep set
 * behind CC-COVERAGE). A decision/switch node MAY carry a step but is never
 * "missing" one — callers exclude it from `eligibleNodeIds` entirely, so it
 * can neither inflate nor depress the count.
 *
 * Kept import-free of adapters (type-only imports) so it is directly
 * unit-testable under `node --test` (the walkthroughRoots.ts / realization.ts
 * discipline).
 */
import type { ActivityNodeKind, Finding } from '../../contracts/types';
import type { RealizedStep } from '../../contracts/realization';

/** True for the three activity-node kinds a dynamic view must realize. */
export function isEligibleForRealization(kind: ActivityNodeKind): boolean {
  return kind === 'action' || kind === 'timeEvent' || kind === 'acceptEvent';
}

/**
 * "N/M steps realized" — N = how many of the given eligible node ids have a
 * realized step, M = how many were given. Tone is 'error' when any finding is
 * present (a realized-but-flagged chain still needs attention), 'ok' when
 * every eligible node is realized and there are no findings, 'warn' otherwise
 * (steps still missing, nothing flagged yet).
 */
export function realizationChip(
  realization: Map<string, RealizedStep>,
  eligibleNodeIds: string[],
  findings: readonly Finding[]
): { label: string; tone: 'ok' | 'warn' | 'error' } {
  const total = eligibleNodeIds.length;
  const realized = eligibleNodeIds.filter((id) => realization.has(id)).length;
  const label = `${String(realized)}/${String(total)} steps realized`;
  if (findings.length > 0) return { label, tone: 'error' };
  return { label, tone: realized === total ? 'ok' : 'warn' };
}
