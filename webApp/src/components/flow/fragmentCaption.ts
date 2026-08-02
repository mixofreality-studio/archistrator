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
 *   - a `decision`/`switch` node whose DECIDER the caller resolved (founder QA
 *     round 3 addendum) — that participant is lit, so the caption names it;
 *   - any other activity-node kind (merge / decision / fork / join / start /
 *     end / …) — by design, no calls happen here.
 *
 * FOUNDER QA ROUND 4 (the visited trail): a call-less step no longer blanks the
 * diagram — the calls already walked stay lit — so the copy that said "the chain
 * stays as it was" over an empty canvas is retired. The captions now turn on
 * whether a TRAIL exists: with one, a control-flow step reports that the chain
 * so far stays lit; with none (the very first step, the start node) the honest
 * statement is that no call has happened yet. That is the `hasTrail` argument —
 * it is the difference between "nothing here" and "nothing NEW here".
 *
 * ONE FUNCTION OWNS BOTH LINES (fix round 1). The heading and its explanatory
 * body were briefly computed by two independent functions over the same inputs,
 * with the body gated only on `hasTrail` — so a genuine unrealized step reached
 * mid-chain printed "No realization for this step" with the reassuring "what
 * stays lit is the chain you have already walked" directly beneath it, which
 * diluted the very defect signal the precedence above exists to protect. Nothing
 * structural stopped the two from disagreeing, so they are now decided together,
 * in one pass, and the body is emitted ONLY where it is both true and useful:
 * alongside a by-design control-flow heading that has a trail behind it. A
 * defect heading never gets a soothing second line.
 *
 * An unknown kind (should not happen once ArchitectureView wires
 * `focusStepKind` alongside `focusStepNodeId`, but the prop is optional)
 * degrades to the "real gap" caption — staying silent about a genuine
 * realization miss is worse than an occasional over-eager warning.
 */
import type { ActivityNodeKind } from '../../contracts/types';
import { isEligibleForRealization } from '../usecase/useCaseChip.ts';

/** The two caption lines for a call-less fragment, decided together. */
export interface CallLessCaption {
  /** The bold first line — always present. */
  heading: string;
  /** The quiet gloss beneath it, or undefined when the heading must stand
   *  alone (a defect signal, or nothing lit for it to explain). */
  body: string | undefined;
}

/** The only body line this rail ever shows: what the still-lit wires MEAN once
 *  the reader has walked past a step that itself calls nothing. */
const TRAIL_BODY =
  'Nothing new is called here — what stays lit is the chain you have already walked.';

/**
 * The FragmentBar caption for a step that realizes no calls
 * (`calls.length === 0`).
 *
 * @param focusStepNodeId the activity node in focus; '' is the multi-root entry
 *   chooser, which keys no node and therefore can never be a realization gap.
 * @param focusStepKind that node's kind, when the caller knows it.
 * @param hasTrail the reader has already walked past at least one realized
 *   call, so something IS lit behind them.
 * @param deciderLabel the resolved decider's display name for a call-less
 *   `decision`/`switch` node, else undefined.
 */
export function fragmentCallLessCaption(
  focusStepNodeId: string,
  focusStepKind: ActivityNodeKind | undefined,
  hasTrail: boolean,
  deciderLabel: string | undefined
): CallLessCaption {
  // A resolved decider is lit ON TOP of the trail, so it speaks first — it is
  // the one call-less state where something new IS highlighted. Only ever set
  // for decision/switch nodes, which are never call-eligible, so this can not
  // mask a realization gap.
  if (deciderLabel !== undefined) {
    return { heading: `Decided by ${deciderLabel}`, body: hasTrail ? TRAIL_BODY : undefined };
  }
  // The multi-root entry chooser wants its own affordance: you PICK an entry
  // here, there is no single step to move forward through (reviewer, fix round
  // 1). Its trail is always empty — the path is empty by definition.
  if (focusStepNodeId === '') {
    return { heading: 'Choose an entry to begin.', body: undefined };
  }
  // A real realization gap is a defect signal, not a navigation state: reported
  // whether or not a trail exists, and NEVER softened by the trail gloss.
  if (focusStepKind === undefined || isEligibleForRealization(focusStepKind)) {
    return { heading: 'No realization for this step', body: undefined };
  }
  // By design, no calls here. With a trail the chain behind the reader stays
  // lit and the gloss explains what those wires are; without one there is
  // simply nothing lit yet, and the heading already says so.
  return hasTrail
    ? { heading: 'Control flow — no calls; the chain so far stays lit', body: TRAIL_BODY }
    : { heading: 'No calls yet — step forward to begin the chain.', body: undefined };
}

