/**
 * Pure (JSX-free) heading selector for FragmentBar's call-less states (fragment
 * mode, DynamicViewFlow), kept in a leaf module so it is unit-testable under
 * `node --test` (the callStatus.ts / useCaseFindings.ts pattern).
 *
 * Fragment mode lights every call the walkthrough's current activity step
 * authors; when it authors NONE there are three distinct reasons, and founder
 * QA round 3 asked the caption to say which rather than reading like a single
 * undifferentiated "nothing happened" gap:
 *
 *   - the multi-root entry chooser (`focusStepNodeId === ''`, no step chosen
 *     yet) — a neutral prompt, not a gap at all;
 *   - a call-eligible node (action / timeEvent / acceptEvent —
 *     isEligibleForRealization, the same rule the carousel's realization chip
 *     and the walkthrough's per-step badge use) with nothing realized — a REAL
 *     gap;
 *   - any other activity-node kind (merge / decision / fork / join / start /
 *     end / …) — by design, no calls happen here.
 *
 * An unknown kind (should not happen once ArchitectureView wires
 * `focusStepKind` alongside `focusStepNodeId`, but the prop is optional)
 * degrades to the "real gap" caption — staying silent about a genuine
 * realization miss is worse than an occasional over-eager warning.
 */
import type { ActivityNodeKind } from '../../contracts/types';
import { isEligibleForRealization } from '../usecase/useCaseChip.ts';

/** The FragmentBar heading for a call-less step (`calls.length === 0`). */
export function fragmentCallLessHeading(
  focusStepNodeId: string,
  focusStepKind: ActivityNodeKind | undefined
): string {
  if (focusStepNodeId === '') return 'Choose an entry to begin.';
  if (focusStepKind === undefined || isEligibleForRealization(focusStepKind)) {
    return 'No realization for this step';
  }
  return 'Control-flow step — no calls';
}
