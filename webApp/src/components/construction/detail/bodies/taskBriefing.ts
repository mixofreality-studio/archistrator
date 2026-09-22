/**
 * THE BRIEFING — what a Figure A-1 task IS, derived rather than written.
 *
 * 292 of this project's 384 task rows have no attempt recorded at all, so the
 * `unknown` body (UnknownBody.tsx) is shown more often than every other body
 * combined. It is therefore designed as a FEATURE — a calm, instructive card
 * that teaches the Method while it waits — and this module is the half of it
 * that can be tested without a renderer (Node's type-stripping test runner
 * cannot load a `.tsx` module at all, so relative VALUE imports below carry an
 * explicit `.ts` extension, the wire.ts convention).
 *
 * THE RULE THIS MODULE EXISTS TO ENFORCE
 * --------------------------------------
 * NOTHING here is authored per task. Every line is COMPOSED from the generated
 * vocabulary (lifecycleProfiles.ts — the platform's method-assets lifecycles,
 * presented in the profile shape this module already speaks). Sixty-odd
 * hand-written blurbs would drift from the platform's lifecycle the first time a
 * task was renamed, and the drift would be invisible: the card would keep
 * reading fluently while describing work the system no longer schedules.
 *
 * So the composition is a SHAPE applied uniformly:
 *
 *   WHAT IT IS   gate-ness × the phase's display name (+ the phase's own gate
 *                task, named, when this task is not it)
 *   EXIT         the profile phase's generated exitCriterion — verbatim
 *   WEIGHT       the phase's display name + its Table A-1 weight
 *   RETRY RULE   gate-ness again: a gate's failure repeats its whole phase,
 *                a work task's retry re-runs only itself
 *
 * ABSENCE IS NOT THE SAME AS MISSING DATA
 * ---------------------------------------
 * `absenceFor` answers a DIFFERENT question from "is there a record?": a
 * Deployment activity carries no Test Plan phase at all (its profile is
 * Provisioning Spec / Construction / Convergence Verification), so a deep link
 * naming one is a correct absence, not a gap. AbsentBody.tsx says so in those
 * words. Collapsing the two would be the same conflation this whole stage keeps
 * refusing — see activityTree.ts's `unknown` vs `incomplete`, and
 * provenanceAxis.ts's `unknown` vs `observed`.
 */
import type { ConstructionRow } from '../../../../contracts/types';
import type { LensSelection } from '../../lens/useLensSelection';
import type { ClassifiedKind } from '../../list/activityTree.ts';
import type { TaskDetailState } from '../detailPaneState.ts';
import {
  profileFor as lifecycleProfileFor,
  type GeneratedPhase,
  type GeneratedTask,
  type LifecyclePhase,
} from '../../lifecycleProfiles.ts';

// ---------------------------------------------------------------------------
// Vocabulary that has no generated source
// ---------------------------------------------------------------------------

/**
 * The five CANONICAL lifecycle-phase names.
 *
 * Distinct from a profile phase's `name`, which is per-kind (`test_plan` reads
 * "Test Plan" for a service and "Flows" for a frontend). The absent card needs a
 * name for a phase the activity's profile does NOT carry, so by definition there
 * is no per-kind name to read — only the canonical one. Keyed by LifecyclePhase
 * so a new canonical phase is a compile error here rather than a blank card.
 */
export const CANONICAL_PHASE_NAME: Record<LifecyclePhase, string> = {
  requirements: 'Requirements',
  detailed_design: 'Detailed Design',
  test_plan: 'Test Plan',
  construction: 'Construction',
  integration: 'Integration',
};

/**
 * How one activity KIND is named in a sentence ("Deployment activities carry
 * no…"). KindBadge.tsx's KIND_META already carries a display label, but it is a
 * `.tsx` module and node:test cannot load one — so the seven nouns live here.
 * Kind-level vocabulary, not per-task prose: seven entries, exhaustive over
 * ClassifiedKind, so a new kind is a compile error.
 */
export const KIND_NOUN: Record<ClassifiedKind, string> = {
  service: 'Service',
  frontend: 'Frontend',
  testing: 'Testing',
  deployment: 'Deployment',
  documentation: 'Documentation',
  uiDesign: 'UI-design',
  integration: 'Integration',
};

// ---------------------------------------------------------------------------
// Profile resolution — the same rule detailPaneState/activityTree already apply
// ---------------------------------------------------------------------------

/**
 * The activity's Figure A-1 profile, or `undefined` when the server did not
 * classify it. Re-exported under this module's own name because
 * bodyDispatch.ts imports `profileFor` FROM here, taking a `ConstructionRow`
 * rather than the adapter's `kind`/`variant` pair.
 */
