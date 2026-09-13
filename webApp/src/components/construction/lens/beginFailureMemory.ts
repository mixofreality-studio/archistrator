/**
 * The last failed Begin dispatch, per project, in MODULE memory (fix-E review I2).
 *
 * It used to be component state, so a remount dropped it. After a 500, an in-app
 * trip to /design and back came back to a console with no failure, no hold and
 * no alert: Begin was offered again while the pump might be running. The failure,
 * its hold (`holdExpired`) and the alert's `dismissed` flag now live here, keyed by
 * projectId, the same way the deep-link memory survives a lens switch. The hold's
 * deadline is `at + UNKNOWN_OUTCOME_HOLD_MS`, so a remounted console re-arms its
 * timer for whatever is left, or expires the hold at once if it ran out while the
 * console was away.
 *
 * It is written from the Begin mutation's own onError, not only from the
 * console's, so an answer that lands while the console is unmounted is still
 * recorded. The console reads it through useBeginFailure (useSyncExternalStore).
 */
import { useSyncExternalStore } from 'react';
import type { DispatchOutcome } from './beginControl';

export interface BeginFailure {
  outcome: DispatchOutcome;
  /** When the console learned of the failure (ms since the epoch). */
  at: number;
  /** The alert is hidden. The hold is NOT lifted by this. */
  dismissed: boolean;
  /** The bounded hold ran out with no pump evidence. */
  holdExpired: boolean;
}

const failures = new Map<string, BeginFailure>();
const listeners = new Set<() => void>();

export function readBeginFailure(projectId: string): BeginFailure | null {
  return failures.get(projectId) ?? null;
}

/** Replace (or, with a function, update) one project's failure, and notify. */
export function writeBeginFailure(
  projectId: string,
  next: BeginFailure | null | ((current: BeginFailure | null) => BeginFailure | null)
): void {
  const current = readBeginFailure(projectId);
  const value = typeof next === 'function' ? next(current) : next;
  if (value === current) return;
  if (value === null) failures.delete(projectId);
  else failures.set(projectId, value);
  for (const listener of listeners) listener();
}

export function subscribeBeginFailures(listener: () => void): () => void {
  listeners.add(listener);
  return (): void => {
    listeners.delete(listener);
  };
}

/** Whether a failure still holds Begin for the pump: an unknown outcome whose hold
 *  has not run out. Evidence is not known here; the console decides that. */
export function failureAwaitsPump(failure: BeginFailure | null): boolean {
  return failure?.outcome.kind === 'unknown' && !failure.holdExpired;
}

export function useBeginFailure(projectId: string): BeginFailure | null {
  return useSyncExternalStore(subscribeBeginFailures, () => readBeginFailure(projectId));
}
