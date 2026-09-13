/**
 * The last Begin dispatch's outcome, per project, in MODULE memory (fix-E review
 * I2; fix H). Two records, one mechanism:
 *
 *  - a FAILED dispatch (BeginFailure). It used to be component state, so a
 *    remount dropped it. After a 500, an in-app trip to /design and back came back
 *    to a console with no failure, no hold and no alert: Begin was offered again
 *    while the pump might be running. The failure, its hold (`holdExpired`) and the
 *    alert's `dismissed` flag live here, keyed by projectId, the same way the
 *    deep-link memory survives a lens switch. The hold's deadline is
 *    `at + UNKNOWN_OUTCOME_HOLD_MS`, so a remounted console re-arms its timer for
 *    whatever is left, or expires the hold at once if it ran out while away.
 *  - a SUCCESSFUL dispatch still awaiting its pickup (BeginDispatched, fix H). The
 *    same bounded hold, for the same reason: a trip away and back during the gap
 *    before the first read shows the pickup must not hand back an enabled Begin.
 *    It leaves memory once the pickup shows, or when the hold runs out.
 *
 * Both are written from the Begin mutation's own callbacks, not only from the
 * console's, so an answer that lands while the console is unmounted is still
 * recorded. The console reads them through useSyncExternalStore.
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
  /**
   * What the project read on screen said about constructionStarted when the
   * dispatch failed (`undefined` if there was none). Only a change from `false`
   * counts as pump evidence: on a project already started it was true before the
   * dispatch, and proves nothing about it (fix-G review I1).
   */
  startedAtFailure: boolean | undefined;
}

/** A successful dispatch whose pickup no read has shown yet. */
export interface BeginDispatched {
  /** When the success was answered (ms since the epoch). */
  at: number;
}

type Update<T> = T | null | ((current: T | null) => T | null);

/** One keyed store: a value per project, a listener set, and a stable snapshot. */
function projectMemory<T>(): {
  read: (projectId: string) => T | null;
  write: (projectId: string, next: Update<T>) => void;
  subscribe: (listener: () => void) => () => void;
} {
  const values = new Map<string, T>();
  const listeners = new Set<() => void>();
  const read = (projectId: string): T | null => values.get(projectId) ?? null;
  return {
    read,
    write: (projectId, next): void => {
      const current = read(projectId);
      const value =
        typeof next === 'function' ? (next as (current: T | null) => T | null)(current) : next;
      if (value === current) return;
      if (value === null) values.delete(projectId);
      else values.set(projectId, value);
      for (const listener of listeners) listener();
    },
    subscribe: (listener) => {
      listeners.add(listener);
      return (): void => {
        listeners.delete(listener);
      };
    },
  };
}

const failures = projectMemory<BeginFailure>();
const dispatches = projectMemory<BeginDispatched>();

export function readBeginFailure(projectId: string): BeginFailure | null {
  return failures.read(projectId);
}

/** Replace (or, with a function, update) one project's failure, and notify. */
export function writeBeginFailure(projectId: string, next: Update<BeginFailure>): void {
  failures.write(projectId, next);
}

export function subscribeBeginFailures(listener: () => void): () => void {
  return failures.subscribe(listener);
}

/** Whether a failure still holds Begin for the pump: an unknown outcome whose hold
 *  has not run out. Evidence is not known here; the console decides that. */
export function failureAwaitsPump(failure: BeginFailure | null): boolean {
  return failure?.outcome.kind === 'unknown' && !failure.holdExpired;
}

export function useBeginFailure(projectId: string): BeginFailure | null {
  return useSyncExternalStore(subscribeBeginFailures, () => readBeginFailure(projectId));
}

export function readBeginDispatched(projectId: string): BeginDispatched | null {
  return dispatches.read(projectId);
}

/** Replace (or, with a function, update) one project's awaited pickup, and notify. */
export function writeBeginDispatched(projectId: string, next: Update<BeginDispatched>): void {
  dispatches.write(projectId, next);
}

export function subscribeBeginDispatches(listener: () => void): () => void {
  return dispatches.subscribe(listener);
}

export function useBeginDispatched(projectId: string): BeginDispatched | null {
  return useSyncExternalStore(subscribeBeginDispatches, () => readBeginDispatched(projectId));
}
