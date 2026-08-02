/**
 * Pure (JSX-free) Design-Health -> per-call status join for the Architecture
 * dynamic lens, kept in a leaf module so it is unit-testable under `node --test`
 * (the findingOverlays.ts / useCaseFindings.ts pattern).
 *
 * In this lens every rendered call IS realized by construction — it exists only
 * because some CallStep authored it against an activity node. So the only
 * question the tint answers is "did designhealth flag the step this call belongs
 * to?": the whole step goes RED (a flagged step's calls are all suspect — the
 * finding is anchored at step granularity, not call granularity), an unflagged
 * step goes GREEN. A call with no owning step is left OUT of the map entirely
 * rather than guessed green.
 *
 * The caller decides WHETHER to tint at all: with no CC findings context loaded
 * (an empty findings list) an all-green map would falsely claim the chain was
 * checked, so ArchitectureView passes no map in that case and the step-through
 * keeps its neutral look.
 */
import type { DynamicViewModel } from '../../contracts/adapters';
import type { Finding } from '../../contracts/types';
import type { StepStatus } from './DynamicViewFlow';
import { findingsForStep } from './useCaseFindings.ts';

/**
 * Per-call status for the view's step-through, keyed by the call's GLOBAL seq:
 * 'red' where the owning step carries a CC finding, 'green' where the step is
 * realized and clean. `dvLabel` is the dynamic view's KEY — the same label the
 * designhealth section grammar uses (see useCaseFindings). Calls whose step is
 * unknown (blank stepNodeId), and every call when the label is blank, are
 * omitted: there is no step to attribute a verdict to.
 */
export function statusBySeqFromFindings(
  dv: DynamicViewModel,
  findings: readonly Finding[],
  dvLabel: string
): Map<number, StepStatus> {
  const out = new Map<number, StepStatus>();
  if (dvLabel.trim().length === 0) return out;

  // A step's verdict is resolved once and reused by each of its calls.
  const byStep = new Map<string, StepStatus>();
  for (const call of dv.edges) {
    if (call.stepNodeId.length === 0) continue;
    let status = byStep.get(call.stepNodeId);
    if (status === undefined) {
      status = findingsForStep(findings, dvLabel, call.stepNodeId).length > 0 ? 'red' : 'green';
      byStep.set(call.stepNodeId, status);
    }
    out.set(call.seq, status);
  }
  return out;
}
