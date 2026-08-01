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
 * steps is not "in progress" (realizationChip would still call that 'warn') —
 * it reads as an honest "nothing built here yet, as expected" state, distinct
 * from a real CC defect. Checked BEFORE the findings branch on purpose: a
 * wholly-unrealized view's CC-COVERAGE findings (one per missing eligible
 * node) are the EXPECTED shape of "hasn't been realized yet", not a fault to
 * flag loud-error red — the 'pending' amber reads as "not yet", the 'error'
 * red as "built, but wrong". Once at least one step is realized, a finding
 * always wins tone regardless of ratio (mirrors realizationChip verbatim).
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
 *   0 realized (eligibleNodeCount > 0)      -> 'pending' — "0/7 realized · pending"
 *   any view finding, otherwise             -> 'error'   — "2 CC findings"
 *   realizedStepCount === eligibleNodeCount -> 'ok'       — "15/15 realized · CC clean"
 *   otherwise                                -> 'warn'     — "3/7 realized"
 */
export function viewVerdict(
  findings: readonly Finding[],
  dvKey: string,
  realizedStepCount: number,
  eligibleNodeCount: number
): ViewVerdict {
  if (eligibleNodeCount > 0 && realizedStepCount === 0) {
    return { label: `0/${String(eligibleNodeCount)} realized · pending`, tone: 'pending' };
  }
  const viewFindings = findingsForView(findings, dvKey);
  if (viewFindings.length > 0) {
    const n = viewFindings.length;
    return { label: `${String(n)} CC finding${n === 1 ? '' : 's'}`, tone: 'error' };
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
