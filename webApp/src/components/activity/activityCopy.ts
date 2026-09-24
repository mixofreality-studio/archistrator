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

/**
 * The 1-based plan position an activity's eyebrow shows, or `undefined` for one
 * that has none.
 *
 * ONLY the three design activities have one, and it is not read from the
 * committed activity list: that list (slot 9) is the BUILD plan and does not
 * contain them — they are derived and sit ahead of it, as activities 1–3 (spec
 * 2026-09-20 §5.1). Every other activity is addressed by its id, which is what
 * the plan, the network and the repo all call it.
 */
export function planIndexFor(type: string): number | undefined {
  return DESIGN_PLAN_INDEX[type];
}

const DESIGN_PLAN_INDEX: Readonly<Record<string, number>> = {
  requirements: 1,
  architecture: 2,
  projectDesign: 3,
};

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

/**
 * Any OTHER failure of the activity read. It is transient by construction —
 * `activityViewPollIntervalMs` degrades to a 5 s retry rather than stopping —
 * so the sentence says so instead of reading like a dead end.
 */
export function activityReadFailed(detail: string): string {
  return `Could not read this activity: ${detail}. Retrying.`;
}

/** The activity read is in flight and nothing is on screen yet. */
export const ACTIVITY_LOADING = 'Reading this activity…';

/**
 * The agent charter ids `lifecycles.gen.ts` carries, said as a reader says them.
 * A charter the table does not know is shown VERBATIM rather than mangled by a
 * generic de-hyphenator, which turns `ui-designer` into "Ui designer".
 */
const ROLE_LABEL: Readonly<Record<string, string>> = {
  'system-architect': 'System architect',
  'product-manager': 'Product manager',
  'project-manager': 'Project manager',
  'senior-developer': 'Senior developer',
  'junior-developer': 'Junior developer',
  'ui-designer': 'UI designer',
  'ux-reviewer': 'UX reviewer',
  'qa-engineer': 'QA engineer',
  'test-engineer': 'Test engineer',
  'software-tester': 'Software tester',
};

/**
 * The generating scene's role line for a CONSTRUCTION dispatch — the caller-side
 * counterpart of the design rail's `roleLineFor` (components/design/roleLine.ts),
 * which cannot serve here: its `ActiveRole`/`ActiveStep` wire enums carry no
 * worker class, so a construction task has no live sub-step to restate. What it
 * DOES know for certain is who was dispatched and on what, which is what this
 * says — and nothing more. No phase, no percentage, no timer.
 *
 * `seed` is the charter id itself: `RoleAvatar`'s `PROP_FOR` already carries all
 * nine construction charters, and an unrecognised seed falls back to a plain
 * figure rather than throwing.
 */
export function dispatchRoleLine(
  workerClass: string,
  title: string
): { seed: string; text: string } {
  return {
    seed: workerClass,
    text: `${ROLE_LABEL[workerClass] ?? workerClass} is working on ${title}`,
  };
}

/**
 * The generating scene's footer, replacing the design rail's standing "design job
 * is running in your GitHub Actions" sentence. It deliberately does NOT name a
 * venue: construction runs in whichever venue the project is configured for
 * (GitHub Actions, local, platform-funded), nothing on this screen's read reports
 * which, and naming the wrong one is exactly the fabrication the generating
 * scene was rewritten to remove.
 */
export const DISPATCH_JOB_NOTE =
  "This task's agent job runs outside the browser, in the venue this project is configured for. It keeps running if you close this screen.";

/** A task with no revision at all: nothing has been dispatched on it yet. */
export function notDispatchedYet(locked: boolean): string {
  return locked
    ? 'Locked — an upstream task has not finished, so nothing has been dispatched here yet.'
    : 'Nothing has been dispatched on this task yet.';
}

/**
 * A revision that reached the gate without an episode behind it: a backfilled or
 * reconstructed row, which the ledger marks and which carries no `episodeId`
 * (schema: "Omitted … where no episode was captured"). Rendering an empty
 * timeline instead would read as "this agent did nothing", which is a different
 * and false claim.
 */
export const NO_EPISODE_CAPTURED =
  'No episode was captured for this revision, so there is no turn-by-turn timeline to show.';

/**
 * The stand-in body for a REVIEW task until Task 9 lands the real one. Honest
 * about what is missing rather than rendering an empty review that reads as
 * "nobody said anything".
 */
export const REVIEW_BODY_NOT_YET =
  'The review body — the reviewers, the artifact and the decision — is not built yet. The revision history above is live.';
