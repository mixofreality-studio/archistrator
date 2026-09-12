/**
 * The Begin/Resume control's pure rules — label, enabled state, and the list a
 * Begin names before it dispatches anything. No React here, so node:test can pin
 * it (beginControl.test.ts); the route wires it and the dialog renders it.
 *
 * Two rules the designer pass (P0-3) turned up:
 *
 *  1. The label is decided ONCE. While the project or any session probe is
 *     loading the button is disabled and says neither "Begin" nor "Resume", so
 *     it can no longer read "Begin" and flip to "Resume" a second after load.
 *  2. Begin is a real dispatch, so it asks first and says what it would start —
 *     the activities with no stored record, read from the rows the server sent,
 *     never a hardcoded list.
 */
import type { ConstructionRows } from '../../../contracts/types';

/**
 * The session endpoint's answer, as hooks/constructionStarted.ts reduces it.
 * Restated as a literal union because this pure layer may not import from hooks;
 * the route hands one straight to the other, so tsc fails the moment they drift.
 */
export type StartedAnswer = 'loading' | 'started' | 'notStarted' | 'unknown';

export interface BeginControl {
  label: string;
  disabled: boolean;
  /** Show the spinner: something is in flight (a check or a run). */
  busy: boolean;
  /** The verb the confirm step asks with. */
  verb: 'Begin' | 'Resume' | 'Begin or resume';
}

const CHECKING: BeginControl = {
  label: 'Checking construction…',
  disabled: true,
  busy: true,
  verb: 'Begin',
};

export function beginControlFor(input: {
  started: StartedAnswer;
  projectLoading: boolean;
  running: boolean;
}): BeginControl {
  if (input.running) {
    return { label: 'Construction running…', disabled: true, busy: true, verb: 'Resume' };
  }
  if (input.projectLoading) return CHECKING;
  switch (input.started) {
    case 'loading':
      return CHECKING;
    case 'started':
      return { label: 'Resume construction', disabled: false, busy: false, verb: 'Resume' };
    case 'notStarted':
      return { label: 'Begin construction', disabled: false, busy: false, verb: 'Begin' };
    case 'unknown':
      // A probe failed and none found a session: neither word can be claimed, so
      // the label says both rather than guessing one.
      return {
        label: 'Begin or resume construction',
        disabled: false,
        busy: false,
        verb: 'Begin or resume',
      };
  }
}

export interface DispatchCandidate {
  activityId: string;
  title?: string;
}

/**
 * The activities a Begin could start: every row with NO stored record
 * (`recorded === false`) — the server's own signal, not re-derived from empty
 * fields. A recorded row is already under way (or reconstructed as done), and the
 * pump does not start it again. Sorted by id so the list is stable across polls.
 */
export function notStartedActivities(
  rows: ConstructionRows | undefined,
  titleFor: (activityId: string) => string | undefined
): DispatchCandidate[] {
  return Object.values(rows ?? {})
    .filter((r) => !r.recorded)
    .map((r): DispatchCandidate => {
      const title = titleFor(r.activityId);
      return title !== undefined && title.length > 0
        ? { activityId: r.activityId, title }
        : { activityId: r.activityId };
    })
    .sort((a, b) => a.activityId.localeCompare(b.activityId));
}