export function profileFor(
  row: ConstructionRow | undefined
): readonly GeneratedPhase[] | undefined {
  return lifecycleProfileFor(row?.kind, row?.variant);
}

/** Whether a string is one of the five canonical phases (a URL can carry anything). */
export function isLifecyclePhase(value: string): value is LifecyclePhase {
  return Object.prototype.hasOwnProperty.call(CANONICAL_PHASE_NAME, value);
}

/**
 * The profile phase the selection names — the explicitly selected one, else the
 * row's own current phase. `undefined` when neither resolves to a phase the
 * profile carries (which `absenceFor` then classifies).
 */
export function selectedProfilePhase(
  row: ConstructionRow | undefined,
  selection: LensSelection
): GeneratedPhase | undefined {
  const phases = profileFor(row);
  if (phases === undefined) return undefined;
  const phaseId = selection.lifecyclePhase ?? row?.currentLifecyclePhase;
  if (phaseId === undefined) return undefined;
  return phases.find((p) => p.phase === phaseId);
}

function taskWithin(profilePhase: GeneratedPhase, task: string): GeneratedTask | undefined {
  return profilePhase.tasks.find((tk) => tk.task === task);
}

// ---------------------------------------------------------------------------
// Absence — a phase (or task) the profile deliberately does not carry
// ---------------------------------------------------------------------------

export interface ProfileAbsence {
  /** What is absent: a whole lifecycle phase, or one task inside a phase. */
  scope: 'lifecyclePhase' | 'task';
  /** The absent thing's own name, for the card's title. */
  title: string;
  /** The one sentence the card leads with — "by design, not missing data". */
  statement: string;
  /** What this activity's profile DOES carry, so the reader sees the real shape. */
  carried: { name: string; weight: number }[];
}

/**
 * Whether the selection names something this activity's profile does not carry.
 *
 * Returns `undefined` for every ordinary selection — including one the profile
 * carries but has no record for, which is the `unknown` body's job, not this
 * one. Only a genuine, BY-DESIGN absence lands here.
 *
 * Unresolvable inputs (no row, no profile, a phase id that is not even a
 * canonical Method phase) yield `undefined` too: we cannot assert that a
 * profile deliberately omits something when we do not know what the profile is.
 */
export function absenceFor(
  row: ConstructionRow | undefined,
  selection: LensSelection
): ProfileAbsence | undefined {
  const phases = profileFor(row);
  if (row?.kind === undefined || phases === undefined) return undefined;

  const carried = phases.map((p) => ({ name: p.name, weight: p.weight }));
  const noun = KIND_NOUN[row.kind];

  const phaseId = selection.lifecyclePhase;
  if (phaseId !== undefined && !phases.some((p) => p.phase === phaseId)) {
    // Only a CANONICAL phase can be deliberately absent. An unrecognized id is
    // junk in the URL, not a Method statement, and gets no by-design claim.
    if (!isLifecyclePhase(phaseId)) return undefined;
    const name = CANONICAL_PHASE_NAME[phaseId];
    return {
      scope: 'lifecyclePhase',
      title: name,
      statement: `${noun} activities carry no ${name} phase. This is by design, not missing data.`,
      carried,
    };
  }

  const task = selection.task;
  if (task === undefined) return undefined;
  // Matched across the WHOLE profile, not just the selected phase: a task key
  // belongs to exactly one canonical phase, so a selection whose phase drifted
  // from the profile's is a stale link, not an absence.
  if (phases.some((p) => taskWithin(p, task) !== undefined)) return undefined;

  const hostName = selectedProfilePhase(row, selection)?.name;
  return {
    scope: 'task',
    title: task,
    statement:
      hostName !== undefined
        ? `${noun} activities carry no “${task}” task in ${hostName}. This is by design, not missing data.`
        : `${noun} activities carry no “${task}” task. This is by design, not missing data.`,
    carried,
  };
}

// ---------------------------------------------------------------------------
// The briefing itself
// ---------------------------------------------------------------------------

export interface Briefing {
  /** The card's title — the task's generated label, or the phase's when no task. */
  title: string;
  /** Whether the briefing describes one task or a whole lifecycle phase. */
  scope: 'task' | 'lifecyclePhase';
  whatItIs: string;
  exit: string;
  weight: string;
  retryRule: string;
}

/** The phase's gate task — the one whose success IS its binary exit criterion. */
function gateTaskOf(profilePhase: GeneratedPhase): GeneratedTask | undefined {
  return profilePhase.tasks.find((tk) => tk.gate);
}

/**
 * Compose the briefing for whatever the selection names.
 *
 * A TASK briefing when one resolves, a PHASE briefing when only a phase does,
 * and `undefined` when neither can be resolved — an unclassified activity has
 * no profile at all, and inventing a lifecycle for it is precisely the
 * fabrication activityTree.ts refuses (rule 1). The card then shows the "no
 * record" line alone rather than a plausible-looking table.
 */
