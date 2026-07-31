/**
 * Pure (JSX-free) caption text for FragmentBar (fragment mode, DynamicViewFlow),
 * kept in a leaf module so it is unit-testable under `node --test` (the
 * callStatus.ts / useCaseFindings.ts pattern).
 *
 * Fragment mode lights every call the walkthrough's current activity step
 * authors; when it authors NONE there are distinct reasons, and founder QA round
 * 3 asked the caption to say which rather than reading like a single
 * undifferentiated "nothing happened" gap:
 *
 *   - a call-eligible node (action / timeEvent / acceptEvent —
 *     isEligibleForRealization, the same rule the carousel's realization chip
 *     and the walkthrough's per-step badge use) with nothing realized — a REAL
 *     gap, and the loudest thing on this rail: it outranks every trail state
 *     below, because a silent realization miss is the defect this lens exists
 *     to surface;
 *   - the multi-root entry chooser (`focusStepNodeId === ''`, no step chosen
 *     yet) — a neutral prompt, not a gap at all;
 *   - any other activity-node kind (merge / decision / fork / join / start /
 *     end / …) — by design, no calls happen here.
 *
 * FOUNDER QA ROUND 4 (the visited trail): a call-less step no longer blanks the
 * diagram — the calls already walked stay lit — so the copy that said "the chain
 * stays as it was" over an empty canvas is retired. The captions now turn on
 * whether a TRAIL exists: with one, a control-flow step reports that the chain
 * so far stays lit; with none (the very first step, the start node, the entry
 * chooser) the honest statement is that no call has happened yet. That is the
 * `hasTrail` argument — it is the difference between "nothing here" and
 * "nothing NEW here".
 *
 * An unknown kind (should not happen once ArchitectureView wires
 * `focusStepKind` alongside `focusStepNodeId`, but the prop is optional)
 * degrades to the "real gap" caption — staying silent about a genuine
 * realization miss is worse than an occasional over-eager warning.
 */
import type { ActivityNodeKind } from '../../contracts/types';
import { isEligibleForRealization } from '../usecase/useCaseChip.ts';

/** The FragmentBar heading for a call-less step (`calls.length === 0`).
 *  `hasTrail` = the reader has already walked past at least one realized call,
 *  so something IS lit behind them. */
export function fragmentCallLessHeading(
  focusStepNodeId: string,
  focusStepKind: ActivityNodeKind | undefined,
  hasTrail: boolean
): string {
  // A real realization gap is a defect signal, not a navigation state: it is
  // reported whether or not a trail exists. The blank id is the entry chooser,
  // which keys no node and therefore can never be a gap.
  if (
    focusStepNodeId !== '' &&
    (focusStepKind === undefined || isEligibleForRealization(focusStepKind))
  ) {
    return 'No realization for this step';
  }
  if (hasTrail) {
    return focusStepNodeId === ''
      ? 'Choose an entry to begin.'
      : 'Control flow — no calls; the chain so far stays lit';
  }
  return 'No calls yet — step forward to begin the chain.';
}

/** The FragmentBar's explanatory second line for a call-less step, or undefined
 *  when the heading already says everything (an empty trail: the heading's
 *  "step forward to begin" needs no gloss, and there is nothing lit to explain). */
export function fragmentCallLessBody(hasTrail: boolean): string | undefined {
  return hasTrail
    ? 'Nothing new is called here — what stays lit is the chain you have already walked.'
    : undefined;
}

/**
 * Where the current fragment sits in the WHOLE chain: "call 5–7 of 22" (or
 * "call 5 of 22" for a single-call step). Founder QA round 4 — the fragment
 * caption reported the step's own calls but never the reader's overall position,
 * so a 22-call chain gave no sense of progress. Undefined when the fragment
 * lights nothing (the call-less headings speak instead) or the chain is empty.
 */
export function fragmentPositionLabel(seqs: readonly number[], total: number): string | undefined {
  if (seqs.length === 0 || total <= 0) return undefined;
  const min = Math.min(...seqs);
  const max = Math.max(...seqs);
  const where = min === max ? String(min) : `${String(min)}–${String(max)}`;
  return `call ${where} of ${String(total)}`;
}
