/**
 * Pure per-use-case call-chain realization summary — the carousel's
 * REALIZATION_CHIP ("N/M steps realized", next to the CORE/NON-CORE chip) and
 * the walkthrough focus card's per-step badge share the SAME eligibility rule:
 * only action / timeEvent / acceptEvent activity nodes are ever required to
 * carry a realized step (mirrors the server's designhealth ccMustHaveStep set
 * behind CC-COVERAGE). A decision/switch node MAY carry a step but is never
 * "missing" one — callers exclude it from `eligibleNodeIds` entirely, and
 * `stepBadgeState` gates on eligibility FIRST, so a decision/switch node that
 * happens to carry a realized step is still never badge-worthy: it can
 * neither inflate/depress the chip's count nor render a ✓/✗ badge.
 *
 * Kept import-free of adapters (type-only imports) so it is directly
 * unit-testable under `node --test` (the walkthroughRoots.ts / realization.ts
 * discipline).
 */
import type { ActivityNodeKind, Finding } from '../../contracts/types';
import type { RealizedStep } from '../../contracts/realization';
import type { Tokens } from '../../utilities/theme/themes';

/** True for the three activity-node kinds a dynamic view must realize. */
export function isEligibleForRealization(kind: ActivityNodeKind): boolean {
  return kind === 'action' || kind === 'timeEvent' || kind === 'acceptEvent';
}

/**
 * The realization badge state for ONE activity node — the walkthrough focus
 * card's per-step badge. Gated on eligibility FIRST: an ineligible kind
 * (anything but action/timeEvent/acceptEvent) returns undefined regardless of
 * whether it happens to carry a realized step — a decision/switch node MAY
 * carry one (realization.ts), but is never "missing" one and is therefore
 * never badge-worthy either, the SAME rule realizationChip's `eligibleNodeIds`
 * filter already enforces for the roll-up count.
 */
export function stepBadgeState(
  kind: ActivityNodeKind,
  realized: RealizedStep | undefined,
  findings: readonly Finding[]
): { label: string; tone: 'ok' | 'warn' | 'error' } | undefined {
  if (!isEligibleForRealization(kind)) return undefined;
  if (realized !== undefined) {
    if (findings.length > 0) {
      return { label: `✗ ${findings[0]?.ruleId ?? ''}`, tone: 'error' };
    }
    return { label: '✓ realized', tone: 'ok' };
  }
  return { label: '— no realization', tone: 'warn' };
}

/** Realization tone → token color — shared by the walkthrough's per-step badge
 *  and the carousel's roll-up chip (byte-identical mapping; hoisted here so
 *  the two call sites can't drift apart). */
export function toneColor(tone: 'ok' | 'warn' | 'error', t: Tokens): string {
  if (tone === 'ok') return t.committedDot;
  if (tone === 'error') return t.dangerFg;
  return t.awaitingFg;
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