/**
 * Where the current fragment sits in the WHOLE chain: "call 5–7 of 22" (or
 * "call 5 of 22" for a single-call step). Founder QA round 4 — the fragment
 * caption reported the step's own calls but never the reader's overall position,
 * so a 22-call chain gave no sense of progress. Undefined when the fragment
 * lights nothing (the call-less captions speak instead) or the chain is empty.
 */
export function fragmentPositionLabel(seqs: readonly number[], total: number): string | undefined {
  if (seqs.length === 0 || total <= 0) return undefined;
  const min = Math.min(...seqs);
  const max = Math.max(...seqs);
  const where = min === max ? String(min) : `${String(min)}–${String(max)}`;
  return `call ${where} of ${String(total)}`;
}

/**
 * The leading number for one call's row in the FragmentBar's per-call list
 * (fix round 1 on the call-chain rollout Task 5 review, FINDING 2 — the
 * summary/detail scale mismatch): `fragmentPositionLabel` above ALWAYS
 * reports the fragment's true GLOBAL `seq` range in the heading ("call 18–22
 * of 22"), but a step authoring an alt-group relabels its OWN rows locally
 * ("1a"/"1b"/"3" — realization.ts' altLabelsForStep). Printed with no
 * cross-reference, a heading reading "18–22 of 22" above a list reading
 * "1a … 3" gives a founder no way to tell these are the SAME five calls
 * rather than a contradiction. Every locally-relabeled row therefore
 * parenthesizes its own true global position too, so the row itself carries
 * the bridge: `1a (call 18)`. A call with no altLabel (the overwhelming
 * common case, and true of every call before this field existed) is
 * unaffected — its number already IS the global position, so a repeated
 * "(call N)" would be pure noise.
 */
export function fragmentRowLabel(call: { seq: number; altLabel?: string }): string {
  if (call.altLabel === undefined) return String(call.seq);
  return `${call.altLabel} (call ${String(call.seq)})`;
}

/**
 * The FragmentBar CC-checks chip's label (Task 6, call-chain rollout): the
 * chip that used to read a bare "passing ✓" / "target" now names WHAT passed —
 * this is Design-Health's CC-* call-chain family, not a test run — and, when
 * flagged, says how many findings rather than leaving the reader to click
 * through and count. `status` is the fragment's own worst-of-its-calls tint
 * (DynamicViewFlow's `worst`, itself sourced from callStatus.ts); a clean
 * fragment's `findingsCount` is never consulted, so a caller that only ever
 * has a count for the flagged case can safely pass 0.
 *
 * SCOPE QUALIFIER (fix round 1, FINDING 2 — task reviewer): this chip's count
 * is step-scoped (this fragment only), while the Architecture picker's
 * viewVerdict roll-up beside it is view-scoped (every step) — the SAME reader
 * can see "CC checks · 2 findings" here and "5 CC findings" there for the
 * same view, which reads as a contradiction unless the smaller number is
 * qualified as local. The trailing "here" is that qualifier; the clean
 * "CC checks · passing" copy is unaffected (there is no count to misread).
 */
export function ccChecksChipLabel(status: 'red' | 'green', findingsCount: number): string {
  if (status === 'green') return 'CC checks · passing';
  const n = Math.max(findingsCount, 0);
  return `CC checks · ${String(n)} finding${n === 1 ? '' : 's'} here`;
}
