/**
 * Pure error→message logic for a failed gate decision (approve / send back /
 * withdraw), split out of the containers so it is unit-testable headlessly
 * (gateFaultLogic.test.ts; see sendBackLogic.ts / roleLine.ts for the pattern of
 * pulling pure UI logic into a React-free module).
 *
 * F-QA2-47: a decision submit that dies in transport (a 503 whose Temporal signal
 * WAS delivered but whose response was lost, a network fault mid-flight) is
 * AMBIGUOUS — the server-side session may already have acted on the decision.
 * Surfacing the raw transport message ("service unavailable …") reads as "nothing
 * happened", which is exactly the wrong claim. For every indeterminate fault
 * (HTTP >= 500 or a non-HTTP error) the gate shows the cause-neutral copy below;
 * the gate's own slow poll (F-QA2-48) plus the container's on-error refetch then
 * render whatever the server actually did. A DEFINITE refusal (HTTP < 500 — the
 * request arrived and was rejected: the FailedPrecondition open-comments race,
 * validation) keeps the server's own precise message, which names the fix.
 *
 * The staged send-back notes are retained by the callers on ANY error (reset()
 * only on success) — this module only decides what the banner says.
 */
// Runtime import carries the explicit .ts extension (allowImportingTsExtensions)
// so this module also loads under node:test's type-stripping.
import { ApiError } from '../../contracts/errors.ts';

/**
 * Cause-neutral copy for an indeterminate decision fault: makes no claim about
 * whether the decision was applied, promises the refresh the gate poll delivers,
 * and leaves retry with the human.
 */
export const DECISION_UNCONFIRMED_MESSAGE =
  'The decision could not be confirmed — the session may still have received it; ' +
  'the view will refresh shortly. Try again if the gate remains.';

/**
 * The gate-error banner message for one failed decision mutation: the server's own
 * message for a definite HTTP refusal (< 500), the cause-neutral
 * {@link DECISION_UNCONFIRMED_MESSAGE} for everything indeterminate (5xx, network
 * faults, anything that is not an HTTP refusal).
 */
export function gateDecisionErrorMessage(error: unknown): string {
  if (error instanceof ApiError && error.status < 500) return error.message;
  return DECISION_UNCONFIRMED_MESSAGE;
}
