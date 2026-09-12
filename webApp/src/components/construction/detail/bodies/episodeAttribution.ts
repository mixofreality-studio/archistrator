/**
 * WHOSE EPISODES ARE THESE? — the honesty rule behind the episode body.
 *
 * The detail pane addresses ONE task attempt. The episode list does not: the
 * construction manager's `listEpisodesForActivity` is keyed by ACTIVITY, and it
 * matches an activity's episodes whether their stored `TargetRef` is the bare
 * activity id (every episode written before Stage A) or the composite attempt
 * key `<activityId>:<task>:<n>` (every episode written since). Capture itself is
 * still deferred, so the live endpoint returns `[]` for essentially every
 * activity today.
 *
 * Put those three facts together and the obvious rendering is a lie: showing N
 * episodes inside a pane whose header says `SRS · attempt 1` states, by
 * placement alone, that those N episodes are that task's. For every legacy
 * episode that is simply false, and there is no way to find out which task it
 * belonged to — the information was never written down.
 *
 * So this module makes the claim explicit and refuses to over-state it:
 *
 *   - An episode is ATTRIBUTED only when its `TargetRef` is LITERALLY the
 *     selected attempt key. Not a prefix match, not "starts with the activity
 *     id" — `C-artifact-access` is a prefix of `C-artifact-access:srs:1` and a prefix match would
 *     re-introduce exactly the guess this exists to stop.
 *   - When any episode IS attributed, the body says so and shows ONLY those.
 *   - Otherwise the caption states the scope out loud: activity-level, and not
 *     attributable to a specific task.
 *
 * Pure, so the rule is tested without a renderer (node:test cannot load `.tsx`).
 */

/** Whether one episode can be tied to the selected attempt, or only to the activity. */
export type EpisodeAttribution = 'attributed' | 'unattributed';

/**
 * EXACT equality, deliberately. A bare activity id (`C-artifact-access`) and an attempt key
 * (`C-artifact-access:srs:1`) share a prefix, and a prefix test would label every legacy
 * episode on an activity as belonging to whichever task happened to be selected.
 */
export function attributionOf(
  targetRef: string,
  attemptId: string | undefined
): EpisodeAttribution {
  if (attemptId === undefined || attemptId.length === 0) return 'unattributed';
  return targetRef === attemptId ? 'attributed' : 'unattributed';
}

/** The caption tail when nothing can be tied to the selected task. */
export const ACTIVITY_LEVEL_CAPTION =
  '— activity-level; episodes written before this release are not attributable to a specific task';

/**
 * The extra line shown when the list is EMPTY. "No episodes" and "no episodes
 * were ever captured" are different facts, and capture being deferred is the
 * actual reason the list is empty for essentially every activity — saying so is
 * cheaper for the reader than letting them conclude the work never ran.
 */
export const CAPTURE_DEFERRED_NOTE =
  'Episode capture is still deferred, so this list is empty for essentially every activity — that is the state of the capture seam, not a statement about whether the work ran.';

export interface EpisodeScope {
  /** Which subset the body renders. */
  showing: 'attributed' | 'all';
  /** How many episodes the activity-level fetch returned. */
  total: number;
  /** How many of those carry the selected attempt key verbatim. */
  attributed: number;
  /** The tail of the panel header, after `EPISODES · N`. */
  caption: string;
}

/**
 * Decide what the episode body may claim, from the episodes' own `TargetRef`s.
 *
 * Takes the refs rather than the records so the rule stays free of the wire
 * shape — and so a test can state the case in one line.
 */
export function episodeScopeFor(
  targetRefs: readonly string[],
  attemptId: string | undefined
): EpisodeScope {
  const attributed = targetRefs.filter(
    (ref) => attributionOf(ref, attemptId) === 'attributed'
  ).length;

  if (attributed > 0 && attemptId !== undefined) {
    return {
      showing: 'attributed',
      total: targetRefs.length,
      attributed,
      caption: `— attributable to this task: TargetRef is the attempt key ${attemptId}`,
    };
  }
  return {
    showing: 'all',
    total: targetRefs.length,
    attributed: 0,
    caption: ACTIVITY_LEVEL_CAPTION,
  };
}
