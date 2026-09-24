/**
 * Every sentence the Activity Experience says, in one pure module with tests
 * (house convention — tasksLensCopy.ts, serviceContractCopy.ts, roleLine.ts,
 * submitVerb's describeConsequence). There is no i18n in this app, so a string
 * written inline in JSX is a string nobody reviews.
 */

/** The chrome eyebrow above the title: `R-GITHUB · DEPLOYMENT`, `ACTIVITY 2 · ARCHITECTURE`. */
export function eyebrowFor(input: {
  activityId: string;
  type: string;
  variant?: string | undefined;
  /** 1-based position in the committed activity list; the three design activities have one. */
  planIndex?: number | undefined;
}): string {
  const kind =
    input.variant !== undefined && input.variant.length > 0
      ? `${input.type}:${input.variant}`
      : input.type;
  const left =
    input.planIndex !== undefined ? `ACTIVITY ${String(input.planIndex)}` : input.activityId;
  return `${left.toUpperCase()} · ${kind.toUpperCase()}`;
}

/** The read-only banner on a non-latest revision (R1). */
export function historyBanner(revision: number, latest: number): string {
  return `Revision ${String(revision)} of ${String(latest)} — read-only`;
}

export const BACK_TO_LATEST = 'Back to latest';

/**
 * The caption over the artifact panel while a non-latest revision is on screen
 * (R1): what is shown is the CURRENT artifact, not the one this revision
 * judged, because no op reads an artifact as of a ref. Saying so IS the
 * feature — a silently-current artifact under a history banner is a lie.
 */
export const HISTORY_ARTIFACT_CAPTION =
  'Showing the current artifact. The version this revision judged is not readable yet — its comments and verdicts below are.';

/** Why a task's artifact cannot be rendered (R17). Names what is missing and what remains. */
export function artifactUnavailable(classification: string): string {
  return `No artifact view for a ${classification} activity yet. Its episodes and review history are below.`;
}

/** Why a construction thread offers no Resolve / Reopen / Ask (R2). */
export const CONSTRUCTION_THREAD_READ_ONLY =
  'Construction review threads are recorded, but cannot be resolved, reopened or replied to from here yet.';

/** The reviewers strip when the review engine refused to propose a roster. */
export function reviewSetRefused(detail: string): string {
  return `The review engine could not propose reviewers: ${detail}`;
}

/**
 * The sub-attempt disclosure. §7.2 shows it only when a revision had MORE than
 * one attempt, so one attempt (and zero) says nothing at all rather than "1
 * attempt". An empty string is the honest render, not a throw: copy does not
 * police its caller, and the caller renders nothing for an empty line.
 */
export function subAttemptsLine(count: number): string {
  if (count <= 1) return '';
  return `${String(count)} attempts before this revision reached the gate`;
}

/** What a 404 from QueryActivityView means: the committed plan has no such activity. */
export const ACTIVITY_NOT_IN_PLAN =
  'This activity is not in the committed activity list, so there is nothing to show for it.';
