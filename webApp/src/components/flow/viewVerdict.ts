/**
 * Pure (JSX-free) per-VIEW call-chain verdict roll-up — the Architecture
 * dynamic lens' picker-row chip (testid Architecture.VIEW_VERDICT; call-chain
 * rollout Task 6), kept in a leaf module so it is unit-testable under
 * `node --test` (the callStatus.ts / useCaseChip.ts pattern).
 *
 * This is a SIBLING of useCaseChip.ts' `realizationChip` (the use-cases
 * carousel's "N/M steps realized" roll-up), not a replacement: that one is
 * scoped to a USE CASE (`findingsForUseCase` — use-case-scoped findings PLUS
 * every finding under its linked view), this one to a VIEW (`dynamicView
 * <dvKey>` findings only — view-scoped plus every step under it, the same
 * prefix join `statusBySeqFromFindings` uses via its own `dvLabel` param). It
 * deliberately REUSES realizationChip's two governing rules so the two
 * roll-ups can never read as contradicting each other for the same use case:
 * eligibility is decided by the caller with the SAME `isEligibleForRealization`
 * predicate (action/timeEvent/acceptEvent), and ANY finding on the view
 * outranks the realized ratio, tone 'error', exactly as realizationChip's own
 * "findings → error tone" rule does.
 *
 * The one genuinely NEW shape here is 'pending': a view with ZERO realized
 * steps and NO dvKey-scoped findings reads as an honest "nothing built here
 * yet, as expected" state (realizationChip would still call that 'warn').
 *
 * PRECEDENCE (fix round 1 — corrected): findings are checked FIRST, always.
 * Any dvKey-scoped finding wins 'error' regardless of the realized ratio,
 * including at zero-realized — mirroring realizationChip's own
 * "findings → error tone" rule with NO carve-out. 'pending' only applies once
 * findings are confirmed empty AND realized is zero. This matters for a real,
 * reachable case: a view can have a realized DECISION step (ineligible for
 * the realized/eligible count) that still carries a genuine dvKey-scoped
 * defect (e.g. CC-ACTOR-EDGE, section "dynamicView <key> step <decisionNode>")
 * while eligible-realized sits at 0 — checking zero-realized first would have
 * printed "pending" and hidden a real error.
 *
 * The EARLIER version of this module got the ordering backwards on a false
 * premise: it assumed a wholly-unrealized view's CC-COVERAGE findings would
 * flood the findings branch and need to be suppressed by checking zero-
 * realized first. CC-COVERAGE findings are actually USE-CASE-scoped
 * ("useCase <useCaseId>"), never dvKey-scoped ("dynamicView <dvKey>...") — see
 * server/internal/utility/designhealth/rules_callchain.go — so
 * `findingsForView`'s dvKey-prefix join structurally never sees them in the
 * first place. Their absence from this roll-up is a harmless side effect of
 * the section grammar (they anchor to the use case, not this one view), not
 * something this function's branch order needs to protect against.
 */
import type { Finding } from '../../contracts/types';

/** The four verdict tones: 'pending' is new here (see module doc); the other
 *  three exactly match useCaseChip.ts' `toneColor` inputs — extended there to
 *  accept 'pending' too, so both roll-ups still share ONE color mapping. */
export type ViewVerdictTone = 'ok' | 'warn' | 'error' | 'pending';

export interface ViewVerdict {
  label: string;
  tone: ViewVerdictTone;
}

/** Findings anchored to this view: its view-scoped section (`dynamicView
 *  <dvKey>`, CC-VIEW-USECASE) plus every step-scoped section under it
 *  (`dynamicView <dvKey> step <nodeId>`) — the same prefix join
 *  `statusBySeqFromFindings` performs per-step, just not narrowed to one
 *  step. A blank `dvKey` joins nothing (no section grammar to match on). */
function findingsForView(findings: readonly Finding[], dvKey: string): Finding[] {
  if (dvKey.trim().length === 0) return [];
  const prefix = `dynamicView ${dvKey}`;
  return findings.filter((f) => {
    const section = f.location?.section;
    if (section === undefined) return false;
    return section.startsWith(prefix);
  });
}

/**
 * The Architecture dynamic lens' per-view roll-up: `realizedStepCount` of
 * `eligibleNodeCount` action/timeEvent/acceptEvent nodes realized, joined
 * against `findings` (the RAW, unfiltered finding list — this function does
 * its own `dvKey`-scoped join, the same idiom `statusBySeqFromFindings` uses)
 * for this one view.
 *
 *   any dvKey-scoped finding, ALWAYS first   -> 'error'   — "2 CC findings"
 *   0 realized, no findings (eligibleNodeCount > 0)
 *                                             -> 'pending' — "0/7 realized · pending"
 *   realizedStepCount === eligibleNodeCount  -> 'ok'       — "15/15 realized · CC clean"
 *   otherwise                                 -> 'warn'     — "3/7 realized"
 */
export function viewVerdict(
  findings: readonly Finding[],
  dvKey: string,
  realizedStepCount: number,
  eligibleNodeCount: number
): ViewVerdict {
  const viewFindings = findingsForView(findings, dvKey);
  if (viewFindings.length > 0) {
    const n = viewFindings.length;
    return { label: `${String(n)} CC finding${n === 1 ? '' : 's'}`, tone: 'error' };
  }
  if (eligibleNodeCount > 0 && realizedStepCount === 0) {
    return { label: `0/${String(eligibleNodeCount)} realized · pending`, tone: 'pending' };
  }
  if (realizedStepCount === eligibleNodeCount) {
    return {
      label: `${String(realizedStepCount)}/${String(eligibleNodeCount)} realized · CC clean`,
      tone: 'ok',
    };
  }
  return {
    label: `${String(realizedStepCount)}/${String(eligibleNodeCount)} realized`,
    tone: 'warn',
  };
}
