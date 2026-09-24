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

/**
 * Why a task whose activity kind DOES have a view still shows no artifact: the
 * artifact belongs to another phase of the same lifecycle. That rule is
 * `ARTIFACT_PHASES` (taskArtifactFor.ts, ported from `bodyDispatch.ts:89`), and
 * it is what stops the service contract being labelled the SRS's artifact.
 * Deliberately a DIFFERENT sentence from {@link artifactUnavailable}: "this kind
 * has no view" and "this kind's view belongs to another phase" are different
 * facts, and only one of them is a gap in the product.
 */
export function artifactNotOfThisPhase(classification: string): string {
  return `A ${classification} activity's artifact belongs to another phase of its lifecycle, so this task has none to show. Its review history is below.`;
}

/**
 * A review task whose artifact kind could not be resolved at all — the lifecycle
 * table names none for it, and nothing is guessed from its id. Shared by the
 * artifact panel and the verb table, which must refuse for the same reason.
 */
export const NO_ARTIFACT_KIND =
  'This task names no artifact kind, so there is nothing to show and nothing to decide on here.';

/**
 * The three honest absences of the activity → component → contract JOIN
 * (contracts/serviceContracts.ts), said on the review body. They are different
 * facts — a gap a person should fix, a component that is never contracted, and
 * committed data that does not place this activity at all — so they are three
 * sentences and not one.
 */
export const CONTRACT_MISSING =
  'No service contract is recorded for this component yet, so there is nothing for this review to show.';
export const CONTRACT_BY_DESIGN =
  'This component is a resource or a utility: it is provisioned or shared, never contracted here.';
export const CONTRACT_UNRESOLVED =
  'The committed architecture and activity list do not place this activity, so its contract cannot be resolved.';

/** A classified artifact whose activity has no construction record to draw from. */
export const NO_CONSTRUCTION_RECORD =
  'Nothing has been recorded against this activity yet, so its artifact has nothing to draw from.';

/** The navigation off the M0 gate: the plan changes by amending what it derives from (R7). */
export const AMEND_ARCHITECTURE = 'Amend Architecture';

/** One proposed reviewer, as the live review set describes them. */
export function reviewerChipLabel(reviewer: {
  role: string;
  perspective: string;
  mayAmend: boolean;
}): string {
  const parts = [reviewer.role];
  if (reviewer.perspective.length > 0) parts.push(reviewer.perspective);
  parts.push(reviewer.mayAmend ? 'may amend' : 'advises only');
  return parts.join(' · ');
}

/**
 * One seat of the roster a persisted round was OPENED with — a different fact
 * from the live proposal above: it names the actor who filled the role, and
 * whether the round could be decided without them.
 */
export function rosterSeatLabel(seat: { role: string; actor: string; required: boolean }): string {
  const parts = [seat.role];
  if (seat.actor.length > 0) parts.push(seat.actor);
  parts.push(seat.required ? 'required' : 'optional');
  return parts.join(' · ');
}

/** One recorded verdict, verbatim: who answered, what they said, and when. */
export function verdictLine(verdict: {
  reviewerRole: string;
  verdict: string;
  summary?: string | undefined;
  at: string;
}): string {
  const parts = [verdict.reviewerRole, verdict.verdict];
  if (verdict.summary !== undefined && verdict.summary.length > 0) parts.push(verdict.summary);
  if (verdict.at.length > 0) parts.push(verdict.at);
  return parts.join(' · ');
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
