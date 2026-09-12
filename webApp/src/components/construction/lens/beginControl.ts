/**
 * The Begin/Resume control's pure rules — label, enabled state, and the list a
 * Begin names before it dispatches anything. No React here, so node:test can pin
 * it (beginControl.test.ts); the route wires it and the dialog renders it.
 *
 * The label is the project read's `constructionStarted`, computed ONCE on the
 * server from the stored head-state (fix round B, item 7) — never counted from rows
 * or attempts here: the backfill filled 23 rows with reconstructed attempts no pump
 * ever ran, and the server already refuses to count those. It replaced probing one
 * construction-session endpoint per committed activity on every load.
 *
 * Two rules the designer pass (P0-3) turned up still hold:
 *
 *  1. The label is decided ONCE. While the project is loading the button is
 *     disabled and says neither "Begin" nor "Resume", so it cannot read "Begin"
 *     and flip to "Resume" after load.
 *  2. Begin is a real dispatch, so it asks first and says what it would start —
 *     the activities with no stored record, read from the rows the server sent,
 *     never a hardcoded list.
 */
import type { ConstructionRows } from '../../../contracts/types';

export interface BeginControl {
  label: string;
  disabled: boolean;
  /** Show the spinner: something is in flight (a load or a run). */
  busy: boolean;
  /** The verb the confirm step asks with. */
  verb: 'Begin' | 'Resume';
}

const CHECKING: BeginControl = {
  label: 'Checking construction…',
  disabled: true,
  busy: true,
  verb: 'Begin',
};

export function beginControlFor(input: {
  /** The project read's constructionStarted; `undefined` when there is no project read. */
  constructionStarted: boolean | undefined;
  projectLoading: boolean;
  running: boolean;
}): BeginControl {
  if (input.running) {
    return { label: 'Construction running…', disabled: true, busy: true, verb: 'Resume' };
  }
  if (input.projectLoading) return CHECKING;
  if (input.constructionStarted === undefined) {
    // Loaded, but no project read to answer from: neither word can be claimed, and
    // nothing can sensibly be dispatched.
    return { label: 'Construction state unavailable', disabled: true, busy: false, verb: 'Begin' };
  }
  return input.constructionStarted
    ? { label: 'Resume construction', disabled: false, busy: false, verb: 'Resume' }
    : { label: 'Begin construction', disabled: false, busy: false, verb: 'Begin' };
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
