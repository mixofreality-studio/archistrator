/**
 * The test scenarios' inks, as data — which colour a case-kind chip and a call's
 * "target" tag carry, per mode (designer final items).
 *
 *   plan    N-STP's committed plan: every call is a target, in red — the plan's job
 *           is to name what must turn green.
 *   run     N-IT once anything has been attempted: each call by its last-run status.
 *   notRun  N-IT that has never been attempted: the plan's semantics, NEUTRAL ink.
 *           "Never run is not failing" (fix round B, P1-12) reached the headline
 *           and the scenario chips; the negative/boundary case chips and the
 *           "target" tag were still red, asserting failures nobody observed.
 *
 * Pure (no React), so node:test pins it (scenarioInk.test.ts). The `StepStatus`
 * import is type-only and erased before Node runs this.
 */
import type { StepStatus } from '../../flow/DynamicViewFlow';

export type ScenarioMode = 'plan' | 'run' | 'notRun';

/** A case chip's ink: `good` (green), `danger` (red), `caution` (amber), `neutral` (muted). */
export type CaseInk = 'good' | 'danger' | 'caution' | 'neutral';

/** happy = good; negative = danger and boundary = caution — neutral when never run. */
export function caseKindInk(kind: string, mode: ScenarioMode): CaseInk {
  if (kind === 'happy') return 'good';
  if (mode === 'notRun') return 'neutral';
  return kind === 'negative' ? 'danger' : 'caution';
}

/** One call's status in the step-through: red target (plan), last-run (run), or a
 *  neutral target nothing has run against (notRun). */
export function stepStatusFor(mode: ScenarioMode, recorded: string | undefined): StepStatus {
  switch (mode) {
    case 'plan':
      return 'red';
    case 'run':
      return recorded === 'green' ? 'green' : 'red';
    case 'notRun':
      return 'planned';
  }
}
