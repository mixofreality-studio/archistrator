/**
 * PENDING OPERATOR NOTES (B1 fix round, item 12) — the words every lens and the pane
 * use for a note that has not reached an agent, from the row's
 * `pendingOperatorNotes` count (wire.ts: undelivered, and not a skip note) and the
 * activity's state, and nothing else.
 *
 * A note is pending from the moment it is recorded until an agent dispatch carries
 * it (plan B1.4). So a pending note on an activity nothing is running says the last
 * dispatch did not start — a failed scaffold sync puts a send-back's redraft back at
 * the gate with its note still pending — and one on a Done or Skipped activity was
 * recorded after the last agent run. While an agent runs, the note is simply queued
 * for the next dispatch, and nothing is said.
 *
 * Pure — node:test pins it (pendingNotes.test.ts).
 */
import type { RowState } from './activityRowPresentation.ts';

/** The pending line's tooltip where the activity still has work to dispatch. */
export const NOTE_PENDING_TOOLTIP =
  "The operator's note has not reached an agent yet. It rides this activity's next dispatch.";

/** The pending line's tooltip on a Done or Skipped activity. */
export const NOTE_UNDELIVERED_TOOLTIP =
  "Recorded after the last agent run. It rides this activity's next dispatch if the activity is re-queued.";

export interface PendingNoteLine {
  /** `didNotStart`: work remains and nothing runs it; `neverDelivered`: Done or Skipped. */
  kind: 'didNotStart' | 'neverDelivered';
  count: number;
  /** "Note pending — the last dispatch did not start", and its plurals. */
  text: string;
  tooltip: string;
}

/** The states in which no agent will run this activity again unless it is re-queued. */
const FINISHED: ReadonlySet<RowState> = new Set<RowState>(['passed', 'skipped']);

/**
 * The line an activity carries for its pending notes, or undefined when it has none
 * or an agent is running it now.
 */
export function pendingNoteLineFor(
  count: number | undefined,
  state: RowState
): PendingNoteLine | undefined {
  const n = count ?? 0;
  if (n < 1) return undefined;
  if (FINISHED.has(state)) {
    return {
      kind: 'neverDelivered',
      count: n,
      text:
        n === 1
          ? 'Note never delivered — no agent ran after it'
          : `${String(n)} notes never delivered — no agent ran after them`,
      tooltip: NOTE_UNDELIVERED_TOOLTIP,
    };
  }
  if (state === 'running') return undefined;
  return {
    kind: 'didNotStart',
    count: n,
    text:
      n === 1
        ? 'Note pending — the last dispatch did not start'
        : `${String(n)} notes pending — the last dispatch did not start`,
    tooltip: NOTE_PENDING_TOOLTIP,
  };
}