export function briefingFor(
  row: ConstructionRow | undefined,
  selection: LensSelection
): Briefing | undefined {
  const profilePhase = selectedProfilePhase(row, selection);
  if (profilePhase === undefined) return undefined;

  const weight = `part of ${profilePhase.name} · ${String(profilePhase.weight)}% of this activity.`;
  const exit = profilePhase.exitCriterion;
  const gate = gateTaskOf(profilePhase);

  const task = selection.task !== undefined ? taskWithin(profilePhase, selection.task) : undefined;
  if (task === undefined) {
    return {
      title: profilePhase.name,
      scope: 'lifecyclePhase',
      whatItIs:
        gate !== undefined
          ? `One App-A lifecycle phase of this activity (Figure A-1). Its binary exit is decided by ${gate.label}.`
          : 'One App-A lifecycle phase of this activity (Figure A-1).',
      exit,
      weight,
      retryRule:
        gate !== undefined
          ? `A failing ${gate.label} repeats ${profilePhase.name}.`
          : `A failure repeats ${profilePhase.name}.`,
    };
  }

  const conditionalNote = task.conditional
    ? ' Conditional: the profile emits it only once a real attempt exists, so it is never shown as owed work.'
    : '';

  return {
    title: task.label,
    scope: 'task',
    whatItIs: task.gate
      ? `The gate task of ${profilePhase.name} (Figure A-1). Its outcome IS this phase's binary exit criterion.${conditionalNote}`
      : gate !== undefined
        ? `A work task within ${profilePhase.name} (Figure A-1). The phase's exit rests on ${gate.label}.${conditionalNote}`
        : `A work task within ${profilePhase.name} (Figure A-1).${conditionalNote}`,
    exit,
    weight,
    retryRule: task.gate
      ? `A failing ${task.label} repeats ${profilePhase.name}.`
      : gate !== undefined
        ? `A retry re-runs ${task.label} alone; ${gate.label} still decides the phase.`
        : `A retry re-runs ${task.label} alone.`,
  };
}

/**
 * The one sentence the unknown card leads with when the selection is UNKNOWN.
 *
 * Two causes, both stated, neither guessed — the surface genuinely cannot tell
 * them apart, and picking one would be a fabrication in a very quiet voice.
 * Deliberately free of error tone: nothing here went wrong.
 *
 * Only for UNKNOWN (detailPaneState.noAttemptStateFor: an unclassified row, or
 * build evidence with ZERO attempts — history that predates per-task capture).
 * Where the history is complete, the second cause cannot be true, so a NOT
 * STARTED selection says NOT_STARTED_STATEMENT instead.
 */
export const UNKNOWN_STATEMENT =
  'No record. This task has not run, or it ran before per-task history was captured.';

/** The same sentence at phase/activity scope — "task" would name the wrong thing. */
export const UNKNOWN_STATEMENT_UNSCOPED =
  'No record. This has not run, or it ran before per-task history was captured.';

/** What a NOT STARTED selection says: its history is complete (or nothing has
 *  happened at all), so "never ran" is the only cause left, and it is stated. */
export const NOT_STARTED_STATEMENT = 'Not started. Nothing has run for this task yet.';

/** The same sentence at phase/activity scope. */
export const NOT_STARTED_STATEMENT_UNSCOPED = 'Not started. Nothing has run for this yet.';

/**
 * The unknown card's lead sentence, by the no-attempt rule the pane's state
 * already follows (`state` is the pane's taskDetailStateFor): hidden attempts
 * first (Observed only), then NOT STARTED, and the two-cause sentence only for
 * what is genuinely UNKNOWN.
 */
export function unknownStatementFor(
  scope: Briefing['scope'] | undefined,
  hiddenCount: number,
  state: TaskDetailState
): string {
  if (hiddenCount > 0) return observedOnlyStatement(hiddenCount);
  if (state === 'notStarted') {
    return scope === 'task' ? NOT_STARTED_STATEMENT : NOT_STARTED_STATEMENT_UNSCOPED;
  }
  return scope === 'task' ? UNKNOWN_STATEMENT : UNKNOWN_STATEMENT_UNSCOPED;
}

/**
 * What the unknown card says when "Observed only" set this selection's attempts
 * aside (designer re-check B1). "No record. This has not run" would be false: a
 * record exists, it was reconstructed, and the toggle is hiding it.
 */
export function observedOnlyStatement(hiddenCount: number): string {
  const n = String(hiddenCount);
  return hiddenCount === 1
    ? `Nothing observed. ${n} reconstructed attempt is hidden by Observed only — turn it off to see it.`
    : `Nothing observed. ${n} reconstructed attempts are hidden by Observed only — turn it off to see them.`;
}

/** The briefing-less note for an activity whose evidence Observed only set aside. */
export const OBSERVED_ONLY_NO_PHASE_NOTE =
  'Nothing observed against this activity, so it has no current phase to brief. Its ' +
  'lifecycle is drawn in the list — select a phase or task there for its exit criterion and weight.';

/** No profile at all — the server could not classify the activity. */
export const NO_PROFILE_NOTE =
  'No Figure A-1 profile resolved for this activity, so there is no exit criterion or Table ' +
  'A-1 weight to show. The server could not classify it, and this surface does not guess one.';

/** A profile, but no single phase to brief — nothing recorded, so no current phase. */
export const NO_CURRENT_PHASE_NOTE =
  'Nothing is recorded against this activity yet, so it has no current phase to brief. Its ' +
  'lifecycle is drawn in the list — select a phase or task there for its exit criterion and weight.';

/**
 * The GRAPH lens's words for the two notes above (designer P2: lens-aware pane
 * copy). There the lifecycle is drawn on the activity's card, and a phase is
 * reached by clicking a segment of its lifecycle bar — "select a phase or task
 * there [in the list]" pointed at a surface not on screen.
 */
export const NO_CURRENT_PHASE_NOTE_GRAPH =
  'Nothing is recorded against this activity yet, so it has no current phase to brief. Its ' +
  'lifecycle is drawn on its card — click a segment of its lifecycle bar for its exit criterion and weight.';

export const OBSERVED_ONLY_NO_PHASE_NOTE_GRAPH =
  'Nothing observed against this activity, so it has no current phase to brief. Its ' +
  'lifecycle is drawn on its card — click a segment of its lifecycle bar for its exit criterion and weight.';

/**
 * The TASKS lens's words for the same two notes (integration review minor, fix I).
 * The tasks lens lists owed work and draws no lifecycle at all, so "drawn in the
 * list — select a phase or task there" pointed at a list that is not on screen. It
 * names the two lenses that do draw it.
 */
export const NO_CURRENT_PHASE_NOTE_TASKS =
  'Nothing is recorded against this activity yet, so it has no current phase to brief. ' +
  'Open it in the List or Graph lens to see its lifecycle, and select a phase or task there for its exit criterion and weight.';

export const OBSERVED_ONLY_NO_PHASE_NOTE_TASKS =
  'Nothing observed against this activity, so it has no current phase to brief. ' +
  'Open it in the List or Graph lens to see its lifecycle, and select a phase or task there for its exit criterion and weight.';

/**
 * The line the unknown card shows in place of the briefing table when
 * `briefingFor` resolves nothing. Two different reasons, never conflated: an
 * activity with NO profile (unclassified — inventing one is the fabrication
 * activityTree.ts refuses), and a CLASSIFIED activity selected at activity
 * level with no reported current phase — every planned-no-record row, whose
 * profile the list is drawing right beside this card. Telling the second one
 * "the server could not classify it" would be false.
 */
export function noBriefingNoteFor(
  row: ConstructionRow | undefined,
  hiddenCount = 0,
  /** Which lens the pane sits beside: the list points at its rows, the graph at the
   *  card, and the tasks lens — which draws no lifecycle — at the other two. */
  lens: 'list' | 'graph' | 'tasks' = 'list'
): string {
  if (profileFor(row) === undefined) return NO_PROFILE_NOTE;
  const observedOnly = hiddenCount > 0;
  switch (lens) {
    case 'list':
      return observedOnly ? OBSERVED_ONLY_NO_PHASE_NOTE : NO_CURRENT_PHASE_NOTE;
    case 'graph':
      return observedOnly ? OBSERVED_ONLY_NO_PHASE_NOTE_GRAPH : NO_CURRENT_PHASE_NOTE_GRAPH;
    case 'tasks':
      return observedOnly ? OBSERVED_ONLY_NO_PHASE_NOTE_TASKS : NO_CURRENT_PHASE_NOTE_TASKS;
  }
}

/**
 * The unknown card's heading names WHAT IS SELECTED (designer P1-6): the task's
 * label, the phase's name, or — for an activity-level selection — "This
 * activity". It used to fall back to "This task" for every selection, so an
 * activity's pane was headed as if one task were open.
 *
 * The briefing's own title wins where one resolved (it is the generated label or
 * phase name); without a profile to resolve against, the raw task / phase key is
 * the most specific honest name there is.
 */
export function unknownTitleFor(briefing: Briefing | undefined, selection: LensSelection): string {
  if (briefing !== undefined) return briefing.title;
  if (selection.task !== undefined) return selection.task;
  if (selection.lifecyclePhase !== undefined) return selection.lifecyclePhase;
  return 'This activity';
}
